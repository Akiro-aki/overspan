package server

import (
	"os"
	"os/exec"
	"strings"

	"overspan/internal/vdisplay"
	"overspan/internal/wl"
)

// Check is one line of the system check.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Doctor inspects the session and reports what works.
func (s *Server) Doctor() []Check {
	return RunDoctor(s.backend, s.note, s.injErr)
}

// RunDoctor is shared by the dashboard and `overspan doctor`.
func RunDoctor(b vdisplay.Backend, note string, injErr error) []Check {
	var cs []Check
	sess := os.Getenv("XDG_SESSION_TYPE")
	desk := os.Getenv("XDG_CURRENT_DESKTOP")
	if sess == "" {
		sess = "unknown"
	}
	cs = append(cs, Check{Name: "Session", Status: "ok", Detail: sess + " / " + orDash(desk)})

	st := "ok"
	fix := ""
	if !b.CanCreate() {
		st = "warn"
		fix = "Extend mode needs Hyprland, Sway or X11 today. Mirror mode works where screen capture does."
	}
	if b.Name() == "none" {
		st = "fail"
	}
	cs = append(cs, Check{Name: "Virtual monitors", Status: st, Detail: b.Name() + ": " + note, Fix: fix})

	switch b.Capture() {
	case vdisplay.CaptureScreencopy:
		if sc, err := wl.NewScreencopy(); err != nil {
			cs = append(cs, Check{Name: "Screen capture", Status: "fail", Detail: err.Error(),
				Fix: "The compositor must offer wlr-screencopy (Hyprland, Sway, river, labwc, niri, Wayfire do)."})
		} else {
			outs, _ := sc.Outputs()
			sc.Close()
			names := make([]string, 0, len(outs))
			for _, o := range outs {
				names = append(names, o.Name)
			}
			cs = append(cs, Check{Name: "Screen capture", Status: "ok", Detail: "wlr-screencopy, outputs: " + strings.Join(names, ", ")})
		}
	case vdisplay.CaptureX11:
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			cs = append(cs, Check{Name: "Screen capture", Status: "fail", Detail: "ffmpeg not found", Fix: "Install ffmpeg from your distribution's packages."})
		} else {
			cs = append(cs, Check{Name: "Screen capture", Status: "ok", Detail: "ffmpeg x11grab"})
		}
	default:
		cs = append(cs, Check{Name: "Screen capture", Status: "fail", Detail: "no capture method for this session"})
	}

	if injErr != nil {
		cs = append(cs, Check{Name: "Touch, mouse and keyboard", Status: "warn", Detail: injErr.Error(),
			Fix: "Run: overspan install-udev   (or: sudo usermod -aG input $USER, then log out and in)"})
	} else {
		cs = append(cs, Check{Name: "Touch, mouse and keyboard", Status: "ok", Detail: "/dev/uinput is writable"})
	}

	if fw := firewallHint(); fw != "" {
		cs = append(cs, Check{Name: "Firewall", Status: "warn", Detail: "a firewall is active and may block viewers", Fix: fw})
	} else {
		cs = append(cs, Check{Name: "Firewall", Status: "ok", Detail: "no active firewall detected"})
	}
	return cs
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firewallHint() string {
	if out, err := exec.Command("systemctl", "is-active", "firewalld").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		return "sudo firewall-cmd --permanent --add-port=7940/tcp --add-port=7941/udp && sudo firewall-cmd --reload"
	}
	if out, err := exec.Command("systemctl", "is-active", "ufw").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		return "sudo ufw allow 7940/tcp && sudo ufw allow 7941/udp   (7941/udp lets the Android app find this computer)"
	}
	return ""
}
