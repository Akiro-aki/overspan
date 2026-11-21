//go:build windows

// winprobe drives the Overspan viewer from outside, for automated UI tests
// (on Windows or under Wine). It finds controls by their text, as a person
// reads them, and acts through ordinary window messages.
//
//	winprobe list
//	winprobe wait <text> [ms]         wait until a visible control or the title contains text
//	winprobe gone <text> [ms]         wait until no visible control has text
//	winprobe click <text>             click the visible, enabled control whose text starts with text
//	winprobe settext <n> <text>       set the nth visible text box (0-based)
//	winprobe mouse <move|down|up|rdown|rup> <x> <y>   in client coordinates
//	winprobe key <vk> <scan> [ext] [up]
//	winprobe size <w> <h>             resize the window (outer size, device pixels)
//	winprobe shot <path.png>          needs the viewer started with OVERSPAN_TEST_HOOKS=1
//	winprobe close
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"overspan/viewer/internal/win"
)

type control struct {
	Text    string `json:"text"`
	Class   string `json:"class"`
	X       int32  `json:"x"`
	Y       int32  `json:"y"`
	W       int32  `json:"w"`
	H       int32  `json:"h"`
	Visible bool   `json:"visible"`
	Enabled bool   `json:"enabled"`
	hwnd    uintptr
}

func mainWindow() uintptr {
	for i := 0; i < 100; i++ {
		if h := win.FindWindowW.Call(win.S("OverspanViewer"), 0); h != 0 {
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	fail("viewer window not found")
	return 0
}

func controls(top uintptr) []control {
	var out []control
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		r := win.WindowRect(h)
		pt := win.POINT{X: r.Left, Y: r.Top}
		win.ScreenToClient.Call(top, win.P(&pt))
		out = append(out, control{
			Text: win.Text(h), Class: win.ClassName(h), X: pt.X, Y: pt.Y, W: r.W(), H: r.H(),
			Visible: win.IsWindowVisible.Call(h) != 0, Enabled: win.IsWindowEnabled.Call(h) != 0, hwnd: h,
		})
		return 1
	})
	win.EnumChildWindows.Call(top, cb, 0)
	return out
}

func find(top uintptr, text string, needEnabled bool) *control {
	t := strings.ToLower(text)
	for _, c := range controls(top) {
		if c.Visible && (!needEnabled || c.Enabled) && strings.HasPrefix(strings.ToLower(c.Text), t) {
			return &c
		}
	}
	return nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "winprobe:", msg)
	os.Exit(1)
}

func arg(i int) string {
	if len(os.Args) <= i {
		fail("missing argument")
	}
	return os.Args[i]
}

func num(i int, def int) int {
	if len(os.Args) <= i {
		return def
	}
	n, err := strconv.Atoi(os.Args[i])
	if err != nil {
		fail("not a number: " + os.Args[i])
	}
	return n
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: winprobe list|wait|gone|click|settext|mouse|key|shot|close ...")
	}
	if os.Args[1] == "close" { // only an existing window; never wait for one
		if h := win.FindWindowW.Call(win.S("OverspanViewer"), 0); h != 0 {
			win.PostMessageW.Call(h, win.WM_CLOSE, 0, 0)
		}
		return
	}
	top := mainWindow()
	switch os.Args[1] {
	case "list":
		cr := win.ClientRect(top)
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"title": win.Text(top), "w": cr.W(), "h": cr.H(), "controls": controls(top),
		})
	case "wait", "gone":
		text, ms := arg(2), num(3, 15000)
		end := time.Now().Add(time.Duration(ms) * time.Millisecond)
		for time.Now().Before(end) {
			present := find(top, text, false) != nil || strings.Contains(strings.ToLower(win.Text(top)), strings.ToLower(text))
			if present == (os.Args[1] == "wait") {
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
		fail(os.Args[1] + " timed out: " + text)
	case "click":
		c := find(top, arg(2), true)
		if c == nil {
			fail("no visible, enabled control: " + arg(2))
		}
		win.SendMessageW.Call(c.hwnd, win.BM_CLICK, 0, 0)
	case "settext":
		n := num(2, 0)
		for _, c := range controls(top) {
			if c.Visible && strings.EqualFold(c.Class, "Edit") {
				if n == 0 {
					win.SendMessageW.Call(c.hwnd, win.WM_SETTEXT, 0, win.S(arg(3)))
					return
				}
				n--
			}
		}
		fail("no such text box")
	case "mouse":
		x, y := num(3, 0), num(4, 0)
		l := uintptr(uint16(x)) | uintptr(uint16(y))<<16
		msgs := map[string][2]uintptr{
			"move": {win.WM_MOUSEMOVE, 0}, "down": {win.WM_LBUTTONDOWN, win.MK_LBUTTON}, "up": {win.WM_LBUTTONUP, 0},
			"rdown": {win.WM_RBUTTONDOWN, win.MK_RBUTTON}, "rup": {win.WM_RBUTTONUP, 0},
		}
		m, ok := msgs[arg(2)]
		if !ok {
			fail("unknown mouse action")
		}
		win.SendMessageW.Call(top, m[0], m[1], l)
	case "key":
		vk, scan := num(2, 0), num(3, 0)
		l := uintptr(1) | uintptr(scan)<<16
		rest := strings.Join(os.Args[4:], " ")
		if strings.Contains(rest, "ext") {
			l |= 1 << 24
		}
		m := uintptr(win.WM_KEYDOWN)
		if strings.Contains(rest, "up") {
			m = win.WM_KEYUP
			l |= 3 << 30
		}
		win.SendMessageW.Call(top, m, uintptr(vk), l)
	case "size":
		win.SetWindowPos.Call(top, 0, 0, 0, uintptr(num(2, 800)), uintptr(num(3, 600)), win.SWP_NOMOVE|win.SWP_NOZORDER)
	case "shot":
		path := arg(2)
		u, _ := syscall.UTF16FromString(path)
		cd := win.COPYDATASTRUCT{Data: 1, Size: uint32(len(u) * 2), Ptr: uintptr(unsafe.Pointer(&u[0]))}
		if win.SendMessageW.Call(top, win.WM_COPYDATA, 0, win.P(&cd)) != 1 {
			fail("snapshot refused (start the viewer with OVERSPAN_TEST_HOOKS=1)")
		}
	default:
		fail("unknown command " + os.Args[1])
	}
}
