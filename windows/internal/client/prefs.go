package client

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// SavedComputer is one remembered address.
type SavedComputer struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Name string `json:"name"`
}

// Prefs are the viewer's settings, stored as JSON.
type Prefs struct {
	mu   sync.Mutex
	path string

	Device       string          `json:"device"`
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`       // extend | mirror
	Res          string          `json:"res"`        // native | balanced | light
	Quality      int             `json:"quality"`    // 30..95
	FPS          int             `json:"fps"`        // 10..60
	Fullscreen   bool            `json:"fullscreen"` // open the display full screen
	FollowWindow bool            `json:"followWindow"`
	Stats        bool            `json:"stats"`
	SeenHint     bool            `json:"seenHint"`
	Saved        []SavedComputer `json:"saved"`
}

// LoadPrefs reads path, filling defaults for anything missing.
func LoadPrefs(path string) *Prefs {
	p := &Prefs{path: path, Mode: "extend", Res: "balanced", Quality: 70, FPS: 30, Fullscreen: true, FollowWindow: true}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, p)
	}
	if p.Mode != "mirror" {
		p.Mode = "extend"
	}
	switch p.Res {
	case "native", "light":
	default:
		p.Res = "balanced"
	}
	p.Quality = max(30, min(95, p.Quality))
	p.FPS = max(10, min(60, p.FPS))
	if len(p.Device) < 16 {
		var b [16]byte
		rand.Read(b[:])
		p.Device = hex.EncodeToString(b[:])
		p.Save()
	}
	return p
}

// Save writes the settings back, atomically.
func (p *Prefs) Save() error {
	p.mu.Lock()
	b, _ := json.MarshalIndent(p, "", "  ")
	path := p.path
	p.mu.Unlock()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeviceName is the name shown on the computer: the saved one, or fallback.
func (p *Prefs) DeviceName(fallback string) string {
	if p.Name != "" {
		return p.Name
	}
	if fallback != "" {
		return fallback
	}
	return "Windows PC"
}

// SavedComputers returns the remembered computers, most recent first.
func (p *Prefs) SavedComputers() []Computer {
	out := make([]Computer, 0, len(p.Saved))
	for _, s := range p.Saved {
		port := s.Port
		if port == 0 {
			port = DefaultPort
		}
		out = append(out, Computer{Host: s.Host, Port: port, Name: s.Name, Saved: true, Enabled: true})
	}
	return out
}

// Remember puts c first in the saved list (12 at most).
func (p *Prefs) Remember(c Computer) {
	list := []SavedComputer{{c.Host, c.Port, c.Name}}
	for _, s := range p.Saved {
		if (Computer{Host: s.Host, Port: s.Port}).Key() != c.Key() && len(list) < 12 {
			list = append(list, s)
		}
	}
	p.Saved = list
	p.Save()
}

// Forget removes c from the saved list.
func (p *Prefs) Forget(c Computer) {
	var list []SavedComputer
	for _, s := range p.Saved {
		if (Computer{Host: s.Host, Port: s.Port}).Key() != c.Key() {
			list = append(list, s)
		}
	}
	p.Saved = list
	p.Save()
}
