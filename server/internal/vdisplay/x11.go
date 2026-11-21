package vdisplay

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"overspan/internal/wl"
)

// X11 works on any X server through xrandr. Drivers that expose VIRTUAL
// outputs (intel, amdgpu/modesetting with VirtualHeads) get a real output
// with its own mode. Everything else gets the framebuffer widened and a
// RandR "monitor" carved out of the new space, which desktops treat as a
// separate screen for window placement and panels.
type X11 struct {
	created map[string]x11Made
}

type x11Made struct {
	viaVirtual bool
	mode       string
	oldFB      [2]int
}

func (x *X11) Name() string         { return "x11" }
func (x *X11) CanCreate() bool      { return true }
func (x *X11) Capture() CaptureKind { return CaptureX11 }

var (
	reMonitor = regexp.MustCompile(`^\s*\d+:\s+[+*]*(\S+)\s+(\d+)/\d+x(\d+)/\d+\+(-?\d+)\+(-?\d+)`)
	reOutput  = regexp.MustCompile(`^(\S+) (connected|disconnected)`)
	reScreen  = regexp.MustCompile(`current (\d+) x (\d+)`)
)

func (x *X11) Outputs() ([]Output, error) {
	raw, err := run("xrandr", "--listmonitors")
	if err != nil {
		return nil, err
	}
	var outs []Output
	for _, line := range strings.Split(raw, "\n") {
		m := reMonitor.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		px, _ := strconv.Atoi(m[4])
		py, _ := strconv.Atoi(m[5])
		outs = append(outs, Output{
			Name: m[1], X: px, Y: py, W: w, H: h, PxW: w, PxH: h, Scale: 1,
			Virtual: strings.HasPrefix(m[1], "OVERSPAN") || strings.HasPrefix(m[1], "VIRTUAL"),
			Primary: strings.Contains(line, "*"),
		})
	}
	return outs, nil
}

func (x *X11) screen() (int, int, string, error) {
	raw, err := run("xrandr", "--query")
	if err != nil {
		return 0, 0, "", err
	}
	m := reScreen.FindStringSubmatch(raw)
	if m == nil {
		return 0, 0, "", fmt.Errorf("cannot read the X screen size")
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	virtual := ""
	for _, line := range strings.Split(raw, "\n") {
		if o := reOutput.FindStringSubmatch(line); o != nil && o[2] == "disconnected" && strings.HasPrefix(strings.ToUpper(o[1]), "VIRTUAL") {
			virtual = o[1]
			break
		}
	}
	return w, h, virtual, nil
}

// Modeline computes a CVT reduced-blanking modeline, so no external `cvt`
// binary is needed.
func Modeline(w, h, refresh int) (clockMHz float64, timings []int) {
	const hBlank, vFront, vSync, minVBlankUs = 160, 3, 6, 460.0
	hTotal := w + hBlank
	vBlankLines := int(math.Ceil(minVBlankUs / (1e6 / float64(refresh) / float64(h+20))))
	vTotal := h + max(vBlankLines, vFront+vSync+6)
	clock := float64(hTotal*vTotal*refresh) / 1e6
	clock = math.Floor(clock/0.25) * 0.25
	return clock, []int{w, w + 48, w + 80, hTotal, h, h + vFront, h + vFront + vSync, vTotal}
}

func (x *X11) Create(id string, s Spec) (string, error) {
	if x.created == nil {
		x.created = map[string]x11Made{}
	}
	outs, err := x.Outputs()
	if err != nil {
		return "", err
	}
	fbW, fbH, virtual, err := x.screen()
	if err != nil {
		return "", err
	}
	px, py := Place(outs, s.Width, s.Height, s.Place)
	if px < 0 || py < 0 {
		px, py = Place(outs, s.Width, s.Height, "right") // X cannot use negative space
	}
	if virtual != "" {
		refresh := max(s.Refresh, 30)
		clock, t := Modeline(s.Width, s.Height, refresh)
		mode := fmt.Sprintf("overspan_%dx%d", s.Width, s.Height)
		args := []string{"--newmode", mode, fmt.Sprintf("%.2f", clock)}
		for _, v := range t {
			args = append(args, strconv.Itoa(v))
		}
		args = append(args, "+hsync", "-vsync")
		run("xrandr", args...) // fails harmlessly if it already exists
		if _, err := run("xrandr", "--addmode", virtual, mode); err == nil {
			if _, err := run("xrandr", "--output", virtual, "--mode", mode, "--pos", fmt.Sprintf("%dx%d", px, py)); err == nil {
				x.created[virtual] = x11Made{viaVirtual: true, mode: mode}
				return virtual, nil
			}
		}
	}
	// fallback: widen the framebuffer and declare a monitor in the new area
	name := "OVERSPAN-" + id
	newW, newH := max(fbW, px+s.Width), max(fbH, py+s.Height)
	if _, err := run("xrandr", "--fb", fmt.Sprintf("%dx%d", newW, newH)); err != nil {
		return "", fmt.Errorf("could not enlarge the X screen: %w", err)
	}
	geom := fmt.Sprintf("%d/%dx%d/%d+%d+%d", s.Width, s.Width*254/960, s.Height, s.Height*254/960, px, py)
	if _, err := run("xrandr", "--setmonitor", name, geom, "none"); err != nil {
		run("xrandr", "--fb", fmt.Sprintf("%dx%d", fbW, fbH))
		return "", err
	}
	x.created[name] = x11Made{oldFB: [2]int{fbW, fbH}}
	return name, nil
}

func (x *X11) Enforce(string, Spec) error { return nil }

func (x *X11) Remove(name string) error {
	made, ok := x.created[name]
	if !ok {
		return nil
	}
	delete(x.created, name)
	if made.viaVirtual {
		run("xrandr", "--output", name, "--off")
		run("xrandr", "--delmode", name, made.mode)
		_, err := run("xrandr", "--rmmode", made.mode)
		return err
	}
	run("xrandr", "--delmonitor", name)
	_, err := run("xrandr", "--fb", fmt.Sprintf("%dx%d", made.oldFB[0], made.oldFB[1]))
	return err
}

// wlOutputs lists outputs over the Wayland protocol, laid out in a row.
func wlOutputs() ([]Output, error) {
	s, err := wl.NewScreencopy()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	infos, err := s.Outputs()
	if err != nil {
		return nil, err
	}
	var outs []Output
	x := 0
	for i, o := range infos {
		outs = append(outs, Output{Name: o.Name, X: x, W: o.Width, H: o.Height, PxW: o.Width, PxH: o.Height, Scale: 1, Primary: i == 0})
		x += o.Width
	}
	return outs, nil
}
