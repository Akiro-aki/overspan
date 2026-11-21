package server

import (
	"encoding/json"
	"net"
	"strings"
)

// ServeDiscovery answers LAN discovery probes on UDP port+1. A native viewer
// broadcasts "OVERSPAN?" and every server replies with a short JSON card.
// Browsers cannot send UDP, so the web viewer uses the QR code or address.
func (s *Server) ServeDiscovery() error {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{Port: s.Port + 1})
	if err != nil {
		return err
	}
	go func() {
		defer pc.Close()
		buf := make([]byte, 64)
		for {
			n, from, err := pc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.TrimSpace(string(buf[:n])) != "OVERSPAN?" {
				continue
			}
			s.mu.Lock()
			card := map[string]any{"product": "Overspan", "version": Version, "name": s.settings.ServerName, "port": s.Port, "enabled": s.settings.Enabled}
			s.mu.Unlock()
			b, _ := json.Marshal(card)
			pc.WriteToUDP(b, from)
		}
	}()
	return nil
}
