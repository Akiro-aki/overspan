package client

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Phase is where a connection is.
type Phase int

const (
	Connecting Phase = iota
	Waiting          // for someone at the computer to approve
	Starting         // the computer is creating or opening a display
	NeedPIN
	Failed // a final error; Retry or Back
	Live
)

// Status is everything the UI needs to draw the connection.
type Status struct {
	Phase    Phase
	Title    string
	Text     string
	Spin     bool
	Retry    bool
	PINError string
	Banner   string // shown over the picture: reconnecting, screen asleep
	Mode     string
	Input    bool
	Toast    string
	ToastSeq int
}

// Hello is what the UI tells the computer about this screen.
type Hello struct {
	Width, Height int
	Scale         float64
}

// Viewer runs one connection to a computer, with automatic reconnects
// after drops. All methods are safe from any goroutine.
type Viewer struct {
	Host, Name, Mode, Agent string
	Port                    int
	Prefs                   *Prefs
	DeviceName              string
	Size                    func() Hello // called on every (re)connect
	OnChange                func()       // status changed; called from any goroutine
	OnFrame                 func()       // a frame landed in Canvas

	Canvas Canvas
	Frames atomic.Int64
	Bytes  atomic.Int64

	mu        sync.Mutex
	st        Status
	conn      *Conn
	gen       int // bumps on every connect, so stale goroutines stay quiet
	pin       string
	lastErr   map[string]any
	retries   int
	userClose bool
	timer     *time.Timer
}

// Start connects.
func (v *Viewer) Start() {
	v.mu.Lock()
	v.st.Mode = v.Mode
	v.mu.Unlock()
	v.connect()
}

// Status returns a copy of the current status.
func (v *Viewer) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.st
}

func (v *Viewer) changed() {
	if v.OnChange != nil {
		v.OnChange()
	}
}

func (v *Viewer) set(f func(s *Status)) {
	v.mu.Lock()
	f(&v.st)
	v.mu.Unlock()
	v.changed()
}

func (v *Viewer) connect() {
	v.mu.Lock()
	if v.timer != nil {
		v.timer.Stop()
	}
	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}
	v.gen++
	gen := v.gen
	v.userClose = false
	v.lastErr = nil
	if v.st.Phase != Live {
		v.st = Status{Phase: Connecting, Title: TxtConnecting, Text: "Reaching " + v.Name + "…", Spin: true,
			Mode: v.st.Mode, Toast: v.st.Toast, ToastSeq: v.st.ToastSeq}
	}
	v.mu.Unlock()
	v.changed()
	go v.run(gen)
}

func (v *Viewer) current(gen int) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return gen == v.gen
}

func (v *Viewer) run(gen int) {
	c, err := Dial(v.Host, v.Port, v.Agent)
	if err != nil {
		v.closed(gen)
		return
	}
	v.mu.Lock()
	if gen != v.gen {
		v.mu.Unlock()
		c.Close()
		return
	}
	v.conn = c
	pin := v.pin
	v.mu.Unlock()

	sz := Hello{Width: 1280, Height: 720, Scale: 1}
	if v.Size != nil {
		sz = v.Size()
	}
	hello, _ := json.Marshal(map[string]any{
		"t": "hello", "device": v.Prefs.Device, "name": v.DeviceName, "mode": v.Mode,
		"width": sz.Width, "height": sz.Height, "scale": sz.Scale, "pin": pin,
		"quality": v.Prefs.Quality, "fps": v.Prefs.FPS,
	})
	c.WriteText(string(hello))

	for {
		op, data, err := c.Read()
		if err != nil {
			break
		}
		if !v.current(gen) {
			break
		}
		if op == OpBinary {
			if seq, ok := v.Canvas.Apply(data); ok {
				c.WriteText(`{"t":"ack","sq":` + strconv.FormatUint(uint64(seq), 10) + `}`)
				v.Frames.Add(1)
				v.Bytes.Add(int64(len(data)))
				if v.OnFrame != nil {
					v.OnFrame()
				}
			}
			continue
		}
		var m map[string]any
		if json.Unmarshal(data, &m) == nil {
			v.handle(c, m)
		}
	}
	c.Close()
	v.closed(gen)
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func (v *Viewer) handle(c *Conn, m map[string]any) {
	switch str(m, "t") {
	case "wait":
		v.set(func(s *Status) {
			*s = Status{Phase: Waiting, Title: TxtWaiting, Text: str(m, "text"), Spin: true, Mode: s.Mode, Toast: s.Toast, ToastSeq: s.ToastSeq}
		})
	case "setup":
		v.set(func(s *Status) {
			if s.Phase == Live {
				return
			}
			title, text := TxtSetupMirror, ""
			if str(m, "mode") == "extend" {
				title, text = TxtSetupExtend, TxtSetupExtendTx
			}
			*s = Status{Phase: Starting, Title: title, Text: text, Spin: true, Mode: str(m, "mode"), Toast: s.Toast, ToastSeq: s.ToastSeq}
		})
	case "error":
		v.mu.Lock()
		v.lastErr = m
		v.mu.Unlock()
	case "ready":
		input, _ := m["input"].(bool)
		v.mu.Lock()
		v.retries = 0
		if md := str(m, "mode"); md != "" {
			v.Mode = md
		}
		first := !v.Prefs.SeenHint
		v.st.Phase, v.st.Title, v.st.Text, v.st.Spin, v.st.Retry = Live, "", "", false, false
		v.st.Banner, v.st.Mode, v.st.Input = "", v.Mode, input
		switch {
		case !input:
			v.toastLocked(TxtInputOff)
		case first:
			v.toastLocked(TxtHint)
		}
		v.mu.Unlock()
		if first {
			v.Prefs.SeenHint = true
			v.Prefs.Save()
		}
		c.WriteText(`{"t":"refresh"}`)
		v.changed()
	case "input":
		on, _ := m["on"].(bool)
		v.mu.Lock()
		v.st.Input = on
		if on {
			v.toastLocked(TxtInputOn)
		} else {
			v.toastLocked(TxtInputOff)
		}
		v.mu.Unlock()
		v.changed()
	case "notice":
		v.set(func(s *Status) {
			if str(m, "code") == "asleep" {
				s.Banner = TxtAsleep
			} else {
				s.Banner = ""
			}
		})
	}
}

func (v *Viewer) toastLocked(t string) {
	v.st.Toast = t
	v.st.ToastSeq++
}

func (v *Viewer) closed(gen int) {
	v.mu.Lock()
	if gen != v.gen {
		v.mu.Unlock()
		return
	}
	v.conn = nil
	wasLive := v.st.Phase == Live
	if v.userClose {
		v.mu.Unlock()
		return
	}
	e := v.lastErr
	v.lastErr = nil
	if e != nil && wasLive && str(e, "code") == "shutdown" {
		e = nil // the computer restarts: keep trying
	}
	keep := func(s Status) Status { s.Mode, s.Toast, s.ToastSeq = v.st.Mode, v.st.Toast, v.st.ToastSeq; return s }
	switch {
	case e != nil && (str(e, "code") == "pin" || str(e, "code") == "pin-wrong"):
		wrong := ""
		if str(e, "code") == "pin-wrong" {
			wrong = TxtPINWrong
		}
		v.st = keep(Status{Phase: NeedPIN, Title: TxtPINTitle, Text: TxtPINText, PINError: wrong})
	case e != nil:
		v.Canvas.Clear()
		v.st = keep(Status{Phase: Failed, Title: errorTitle(str(e, "code")), Text: str(e, "text"), Retry: true})
	case wasLive && v.retries < 6:
		v.retries++
		v.st.Banner = TxtReconnecting
		delay := time.Duration(min(4000, 600*v.retries)) * time.Millisecond
		v.timer = time.AfterFunc(delay, func() {
			v.mu.Lock()
			ok := gen == v.gen && !v.userClose
			v.mu.Unlock()
			if ok {
				v.connect()
			}
		})
	default:
		v.Canvas.Clear()
		v.st = keep(Status{Phase: Failed, Title: TxtLost, Text: TxtLostText, Retry: true})
	}
	v.mu.Unlock()
	v.changed()
}

// SubmitPIN reconnects with a PIN.
func (v *Viewer) SubmitPIN(pin string) {
	v.mu.Lock()
	v.pin = strings.TrimSpace(pin)
	v.mu.Unlock()
	v.connect()
}

// Retry starts over after a failure.
func (v *Viewer) Retry() {
	v.mu.Lock()
	v.retries = 0
	v.st.Phase = Connecting
	v.mu.Unlock()
	v.connect()
}

// Close says goodbye and stops reconnecting.
func (v *Viewer) Close() {
	v.mu.Lock()
	v.userClose = true
	v.gen++
	if v.timer != nil {
		v.timer.Stop()
	}
	c := v.conn
	v.conn = nil
	v.mu.Unlock()
	if c != nil {
		c.WriteText(`{"t":"bye"}`)
		c.Close()
	}
}

// Send passes a message to the computer while live.
func (v *Viewer) Send(msg string) {
	v.mu.Lock()
	c := v.conn
	live := v.st.Phase == Live
	v.mu.Unlock()
	if c != nil && live {
		go c.WriteText(msg) // never block the UI thread on the network
	}
}

// sendSync is Send without the goroutine, for ordered input streams.
func (v *Viewer) sendSync(msg string) {
	v.mu.Lock()
	c := v.conn
	live := v.st.Phase == Live
	v.mu.Unlock()
	if c != nil && live {
		c.WriteText(msg)
	}
}

// Input messages go through one queue so they arrive in order without
// ever blocking the UI thread.
type Input struct {
	v *Viewer
	q chan string
}

// NewInput starts the input queue for v.
func NewInput(v *Viewer) *Input {
	in := &Input{v: v, q: make(chan string, 512)}
	go func() {
		for m := range in.q {
			v.sendSync(m)
		}
	}()
	return in
}

func (in *Input) push(m string) {
	select {
	case in.q <- m:
	default: // the link is stalled; drop rather than freeze
	}
}

func f5(x float64) string { return strconv.FormatFloat(x, 'f', 5, 64) }

// Pointer: e is move, down or up; x, y in 0..1; b 0 left, 1 middle, 2 right, 3 back, 4 forward.
func (in *Input) Pointer(e string, x, y float64, b int) {
	in.push(`{"t":"p","e":"` + e + `","x":` + f5(x) + `,"y":` + f5(y) + `,"b":` + strconv.Itoa(b) + `}`)
}

// Button presses or releases without moving.
func (in *Input) Button(b int, down bool) {
	in.push(`{"t":"btn","b":` + strconv.Itoa(b) + `,"d":` + strconv.FormatBool(down) + `}`)
}

// Wheel scrolls in 1/120 detents, positive is down / right.
func (in *Input) Wheel(dx, dy int) {
	in.push(`{"t":"w","dx":` + strconv.Itoa(dx) + `,"dy":` + strconv.Itoa(dy) + `}`)
}

// Key sends a physical key by its KeyboardEvent.code name.
func (in *Input) Key(code string, down bool) {
	in.push(`{"t":"k","c":"` + code + `","d":` + strconv.FormatBool(down) + `}`)
}

// Pen sends a stylus sample: e is hover, down, move, up or leave.
func (in *Input) Pen(e string, x, y, pressure, tiltX, tiltY float64, barrel, eraser bool) {
	b := 0
	if barrel {
		b = 1
	}
	in.push(fmt.Sprintf(`{"t":"pen","e":"%s","x":%s,"y":%s,"p":%s,"tx":%s,"ty":%s,"b":%d,"r":%t}`,
		e, f5(x), f5(y), f5(pressure), f5(tiltX), f5(tiltY), b, eraser))
}

// Config changes quality and frame rate live.
func (v *Viewer) Config(quality, fps int) {
	v.Send(fmt.Sprintf(`{"t":"cfg","q":%d,"f":%d}`, quality, fps))
}

// Resize asks for a new monitor size (extend mode).
func (v *Viewer) Resize(w, h int) {
	v.Send(fmt.Sprintf(`{"t":"resize","w":%d,"h":%d}`, w, h))
}
