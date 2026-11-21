package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Settings is everything the dashboard can change. It is stored as JSON in
// $XDG_CONFIG_HOME/overspan/config.json.
type Settings struct {
	Enabled         bool      `json:"enabled"`
	ServerName      string    `json:"serverName"`
	RequireApproval bool      `json:"requireApproval"`
	PIN             string    `json:"pin"`
	DefaultMode     string    `json:"defaultMode"`  // extend | mirror
	MirrorOutput    string    `json:"mirrorOutput"` // "" = first output
	Placement       string    `json:"placement"`    // right | left | above | below
	MaxLongEdge     int       `json:"maxLongEdge"`  // cap for virtual monitor size
	FPS             int       `json:"fps"`
	Quality         int       `json:"quality"`
	ShowCursor      bool      `json:"showCursor"`
	AllowInput      bool      `json:"allowInput"`
	KeepSeconds     int       `json:"keepSeconds"` // keep a virtual monitor after a drop
	Trusted         []Trusted `json:"trusted"`
}

// Trusted is a viewer that skips the approval prompt.
type Trusted struct {
	DeviceID string    `json:"deviceId"`
	Name     string    `json:"name"`
	Added    time.Time `json:"added"`
	LastSeen time.Time `json:"lastSeen"`
}

func defaultSettings() Settings {
	host, _ := os.Hostname()
	return Settings{
		Enabled:         true,
		ServerName:      host,
		RequireApproval: true,
		DefaultMode:     "extend",
		Placement:       "right",
		MaxLongEdge:     2560,
		FPS:             30,
		Quality:         70,
		ShowCursor:      true,
		AllowInput:      true,
		KeepSeconds:     15,
	}
}

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "overspan", "config.json")
}

func loadSettings(path string) Settings {
	s := defaultSettings()
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	json.Unmarshal(b, &s)
	s.sanitize()
	return s
}

func (s *Settings) sanitize() {
	clampInt := func(v *int, lo, hi, def int) {
		if *v == 0 {
			*v = def
		}
		*v = max(lo, min(hi, *v))
	}
	clampInt(&s.FPS, 5, 60, 30)
	clampInt(&s.Quality, 20, 95, 70)
	clampInt(&s.MaxLongEdge, 640, 7680, 2560)
	if s.KeepSeconds < 0 {
		s.KeepSeconds = 0
	}
	s.KeepSeconds = min(s.KeepSeconds, 600)
	switch s.DefaultMode {
	case "extend", "mirror":
	default:
		s.DefaultMode = "extend"
	}
	switch s.Placement {
	case "right", "left", "above", "below":
	default:
		s.Placement = "right"
	}
	if len(s.PIN) > 12 {
		s.PIN = s.PIN[:12]
	}
	if len(s.ServerName) > 60 {
		s.ServerName = s.ServerName[:60]
	}
}

func saveSettings(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
