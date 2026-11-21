package vdisplay

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Sway creates outputs with `swaymsg create_output` (named HEADLESS-n) and
// unplugs them when done.
type Sway struct{}

func (w *Sway) Name() string         { return "sway" }
func (w *Sway) CanCreate() bool      { return true }
func (w *Sway) Capture() CaptureKind { return CaptureScreencopy }

type swayOutput struct {
	Name    string  `json:"name"`
	Active  bool    `json:"active"`
	Primary bool    `json:"primary"`
	Scale   float64 `json:"scale"`
	Rect    struct {
		X, Y, Width, Height int
	} `json:"rect"`
	CurrentMode struct {
		Width, Height int
	} `json:"current_mode"`
}

func (w *Sway) Outputs() ([]Output, error) {
	raw, err := run("swaymsg", "-r", "-t", "get_outputs")
	if err != nil {
		return nil, err
	}
	var so []swayOutput
	if err := json.Unmarshal([]byte(raw), &so); err != nil {
		return nil, err
	}
	var outs []Output
	for _, o := range so {
		if !o.Active {
			continue
		}
		outs = append(outs, Output{
			Name: o.Name, X: o.Rect.X, Y: o.Rect.Y, W: o.Rect.Width, H: o.Rect.Height,
			PxW: o.CurrentMode.Width, PxH: o.CurrentMode.Height, Scale: o.Scale,
			Virtual: strings.HasPrefix(o.Name, "HEADLESS"), Primary: o.Rect.X == 0 && o.Rect.Y == 0,
		})
	}
	return outs, nil
}

func (w *Sway) Create(id string, s Spec) (string, error) {
	before, err := w.Outputs()
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, o := range before {
		seen[o.Name] = true
	}
	if _, err := run("swaymsg", "create_output"); err != nil {
		return "", err
	}
	var name string
	waitFor(3*time.Second, func() bool {
		outs, _ := w.Outputs()
		for _, o := range outs {
			if !seen[o.Name] {
				name = o.Name
				return true
			}
		}
		return false
	})
	if name == "" {
		return "", fmt.Errorf("sway did not create an output (is the headless backend available?)")
	}
	s.Place = placeKey(before, s)
	if err := w.Enforce(name, s); err != nil {
		w.Remove(name)
		return "", err
	}
	return name, nil
}

func (w *Sway) Enforce(name string, s Spec) error {
	var x, y int
	fmt.Sscanf(s.Place, "%d,%d", &x, &y)
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	refresh := s.Refresh
	if refresh <= 0 {
		refresh = 60
	}
	_, err := run("swaymsg", "output", name,
		"mode", fmt.Sprintf("%dx%d@%dHz", s.Width, s.Height, refresh),
		"position", fmt.Sprint(x), fmt.Sprint(y),
		"scale", fmt.Sprintf("%.3f", scale))
	return err
}

func (w *Sway) Remove(name string) error {
	_, err := run("swaymsg", "output", name, "unplug")
	return err
}

// Wlroots covers compositors that speak wlr-screencopy but have no command
// for adding outputs (river, labwc, wayfire, niri...). Mirror mode works;
// extend mode needs a dummy monitor configured by hand or the EVDI module.
type Wlroots struct{}

func (w *Wlroots) Name() string                        { return "wlroots" }
func (w *Wlroots) CanCreate() bool                     { return false }
func (w *Wlroots) Capture() CaptureKind                { return CaptureScreencopy }
func (w *Wlroots) Create(string, Spec) (string, error) { return "", ErrUnsupported }
func (w *Wlroots) Remove(string) error                 { return nil }
func (w *Wlroots) Enforce(string, Spec) error          { return nil }

// Outputs comes from the Wayland protocol itself; positions are unknown to
// wl_output v4 without xdg-output, so outputs are laid out left to right.
// Input mapping is approximate on multi-monitor setups.
func (w *Wlroots) Outputs() ([]Output, error) { return wlOutputs() }
