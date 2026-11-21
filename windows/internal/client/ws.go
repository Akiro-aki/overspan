// Package client is the platform-neutral half of the Windows viewer: the
// WebSocket link, frame decoding, discovery, saved settings and the
// connection state machine. It has no Windows dependencies, so it is tested
// on any OS against a real Overspan server.
package client

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WebSocket opcodes.
const (
	OpText   = 0x1
	OpBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

// Conn is a client-side RFC 6455 connection. Reads happen on one goroutine;
// writes may come from any goroutine.
type Conn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex
}

// Dial opens ws://host:port/ws.
func Dial(host string, port int, userAgent string) (*Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	c, err := net.DialTimeout("tcp", addr, 6*time.Second)
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	key := base64.StdEncoding.EncodeToString(nonce[:])
	req := "GET /ws HTTP/1.1\r\nHost: " + addr +
		"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + key +
		"\r\nSec-WebSocket-Version: 13\r\nUser-Agent: " + userAgent + "\r\n\r\n"
	c.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(c, 1<<16)
	status, err := r.ReadString('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101") {
		c.Close()
		return nil, fmt.Errorf("server refused the upgrade: %s", strings.TrimSpace(status))
	}
	accept := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			c.Close()
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(v)
		}
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if accept != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, errors.New("bad handshake")
	}
	c.SetDeadline(time.Time{})
	return &Conn{c: c, r: r}, nil
}

// Read returns the next text or binary message, answering pings on the way.
func (w *Conn) Read() (op int, data []byte, err error) {
	var msg []byte
	msgOp := 0
	for {
		var hdr [2]byte
		if _, err = io.ReadFull(w.r, hdr[:]); err != nil {
			return
		}
		fin := hdr[0]&0x80 != 0
		o := int(hdr[0] & 0x0f)
		n := uint64(hdr[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err = io.ReadFull(w.r, b[:]); err != nil {
				return
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err = io.ReadFull(w.r, b[:]); err != nil {
				return
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 64<<20 {
			return 0, nil, errors.New("frame too large")
		}
		var mask [4]byte
		masked := hdr[1]&0x80 != 0
		if masked {
			if _, err = io.ReadFull(w.r, mask[:]); err != nil {
				return
			}
		}
		p := make([]byte, n)
		if _, err = io.ReadFull(w.r, p); err != nil {
			return
		}
		if masked {
			for i := range p {
				p[i] ^= mask[i&3]
			}
		}
		switch o {
		case opPing:
			w.write(opPong, p)
			continue
		case opPong:
			continue
		case opClose:
			return 0, nil, io.EOF
		case 0:
			msg = append(msg, p...)
		default:
			msg, msgOp = p, o
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (w *Conn) write(op int, p []byte) error {
	n := len(p)
	f := make([]byte, 0, n+14)
	f = append(f, byte(0x80|op))
	switch {
	case n < 126:
		f = append(f, byte(0x80|n))
	case n < 65536:
		f = append(f, 0x80|126, byte(n>>8), byte(n))
	default:
		f = append(f, 0x80|127)
		f = binary.BigEndian.AppendUint64(f, uint64(n))
	}
	var mask [4]byte
	rand.Read(mask[:])
	f = append(f, mask[:]...)
	for i, b := range p {
		f = append(f, b^mask[i&3])
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	w.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := w.c.Write(f)
	return err
}

// WriteText sends one text message.
func (w *Conn) WriteText(s string) error { return w.write(OpText, []byte(s)) }

// Close sends a close frame and drops the connection.
func (w *Conn) Close() {
	w.write(opClose, []byte{0x03, 0xe8})
	w.c.Close()
}
