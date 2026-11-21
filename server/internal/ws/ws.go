// Package ws is a small RFC 6455 WebSocket server implementation: enough for
// one browser talking to one handler, with no third-party modules.
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	OpText   = 1
	OpBinary = 2
	opClose  = 8
	opPing   = 9
	opPong   = 10

	maxClientMessage = 1 << 20
)

var ErrClosed = errors.New("websocket closed")

// Conn is one accepted WebSocket.
type Conn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

// AcceptKey computes Sec-WebSocket-Accept for a client key.
func AcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// Upgrade completes the handshake. checkOrigin may be nil.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != http.MethodGet || !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") {
		http.Error(w, "expected a WebSocket upgrade", http.StatusBadRequest)
		return nil, errors.New("not a websocket request")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "unsupported WebSocket version", http.StatusBadRequest)
		return nil, errors.New("bad websocket version")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("server does not support hijacking")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	if _, err := c.Write([]byte(resp)); err != nil {
		c.Close()
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	return &Conn{c: c, r: rw.Reader}, nil
}

// RemoteAddr is the peer address.
func (c *Conn) RemoteAddr() net.Addr { return c.c.RemoteAddr() }

// Write sends one unfragmented message.
func (c *Conn) Write(op byte, data []byte) error {
	return c.writeFrame(op, data, 10*time.Second)
}

func (c *Conn) writeFrame(op byte, data []byte, timeout time.Duration) error {
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|op)
	switch n := len(data); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 65536:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.c.SetWriteDeadline(time.Now().Add(timeout))
	bufs := net.Buffers{hdr, data}
	_, err := bufs.WriteTo(c.c)
	return err
}

// Read returns the next complete data message, answering pings itself.
func (c *Conn) Read() (op byte, data []byte, err error) {
	var msgOp byte
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.r, h[:]); err != nil {
			return 0, nil, err
		}
		fin := h[0]&0x80 != 0
		fop := h[0] & 0x0f
		masked := h[1]&0x80 != 0
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if !masked {
			return 0, nil, errors.New("client frames must be masked")
		}
		if n > maxClientMessage || uint64(len(msg))+n > maxClientMessage {
			return 0, nil, errors.New("message too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.r, mask[:]); err != nil {
			return 0, nil, err
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return 0, nil, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch fop {
		case opPing:
			c.writeFrame(opPong, payload, 5*time.Second)
			continue
		case opPong:
			continue
		case opClose:
			c.writeFrame(opClose, payload[:min(2, len(payload))], time.Second)
			return 0, nil, ErrClosed
		case 0: // continuation
			msg = append(msg, payload...)
		default:
			msgOp, msg = fop, payload
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

// Ping keeps NAT and Wi-Fi power saving from dropping an idle link.
func (c *Conn) Ping() error { return c.writeFrame(opPing, nil, 5*time.Second) }

func (c *Conn) Close() error {
	c.writeFrame(opClose, []byte{0x03, 0xe8}, time.Second)
	return c.c.Close()
}

// SetReadDeadline bounds the next Read (zero time clears it).
func (c *Conn) SetReadDeadline(t time.Time) error { return c.c.SetReadDeadline(t) }
