// Package vdisplay creates and removes virtual monitors. Linux has no single
// API for this, so each desktop gets a backend and Detect picks the one that
// matches the running session:
//
//	hyprland  hyprctl output create headless        (Wayland)
//	sway      swaymsg create_output                 (Wayland, wlroots)
//	x11       xrandr VIRTUAL outputs or --setmonitor (any X11 desktop)
//	wlroots   no virtual outputs, mirror only        (river, labwc, niri, wayfire...)
//
// Every backend reports the global layout so input can be mapped onto it.
package vdisplay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Output is one monitor in global layout (logical) coordinates.
type Output struct {
	Name    string  `json:"name"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	W       int     `json:"w"` // logical size
	H       int     `json:"h"`
	PxW     int     `json:"pxW"` // framebuffer size
	PxH     int     `json:"pxH"`
	Scale   float64 `json:"scale"`
	Virtual bool    `json:"virtual"`
	Primary bool    `json:"primary"`
}

// Spec is what a viewer asks for.
type Spec struct {
	Width, Height int     // physical pixels
	Refresh       int     // Hz
	Scale         float64 // desktop scale on the virtual monitor
	Place         string  // right, left, above, below
}

// CaptureKind tells the server which capturer works with a backend.
type CaptureKind string

const (
	CaptureScreencopy CaptureKind = "screencopy"
	CaptureX11        CaptureKind = "x11grab"
	CaptureNone       CaptureKind = "none"
)

// Backend is one way of making virtual monitors.
type Backend interface {
	Name() string
	CanCreate() bool
	Capture() CaptureKind
	Outputs() ([]Output, error)
	// Create adds a monitor named after id and returns its output name.
	Create(id string, s Spec) (string, error)
	Remove(name string) error
	// Enforce re-applies the mode and position if something reset them.
	Enforce(name string, s Spec) error
}

// ErrUnsupported is returned by backends that cannot create monitors.
var ErrUnsupported = errors.New("this desktop cannot create virtual monitors; mirror mode still works")

// Detect returns the backend for the running session, plus a short note
// describing why it was picked (or what is missing).
func Detect() (Backend, string) {
	switch {
	case os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" && have("hyprctl"):
		return &Hyprland{}, "Hyprland session (hyprctl)"
	case os.Getenv("SWAYSOCK") != "" && have("swaymsg"):
		return &Sway{}, "Sway session (swaymsg)"
	case os.Getenv("WAYLAND_DISPLAY") != "":
		desk := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
		if strings.Contains(desk, "gnome") || strings.Contains(desk, "kde") {
			return &Unsupported{Desktop: os.Getenv("XDG_CURRENT_DESKTOP")},
				"GNOME and KDE Wayland need the screencast portal backend, which is not built yet"
		}
		return &Wlroots{}, "generic Wayland session: mirror only, through wlr-screencopy"
	case os.Getenv("DISPLAY") != "" && have("xrandr"):
		return &X11{}, "X11 session (xrandr)"
	}
	return &Unsupported{}, "no graphical session found (run inside your desktop session)"
}

// ByName returns a backend by name, for the --backend override.
func ByName(name string) (Backend, error) {
	switch name {
	case "hyprland":
		return &Hyprland{}, nil
	case "sway":
		return &Sway{}, nil
	case "x11":
		return &X11{}, nil
	case "wlroots":
		return &Wlroots{}, nil
	}
	return nil, fmt.Errorf("unknown backend %q (hyprland, sway, x11, wlroots)", name)
}

// Place computes where a new monitor of logical size w×h goes.
func Place(outs []Output, w, h int, side string) (int, int) {
	if len(outs) == 0 {
		return 0, 0
	}
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, o := range outs {
		minX, minY = min(minX, o.X), min(minY, o.Y)
		maxX, maxY = max(maxX, o.X+o.W), max(maxY, o.Y+o.H)
	}
	switch side {
	case "left":
		return minX - w, minY
	case "above":
		return minX, minY - h
	case "below":
		return minX, maxY
	}
	return maxX, minY
}

// Bounds is the bounding box of the layout, used to map the absolute pointer.
func Bounds(outs []Output) (x, y, w, h int) {
	if len(outs) == 0 {
		return 0, 0, 1, 1
	}
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, o := range outs {
		minX, minY = min(minX, o.X), min(minY, o.Y)
		maxX, maxY = max(maxX, o.X+o.W), max(maxY, o.Y+o.H)
	}
	return minX, minY, maxX - minX, maxY - minY
}

// LogicalSize converts a physical size and scale to layout units.
func LogicalSize(w, h int, scale float64) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	return int(math.Round(float64(w) / scale)), int(math.Round(float64(h) / scale))
}

func have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// waitFor polls until cond returns true or the timeout passes.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

// Unsupported is the backend for sessions with nothing to offer.
type Unsupported struct{ Desktop string }

func (u *Unsupported) Name() string                        { return "none" }
func (u *Unsupported) CanCreate() bool                     { return false }
func (u *Unsupported) Capture() CaptureKind                { return CaptureNone }
func (u *Unsupported) Outputs() ([]Output, error)          { return nil, nil }
func (u *Unsupported) Create(string, Spec) (string, error) { return "", ErrUnsupported }
func (u *Unsupported) Remove(string) error                 { return nil }
func (u *Unsupported) Enforce(string, Spec) error          { return nil }
