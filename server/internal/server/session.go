package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"overspan/internal/capture"
	"overspan/internal/encode"
	"overspan/internal/input"
	"overspan/internal/vdisplay"
	"overspan/internal/ws"
)

// Viewer is one connected device.
type Viewer struct {
	s        *Server
	ID       string
	DeviceID string
	Name     string
	Addr     string
	Agent    string
	Mode     string
	Since    time.Time

	conn     *ws.Conn
	decision chan bool
	done     chan struct{}
	stopOnce sync.Once

	mu         sync.Mutex
	state      string // waiting | starting | live
	output     string
	spec       vdisplay.Spec
	ownDisplay bool
	pxW, pxH   int
	pos        [2]float64 // trackpad-mode cursor, normalised

	quality  atomic.Int32
	fps      atomic.Int32
	refresh  atomic.Bool
	credits  chan struct{}
	frames   atomic.Int64
	bytes    atomic.Int64
	statFPS  atomic.Int64 // ×100
	statKBps atomic.Int64
	lastAt   atomic.Int64 // unix nano of last frame
}

// ViewerDTO is the dashboard's view of a viewer.
type ViewerDTO struct {
	ID       string    `json:"id"`
	DeviceID string    `json:"deviceId"`
	Name     string    `json:"name"`
	Addr     string    `json:"addr"`
	Agent    string    `json:"agent"`
	Mode     string    `json:"mode"`
	State    string    `json:"state"`
	Output   string    `json:"output"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
	Since    time.Time `json:"since"`
	FPS      float64   `json:"fps"`
	KBps     int64     `json:"kbps"`
	Frames   int64     `json:"frames"`
}

func (v *Viewer) dto() ViewerDTO {
	v.mu.Lock()
	defer v.mu.Unlock()
	fps := float64(v.statFPS.Load()) / 100
	kbps := v.statKBps.Load()
	if time.Since(time.Unix(0, v.lastAt.Load())) > 1500*time.Millisecond {
		fps, kbps = 0, 0
	}
	return ViewerDTO{
		ID: v.ID, DeviceID: v.DeviceID, Name: v.Name, Addr: v.Addr, Agent: v.Agent,
		Mode: v.Mode, State: v.state, Output: v.output, Width: v.pxW, Height: v.pxH,
		Since: v.Since, FPS: fps, KBps: kbps, Frames: v.frames.Load(),
	}
}

type hello struct {
	T       string  `json:"t"`
	Device  string  `json:"device"`
	Name    string  `json:"name"`
	Mode    string  `json:"mode"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Scale   float64 `json:"scale"`
	PIN     string  `json:"pin"`
	Quality int     `json:"quality"`
	FPS     int     `json:"fps"`
	Output  string  `json:"output"`
}

type inMsg struct {
	T  string  `json:"t"`
	E  string  `json:"e"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	DX float64 `json:"dx"`
	DY float64 `json:"dy"`
	B  int     `json:"b"`
	C  string  `json:"c"`
	D  bool    `json:"d"`
	S  string  `json:"s"`
	Q  int     `json:"q"`
	F  int     `json:"f"`
	W  int     `json:"w"`
	H  int     `json:"h"`
	Sq uint32  `json:"sq"`
}

func (v *Viewer) send(msg any) error {
	b, _ := json.Marshal(msg)
	return v.conn.Write(ws.OpText, b)
}

func fail(c *ws.Conn, code, text string) {
	b, _ := json.Marshal(map[string]string{"t": "error", "code": code, "text": text})
	c.Write(ws.OpText, b)
	c.Close()
}

func cleanName(s string, def string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		return def
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

// even rounds down to a multiple of 8, which every compositor accepts.
func even8(v int) int { return max(320, v-v%8) }

func (s *Server) handleViewer(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	op, raw, err := conn.Read()
	if err != nil || op != ws.OpText {
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})
	var h hello
	if json.Unmarshal(raw, &h) != nil || h.T != "hello" || len(h.Device) < 8 || len(h.Device) > 64 {
		fail(conn, "protocol", "This viewer speaks a different protocol version. Reload the page.")
		return
	}

	s.mu.Lock()
	st := s.settings
	s.mu.Unlock()
	if !st.Enabled {
		fail(conn, "disabled", "The server is paused and is not accepting viewers right now.")
		return
	}
	if st.PIN != "" && h.PIN != st.PIN {
		code := "pin"
		if h.PIN != "" {
			code = "pin-wrong"
			s.logf("warn", "Wrong PIN from %s", r.RemoteAddr)
		}
		fail(conn, code, "This server asks for a PIN.")
		return
	}
	mode := h.Mode
	if mode != "mirror" && mode != "extend" {
		mode = st.DefaultMode
	}
	if mode == "extend" && !s.backend.CanCreate() {
		mode = "mirror"
	}

	v := &Viewer{
		s: s, ID: randomHex(4), DeviceID: h.Device, Name: cleanName(h.Name, "Unnamed device"),
		Addr: r.RemoteAddr, Agent: cleanName(r.UserAgent(), ""), Mode: mode, Since: time.Now(),
		conn: conn, decision: make(chan bool, 1), done: make(chan struct{}),
		state: "waiting", credits: make(chan struct{}, 2),
	}
	v.Agent = shortAgent(v.Agent)
	v.credits <- struct{}{}
	v.credits <- struct{}{}
	v.quality.Store(int32(pick(h.Quality, st.Quality, 20, 95)))
	v.fps.Store(int32(pick(h.FPS, st.FPS, 5, 60)))

	// size the virtual monitor from the viewer's screen, capped
	wpx, hpx := max(h.Width, 320), max(h.Height, 240)
	if long := max(wpx, hpx); long > st.MaxLongEdge {
		f := float64(st.MaxLongEdge) / float64(long)
		wpx, hpx = int(float64(wpx)*f), int(float64(hpx)*f)
	}
	scale := h.Scale
	if scale <= 0 || scale > 3 {
		scale = 1
	}
	v.spec = vdisplay.Spec{Width: even8(wpx), Height: even8(hpx), Refresh: 60, Scale: scale, Place: st.Placement}

	s.mu.Lock()
	s.viewers[v.ID] = v
	s.mu.Unlock()
	s.logf("info", "%s connected from %s (%s mode)", v.Name, hostOnly(v.Addr), mode)
	go v.readLoop()
	defer s.finish(v)

	// approval
	if st.RequireApproval && !s.isTrusted(v.DeviceID) {
		v.send(map[string]any{"t": "wait", "text": "Waiting for someone at the computer to accept this device."})
		select {
		case ok := <-v.decision:
			if !ok {
				s.logf("info", "Declined %s", v.Name)
				v.sendErr("denied", "The computer declined this connection.")
				return
			}
		case <-time.After(2 * time.Minute):
			v.sendErr("timeout", "Nobody answered the request on the computer in time.")
			return
		case <-v.done:
			return
		}
	}

	v.setState("starting")
	v.send(map[string]any{"t": "setup", "mode": v.Mode})
	if err := v.attachDisplay(h.Output); err != nil {
		s.logf("error", "Could not set up a display for %s: %v", v.Name, err)
		v.sendErr("display", "The computer could not create a display: "+err.Error())
		return
	}
	v.mu.Lock()
	out := v.output
	v.mu.Unlock()
	o, _ := s.findOutput(out, true)
	v.setSize(o.PxW, o.PxH)
	v.send(map[string]any{
		"t": "ready", "mode": v.Mode, "id": v.ID,
		"display": map[string]any{"name": out, "w": o.PxW, "h": o.PxH},
		"input":   st.AllowInput && s.inj != nil, "server": st.ServerName,
	})
	v.setState("live")
	s.logf("info", "Streaming %s to %s", out, v.Name)

	go v.keepDisplay()
	v.streamLoop()
}

func pick(asked, def, lo, hi int) int {
	if asked == 0 {
		asked = def
	}
	return max(lo, min(hi, asked))
}

func hostOnly(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return strings.Trim(addr[:i], "[]")
	}
	return addr
}

func shortAgent(ua string) string {
	switch {
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	}
	return "Browser"
}

func (v *Viewer) sendErr(code, text string) {
	v.send(map[string]string{"t": "error", "code": code, "text": text})
}

func (v *Viewer) setState(st string) {
	v.mu.Lock()
	v.state = st
	v.mu.Unlock()
	v.s.notify()
}

func (v *Viewer) setSize(w, h int) {
	v.mu.Lock()
	v.pxW, v.pxH = w, h
	v.mu.Unlock()
}

func (v *Viewer) decide(ok bool) {
	select {
	case v.decision <- ok:
	default:
	}
}

func (v *Viewer) stop(reason string) {
	v.stopOnce.Do(func() {
		if reason != "" {
			v.sendErr("closed", reason)
		}
		close(v.done)
		v.conn.Close()
	})
}

func (v *Viewer) applySettings(st Settings) {
	if !st.AllowInput && v.s.inj != nil {
		v.s.inj.ReleaseAll()
	}
	v.send(map[string]any{"t": "input", "on": st.AllowInput && v.s.inj != nil})
}

// findOutput looks a monitor up in the layout.
func (s *Server) findOutput(name string, force bool) (vdisplay.Output, bool) {
	for _, o := range s.Layout(force) {
		if o.Name == name {
			return o, true
		}
	}
	return vdisplay.Output{}, false
}

func (v *Viewer) attachDisplay(asked string) error {
	s := v.s
	if v.Mode == "mirror" {
		outs := s.Layout(true)
		if len(outs) == 0 {
			return errors.New("no monitors found")
		}
		s.mu.Lock()
		want := s.settings.MirrorOutput
		s.mu.Unlock()
		if asked != "" {
			want = asked
		}
		chosen := outs[0]
		for _, o := range outs {
			if o.Primary && !o.Virtual {
				chosen = o
			}
		}
		for _, o := range outs {
			if o.Name == want {
				chosen = o
			}
		}
		v.mu.Lock()
		v.output = chosen.Name
		v.mu.Unlock()
		return nil
	}
	// extend: reuse the monitor kept from this device's last session
	s.mu.Lock()
	k := s.kept[v.DeviceID]
	if k != nil {
		delete(s.kept, v.DeviceID)
		k.timer.Stop()
	}
	s.mu.Unlock()
	if k != nil {
		if _, ok := s.findOutput(k.output, true); ok {
			spec := v.spec
			spec.Place = k.spec.Place
			if err := s.backend.Enforce(k.output, spec); err == nil {
				v.mu.Lock()
				v.output, v.spec, v.ownDisplay = k.output, spec, true
				v.mu.Unlock()
				s.logf("info", "Reusing %s for %s", k.output, v.Name)
				return nil
			}
		}
		s.backend.Remove(k.output)
	}
	name, err := s.backend.Create(v.ID, v.spec)
	if err != nil {
		return err
	}
	if o, ok := s.findOutput(name, true); ok {
		v.spec.Place = fmt.Sprintf("%d,%d", o.X, o.Y)
	}
	v.mu.Lock()
	v.output, v.ownDisplay = name, true
	v.mu.Unlock()
	s.logf("info", "Created virtual monitor %s (%dx%d)", name, v.spec.Width, v.spec.Height)
	return nil
}

// keepDisplay re-applies our mode if a config reload or a monitor daemon
// resets it, and keeps the link alive with pings.
func (v *Viewer) keepDisplay() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-v.done:
			return
		case <-t.C:
		}
		v.conn.Ping()
		v.mu.Lock()
		own, out, spec := v.ownDisplay, v.output, v.spec
		v.mu.Unlock()
		if own {
			if err := v.s.backend.Enforce(out, spec); err != nil {
				v.s.logf("warn", "%s: %v", out, err)
			}
		}
		v.s.Layout(true)
	}
}

func (s *Server) finish(v *Viewer) {
	v.stop("")
	if s.inj != nil {
		s.inj.ReleaseAll()
	}
	s.mu.Lock()
	delete(s.viewers, v.ID)
	keep := time.Duration(s.settings.KeepSeconds) * time.Second
	s.mu.Unlock()
	v.mu.Lock()
	own, out, spec := v.ownDisplay, v.output, v.spec
	v.mu.Unlock()
	if own {
		if keep > 0 {
			kd := &keptDisplay{output: out, spec: spec}
			kd.timer = time.AfterFunc(keep, func() {
				s.mu.Lock()
				cur := s.kept[v.DeviceID]
				if cur == kd {
					delete(s.kept, v.DeviceID)
				}
				s.mu.Unlock()
				if cur == kd {
					s.backend.Remove(out)
					s.Layout(true)
					s.logf("info", "Removed virtual monitor %s", out)
				}
			})
			s.mu.Lock()
			if old := s.kept[v.DeviceID]; old != nil {
				old.timer.Stop()
				go s.backend.Remove(old.output)
			}
			s.kept[v.DeviceID] = kd
			s.mu.Unlock()
		} else {
			s.backend.Remove(out)
			s.logf("info", "Removed virtual monitor %s", out)
		}
	}
	s.logf("info", "%s disconnected", v.Name)
}

func (v *Viewer) streamLoop() {
	s := v.s
	s.mu.Lock()
	cursor := s.settings.ShowCursor
	s.mu.Unlock()
	v.mu.Lock()
	outName := v.output
	v.mu.Unlock()
	o, _ := s.findOutput(outName, false)

	var capr capture.Capturer
	var err error
	openCap := func() error {
		capr, err = capture.Open(s.backend.Capture(), o, int(v.fps.Load()), cursor)
		return err
	}
	if err := openCap(); err != nil {
		s.logf("error", "Capture failed for %s: %v", outName, err)
		v.sendErr("capture", "The computer could not capture the screen: "+err.Error())
		return
	}
	go func() { <-v.done; capr.Close() }()

	enc := encode.New(int(v.quality.Load()))
	var seq uint32
	var winFrames, winBytes int64
	winStart := time.Now()
	full := true
	failures := 0
	for {
		select {
		case <-v.done:
			return
		case <-v.credits:
		case <-time.After(2 * time.Second): // a lost ack must not stall the stream
		}
		start := time.Now()
		if v.refresh.Swap(false) {
			full = true
		}
		raw, err := capr.Next(full)
		if err != nil {
			select {
			case <-v.done:
				return
			default:
			}
			failures++
			if failures > 20 {
				s.logf("error", "Capture of %s keeps failing: %v", outName, err)
				v.sendErr("capture", "Lost the picture from the computer.")
				v.stop("")
				return
			}
			time.Sleep(250 * time.Millisecond)
			capr.Close()
			o, _ = s.findOutput(outName, true)
			if openCap() == nil {
				go func(c capture.Capturer) { <-v.done; c.Close() }(capr)
			}
			full = true
			continue
		}
		failures = 0
		enc.Quality = int(v.quality.Load())
		v.mu.Lock()
		changed := raw.Width != v.pxW || raw.Height != v.pxH
		v.mu.Unlock()
		if changed {
			v.setSize(raw.Width, raw.Height)
			v.send(map[string]any{"t": "size", "w": raw.Width, "h": raw.Height})
			full = true
		}
		patches, err := enc.Encode(raw, full)
		if err != nil {
			s.logf("error", "Encoding failed: %v", err)
			continue
		}
		if len(patches) == 0 {
			v.credits <- struct{}{} // nothing sent, nothing to ack
			time.Sleep(time.Second / time.Duration(v.fps.Load()))
			continue
		}
		seq++
		msg := encode.Pack(seq, raw.Width, raw.Height, full, patches)
		full = false
		if err := v.conn.Write(ws.OpBinary, msg); err != nil {
			v.stop("")
			return
		}
		v.frames.Add(1)
		v.bytes.Add(int64(len(msg)))
		v.lastAt.Store(time.Now().UnixNano())
		winFrames++
		winBytes += int64(len(msg))
		if el := time.Since(winStart); el >= time.Second {
			v.statFPS.Store(int64(float64(winFrames) / el.Seconds() * 100))
			v.statKBps.Store(int64(float64(winBytes) / el.Seconds() / 1024))
			winFrames, winBytes, winStart = 0, 0, time.Now()
		}
		if wait := time.Second/time.Duration(v.fps.Load()) - time.Since(start); wait > 0 {
			time.Sleep(wait)
		}
	}
}

func (v *Viewer) readLoop() {
	defer v.stop("")
	for {
		op, data, err := v.conn.Read()
		if err != nil {
			return
		}
		if op != ws.OpText {
			continue
		}
		var m inMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		v.handle(&m)
	}
}

func (v *Viewer) inputAllowed() bool {
	v.mu.Lock()
	live := v.state == "live"
	v.mu.Unlock()
	v.s.mu.Lock()
	on := v.s.settings.AllowInput
	v.s.mu.Unlock()
	return live && on && v.s.inj != nil
}

func (v *Viewer) handle(m *inMsg) {
	s := v.s
	switch m.T {
	case "ack":
		select {
		case v.credits <- struct{}{}:
		default:
		}
		return
	case "refresh":
		v.refresh.Store(true)
		return
	case "cfg":
		if m.Q > 0 {
			v.quality.Store(int32(max(20, min(95, m.Q))))
		}
		if m.F > 0 {
			v.fps.Store(int32(max(5, min(60, m.F))))
		}
		return
	case "resize":
		v.resize(m.W, m.H)
		return
	case "bye":
		v.stop("")
		return
	}
	if !v.inputAllowed() {
		return
	}
	inj := s.inj
	switch m.T {
	case "p": // absolute pointer, x/y normalised to the display
		x, y := clamp01(m.X), clamp01(m.Y)
		v.mu.Lock()
		v.pos = [2]float64{x, y}
		v.mu.Unlock()
		v.moveTo(x, y)
		if m.E == "down" || m.E == "up" {
			inj.Button(button(m.B), m.E == "down")
		}
	case "rel": // trackpad mode, deltas in display pixels
		v.mu.Lock()
		w, h := float64(max(v.pxW, 1)), float64(max(v.pxH, 1))
		v.pos[0] = clamp01(v.pos[0] + m.DX/w)
		v.pos[1] = clamp01(v.pos[1] + m.DY/h)
		x, y := v.pos[0], v.pos[1]
		v.mu.Unlock()
		v.moveTo(x, y)
	case "btn":
		inj.Button(button(m.B), m.D)
	case "w":
		inj.Wheel(int32(m.DX), int32(m.DY))
	case "k":
		inj.Key(input.KeyCode(m.C), m.D)
	case "txt":
		for _, r := range []rune(m.S) {
			if len(m.S) > 256 {
				break
			}
			code, shift := input.CharKey(r)
			if code == 0 {
				continue
			}
			if shift {
				inj.Key(42, true)
			}
			inj.Key(code, true)
			inj.Key(code, false)
			if shift {
				inj.Key(42, false)
			}
		}
	}
}

func button(b int) int {
	switch b {
	case 1:
		return input.BtnMiddle
	case 2:
		return input.BtnRight
	case 3:
		return input.BtnSide
	case 4:
		return input.BtnExtra
	}
	return input.BtnLeft
}

func clamp01(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	return math.Max(0, math.Min(1, f))
}

// moveTo maps a point on this viewer's display into global layout space and
// then onto the absolute pointer's 0..65535 range.
func (v *Viewer) moveTo(nx, ny float64) {
	v.mu.Lock()
	name := v.output
	v.mu.Unlock()
	layout := v.s.Layout(false)
	var out vdisplay.Output
	found := false
	for _, o := range layout {
		if o.Name == name {
			out, found = o, true
		}
	}
	if !found {
		return
	}
	bx, by, bw, bh := vdisplay.Bounds(layout)
	// keep strictly inside the monitor so the last pixel does not spill over
	gx := float64(out.X) + nx*float64(out.W-1)
	gy := float64(out.Y) + ny*float64(out.H-1)
	ax := (gx - float64(bx)) / float64(max(bw-1, 1)) * input.AbsMax
	ay := (gy - float64(by)) / float64(max(bh-1, 1)) * input.AbsMax
	v.s.inj.MoveAbs(int32(math.Round(ax)), int32(math.Round(ay)))
}

// resize follows the viewer when it rotates or changes window size.
func (v *Viewer) resize(w, h int) {
	v.mu.Lock()
	own := v.ownDisplay
	spec := v.spec
	name := v.output
	v.mu.Unlock()
	if !own || w < 320 || h < 240 {
		return
	}
	v.s.mu.Lock()
	maxEdge := v.s.settings.MaxLongEdge
	v.s.mu.Unlock()
	if long := max(w, h); long > maxEdge {
		f := float64(maxEdge) / float64(long)
		w, h = int(float64(w)*f), int(float64(h)*f)
	}
	spec.Width, spec.Height = even8(w), even8(h)
	if spec.Width == v.spec.Width && spec.Height == v.spec.Height {
		return
	}
	go func() {
		if err := v.s.backend.Enforce(name, spec); err != nil {
			v.s.logf("warn", "Could not resize %s: %v", name, err)
			return
		}
		v.mu.Lock()
		v.spec = spec
		v.mu.Unlock()
		v.s.Layout(true)
		v.refresh.Store(true)
		v.s.logf("info", "Resized %s to %dx%d", name, spec.Width, spec.Height)
	}()
}
