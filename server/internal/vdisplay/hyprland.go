package vdisplay

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Hyprland creates headless outputs through hyprctl. Newer Hyprland (0.5x
// with the Lua config) takes monitor rules through `hyprctl eval`; older
// releases through `hyprctl keyword monitor`. Both are tried.
type Hyprland struct {
	useLua *bool
}

func (h *Hyprland) Name() string         { return "hyprland" }
func (h *Hyprland) CanCreate() bool      { return true }
func (h *Hyprland) Capture() CaptureKind { return CaptureScreencopy }

type hyprMonitor struct {
	Name      string  `json:"name"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	X         int     `json:"x"`
	Y         int     `json:"y"`
	Scale     float64 `json:"scale"`
	Transform int     `json:"transform"`
	Disabled  bool    `json:"disabled"`
	Focused   bool    `json:"focused"`
}

func (h *Hyprland) Outputs() ([]Output, error) {
	raw, err := run("hyprctl", "monitors", "-j")
	if err != nil {
		return nil, err
	}
	var ms []hyprMonitor
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		return nil, fmt.Errorf("hyprctl monitors: %w", err)
	}
	var outs []Output
	for _, m := range ms {
		if m.Disabled {
			continue
		}
		pw, ph := m.Width, m.Height
		if m.Transform%2 == 1 {
			pw, ph = ph, pw
		}
		lw, lh := LogicalSize(pw, ph, m.Scale)
		outs = append(outs, Output{
			Name: m.Name, X: m.X, Y: m.Y, W: lw, H: lh, PxW: m.Width, PxH: m.Height,
			Scale: m.Scale, Virtual: strings.HasPrefix(m.Name, "OVERSPAN") || strings.HasPrefix(m.Name, "HEADLESS"),
			Primary: m.X == 0 && m.Y == 0,
		})
	}
	return outs, nil
}

func (h *Hyprland) find(name string) (Output, bool) {
	outs, err := h.Outputs()
	if err != nil {
		return Output{}, false
	}
	for _, o := range outs {
		if o.Name == name {
			return o, true
		}
	}
	return Output{}, false
}

func (h *Hyprland) Create(id string, s Spec) (string, error) {
	name := "OVERSPAN-" + id
	before, err := h.Outputs()
	if err != nil {
		return "", err
	}
	if out, err := run("hyprctl", "output", "create", "headless", name); err != nil || !strings.Contains(out, "ok") {
		return "", fmt.Errorf("hyprctl output create: %v %s", err, strings.TrimSpace(out))
	}
	if !waitFor(3*time.Second, func() bool { _, ok := h.find(name); return ok }) {
		return "", fmt.Errorf("headless output %s never appeared", name)
	}
	// Hotplug handlers (monitor daemons) often reload the config right after
	// an output appears (and may switch other panels on or off), which would
	// wipe our rule. Wait until the layout stops changing, then place the new
	// monitor against what is really there.
	_ = before
	h.settle(5 * time.Second)
	var others []Output
	outs, _ := h.Outputs()
	for _, o := range outs {
		if o.Name != name {
			others = append(others, o)
		}
	}
	s.Place = placeKey(others, s)
	for attempt := 0; attempt < 3; attempt++ {
		if err = h.apply(name, s); err != nil {
			break
		}
		// a late reload can still revert us; check once more
		time.Sleep(1500 * time.Millisecond)
		if o, ok := h.find(name); ok && o.PxW == s.Width && o.PxH == s.Height {
			return name, nil
		}
	}
	h.Remove(name)
	if err == nil {
		err = fmt.Errorf("something keeps resetting %s (a monitor daemon?)", name)
	}
	return "", err
}

// settle waits until the monitor list has been unchanged for 1.5 seconds.
func (h *Hyprland) settle(max time.Duration) {
	deadline := time.Now().Add(max)
	last, _ := run("hyprctl", "monitors", "-j")
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		cur, _ := run("hyprctl", "monitors", "-j")
		if cur != last {
			last, stableSince = cur, time.Now()
			continue
		}
		if time.Since(stableSince) >= 1500*time.Millisecond {
			return
		}
	}
}

// placeKey freezes the computed position into the spec so Enforce keeps it.
func placeKey(others []Output, s Spec) string {
	if strings.Contains(s.Place, ",") {
		return s.Place
	}
	lw, lh := LogicalSize(s.Width, s.Height, s.Scale)
	x, y := Place(others, lw, lh, s.Place)
	return fmt.Sprintf("%d,%d", x, y)
}

func (h *Hyprland) apply(name string, s Spec) error {
	var x, y int
	fmt.Sscanf(s.Place, "%d,%d", &x, &y)
	refresh := s.Refresh
	if refresh <= 0 {
		refresh = 60
	}
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	mode := fmt.Sprintf("%dx%d@%d", s.Width, s.Height, refresh)
	sc := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", scale), "0"), ".")
	tryLua := h.useLua == nil || *h.useLua
	if h.useLua == nil || !*h.useLua {
		out, err := run("hyprctl", "keyword", "monitor", fmt.Sprintf("%s,%s,%dx%d,%s", name, mode, x, y, sc))
		if err == nil && strings.Contains(out, "ok") {
			f := false
			h.useLua = &f
			tryLua = false
		}
	}
	if tryLua {
		lua := fmt.Sprintf(`hl.monitor({ output = %q, mode = %q, position = "%dx%d", scale = %s })`, name, mode, x, y, sc)
		out, err := run("hyprctl", "eval", lua)
		if err != nil || !strings.Contains(out, "ok") {
			return fmt.Errorf("could not set the monitor mode: %v %s", err, strings.TrimSpace(out))
		}
		t := true
		h.useLua = &t
	}
	ok := waitFor(2*time.Second, func() bool {
		o, found := h.find(name)
		return found && o.PxW == s.Width && o.PxH == s.Height
	})
	if !ok {
		return fmt.Errorf("Hyprland did not accept %s for %s", mode, name)
	}
	return nil
}

func (h *Hyprland) Enforce(name string, s Spec) error {
	o, ok := h.find(name)
	if !ok {
		return fmt.Errorf("%s is gone", name)
	}
	var x, y int
	fmt.Sscanf(s.Place, "%d,%d", &x, &y)
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	// Scale is not compared: Hyprland may round a requested scale to one that
	// divides the mode evenly, and fighting it would loop forever.
	_ = math.Abs(o.Scale - scale)
	if o.PxW == s.Width && o.PxH == s.Height && o.X == x && o.Y == y {
		return nil
	}
	return h.apply(name, s)
}

func (h *Hyprland) Remove(name string) error {
	_, err := run("hyprctl", "output", "remove", name)
	return err
}
