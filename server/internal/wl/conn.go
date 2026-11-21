// Package wl is a minimal, dependency-free Wayland client: just enough of the
// wire protocol to bind globals, enumerate outputs and pull frames through
// wlr-screencopy. It needs no libwayland and no cgo, so the resulting binary
// runs on any distribution.
package wl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Event is one decoded message from the compositor.
type Event struct {
	Sender uint32
	Opcode uint16
	Args   []byte
	FDs    []int
}

// Handler receives the events addressed to one object.
type Handler func(e *Event)

// Conn is a connection to a Wayland display.
type Conn struct {
	sock     *net.UnixConn
	mu       sync.Mutex // guards writes and the object table
	nextID   uint32
	handlers map[uint32]Handler
	rbuf     []byte
	pendFDs  []int
	err      error
}

// Dial connects to $WAYLAND_DISPLAY (or wayland-0).
func Dial() (*Conn, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	path := name
	if !filepath.IsAbs(name) {
		rt := os.Getenv("XDG_RUNTIME_DIR")
		if rt == "" {
			return nil, errors.New("XDG_RUNTIME_DIR is not set")
		}
		path = filepath.Join(rt, name)
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	conn := &Conn{sock: c, nextID: 2, handlers: map[uint32]Handler{}}
	conn.handlers[1] = conn.displayEvent
	return conn, nil
}

func (c *Conn) Close() error { return c.sock.Close() }

func (c *Conn) displayEvent(e *Event) {
	r := NewReader(e.Args)
	switch e.Opcode {
	case 0: // error
		obj := r.Uint()
		code := r.Uint()
		msg := r.String()
		c.err = fmt.Errorf("wayland error on object %d code %d: %s", obj, code, msg)
	case 1: // delete_id
		id := r.Uint()
		c.mu.Lock()
		delete(c.handlers, id)
		c.mu.Unlock()
	}
}

// NewID allocates an object id and attaches h to it.
func (c *Conn) NewID(h Handler) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	if h == nil {
		h = func(*Event) {}
	}
	c.handlers[id] = h
	return id
}

// SetHandler replaces the handler of an existing object.
func (c *Conn) SetHandler(id uint32, h Handler) {
	c.mu.Lock()
	c.handlers[id] = h
	c.mu.Unlock()
}

// Send writes one request. fds are passed with SCM_RIGHTS.
func (c *Conn) Send(obj uint32, opcode uint16, w *Writer, fds ...int) error {
	var payload []byte
	if w != nil {
		payload = w.buf
	}
	msg := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(msg[0:], obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(opcode))
	copy(msg[8:], payload)
	c.mu.Lock()
	defer c.mu.Unlock()
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	_, _, err := c.sock.WriteMsgUnix(msg, oob, nil)
	return err
}

// Dispatch blocks until at least one event arrived and runs its handler.
func (c *Conn) Dispatch() error {
	if c.err != nil {
		return c.err
	}
	for {
		if len(c.rbuf) >= 8 {
			size := int(binary.LittleEndian.Uint32(c.rbuf[4:]) >> 16)
			if size < 8 {
				return errors.New("wayland: malformed message")
			}
			if len(c.rbuf) >= size {
				e := &Event{
					Sender: binary.LittleEndian.Uint32(c.rbuf[0:]),
					Opcode: uint16(binary.LittleEndian.Uint32(c.rbuf[4:]) & 0xffff),
					Args:   append([]byte(nil), c.rbuf[8:size]...),
				}
				c.rbuf = c.rbuf[size:]
				c.mu.Lock()
				h := c.handlers[e.Sender]
				c.mu.Unlock()
				e.FDs = c.pendFDs // fds are rare for the events we care about
				c.pendFDs = nil
				if h != nil {
					h(e)
				}
				for _, fd := range e.FDs {
					if h == nil {
						syscall.Close(fd)
					}
				}
				return c.err
			}
		}
		buf := make([]byte, 65536)
		oob := make([]byte, 256)
		n, oobn, _, _, err := c.sock.ReadMsgUnix(buf, oob)
		if err != nil {
			return err
		}
		if oobn > 0 {
			msgs, _ := syscall.ParseSocketControlMessage(oob[:oobn])
			for _, m := range msgs {
				if fds, err := syscall.ParseUnixRights(&m); err == nil {
					c.pendFDs = append(c.pendFDs, fds...)
				}
			}
		}
		c.rbuf = append(c.rbuf, buf[:n]...)
	}
}

// Roundtrip sends wl_display.sync and dispatches until the callback fires.
func (c *Conn) Roundtrip() error {
	done := false
	cb := c.NewID(func(e *Event) { done = true })
	if err := c.Send(1, 0, NewWriter().Uint(cb)); err != nil {
		return err
	}
	for !done {
		if err := c.Dispatch(); err != nil {
			return err
		}
	}
	return nil
}

// Writer marshals request arguments.
type Writer struct{ buf []byte }

func NewWriter() *Writer { return &Writer{} }

func (w *Writer) Uint(v uint32) *Writer {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
	return w
}

func (w *Writer) Int(v int32) *Writer { return w.Uint(uint32(v)) }

func (w *Writer) String(s string) *Writer {
	n := len(s) + 1
	w.Uint(uint32(n))
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0)
	}
	return w
}

// Bytes returns the encoded arguments (for tests).
func (w *Writer) Bytes() []byte { return w.buf }

// Reader unmarshals event arguments.
type Reader struct {
	b   []byte
	off int
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) Uint() uint32 {
	if r.off+4 > len(r.b) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *Reader) Int() int32 { return int32(r.Uint()) }

func (r *Reader) String() string {
	n := int(r.Uint())
	if n == 0 || r.off+n > len(r.b) {
		return ""
	}
	s := string(r.b[r.off : r.off+n-1])
	r.off += (n + 3) &^ 3
	return s
}
