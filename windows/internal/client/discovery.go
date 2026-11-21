package client

import (
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"
)

// Discovery finds servers by sending "OVERSPAN?" to UDP port 7941 on every
// broadcast address, plus each saved computer directly (some networks and
// firewalls drop broadcasts).
type Discovery struct {
	mu     sync.Mutex
	direct []string
	conn   *net.UDPConn
	stop   chan struct{}
}

// AddHost also probes host directly.
func (d *Discovery) AddHost(host string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, h := range d.direct {
		if h == host {
			return
		}
	}
	d.direct = append(d.direct, host)
}

// Start probes every 2.5 s and calls found for each answer, on its own goroutine.
func (d *Discovery) Start(found func(Computer)) error {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.conn, d.stop = c, make(chan struct{})
	stop := d.stop
	d.mu.Unlock()

	go func() {
		probe := []byte("OVERSPAN?")
		t := time.NewTicker(2500 * time.Millisecond)
		defer t.Stop()
		for {
			for _, a := range d.targets() {
				c.WriteToUDP(probe, a)
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var card struct {
				Product string `json:"product"`
				Name    string `json:"name"`
				Port    int    `json:"port"`
				Enabled *bool  `json:"enabled"`
				Version string `json:"version"`
			}
			if json.Unmarshal(buf[:n], &card) != nil || card.Product != "Overspan" {
				continue
			}
			if card.Port == 0 {
				card.Port = DefaultPort
			}
			found(Computer{
				Host: from.IP.String(), Port: card.Port, Name: card.Name,
				Enabled: card.Enabled == nil || *card.Enabled, Found: true,
				Version: card.Version, SeenAt: time.Now(),
			})
		}
	}()
	return nil
}

// Stop ends discovery.
func (d *Discovery) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		close(d.stop)
		d.stop = nil
	}
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
}

func (d *Discovery) targets() []*net.UDPAddr {
	port := DefaultPort + 1
	out := []*net.UDPAddr{{IP: net.IPv4bcast, Port: port}}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, m := n.IP.To4(), n.Mask
			if len(m) == 16 {
				m = m[12:]
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip[i] | ^m[i]
			}
			out = append(out, &net.UDPAddr{IP: b, Port: port})
		}
	}
	d.mu.Lock()
	hosts := append([]string(nil), d.direct...)
	d.mu.Unlock()
	for _, h := range hosts {
		if a, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(h, strconv.Itoa(port))); err == nil {
			out = append(out, a)
		}
	}
	return out
}
