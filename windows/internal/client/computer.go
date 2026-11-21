package client

import (
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the server's HTTP/WebSocket port; discovery uses the next one.
const DefaultPort = 7940

// Computer is one Overspan server the viewer knows about.
type Computer struct {
	Host    string
	Port    int
	Name    string
	Enabled bool
	Found   bool // answered discovery recently
	Saved   bool
	Version string
	SeenAt  time.Time
}

// Key identifies a computer by address.
func (c Computer) Key() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// Title is the friendly name, or the address when there is none.
func (c Computer) Title() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Host
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-]+$`)

// ParseAddress accepts "host", "host:port" or a pasted URL.
func ParseAddress(input string) (Computer, bool) {
	s := strings.TrimSpace(input)
	if i := strings.Index(s, "://"); i > 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port := s, DefaultPort
	if i := strings.LastIndexByte(s, ':'); i > 0 && strings.IndexByte(s, ':') == i {
		p, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return Computer{}, false
		}
		host, port = s[:i], p
	}
	if host == "" || port < 1 || port > 65535 || !hostRe.MatchString(host) {
		return Computer{}, false
	}
	return Computer{Host: host, Port: port, Enabled: true}, true
}

// Info is the server's public /api/info card.
type Info struct {
	Product   string `json:"product"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Enabled   bool   `json:"enabled"`
	CanExtend bool   `json:"canExtend"`
	PIN       bool   `json:"pin"`
}

// FetchInfo asks a computer for its card. It fails if the answer is not
// from an Overspan server.
func FetchInfo(c Computer) (*Info, error) {
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + c.Key() + "/api/info")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	info := &Info{Enabled: true, CanExtend: true}
	if resp.StatusCode != 200 {
		return nil, errNotOverspan
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 65536)).Decode(info); err != nil {
		return nil, err
	}
	if info.Product != "Overspan" {
		return nil, errNotOverspan
	}
	return info, nil
}

type strErr string

func (e strErr) Error() string { return string(e) }

const errNotOverspan = strErr("not an Overspan server")
