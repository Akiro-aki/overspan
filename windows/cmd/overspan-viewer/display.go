//go:build windows

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"overspan/viewer/internal/client"
	"overspan/viewer/internal/win"
)

// D02–D04: connection status screens, the picture with its toolbar, and
// the picture-and-input settings.
type displayState struct {
	viewer *client.Viewer
	input  *client.Input
	target client.Computer
	st     client.Status

	framePending atomic.Bool
	sizeMu       sync.Mutex
	size         client.Hello

	fullscreen bool
	placement  win.WINDOWPLACEMENT
	style      uintptr
	pic        win.RECT // where the picture is drawn
	lastAsked  [2]int   // last monitor size sent, to skip no-op resizes

	toolbar    bool
	toolbarAt  time.Time
	tbFull     *ctl
	tbSettings *ctl
	tbClose    *ctl
	btnRetry   *ctl
	btnBack    *ctl
	pinEdit    *ctl
	btnPIN     *ctl

	toast    string
	toastSeq int
	stats    string
	frames   int64
	bytes    int64

	buttons int // mouse buttons held
	held    map[string]bool
	hook    uintptr
	edgeOn  bool

	// settings sheet working copy
	sRes     string
	sQuality int
	sFPS     int
	sFollow  bool
	sStats   bool
	sSegs    [3]*ctl
	sQ, sF   *ctl
	sTFollow *ctl
	sTStats  *ctl
}

var resValues = [3]string{"native", "balanced", "light"}

func (a *app) enterDisplay(target client.Computer, mode string) {
	a.leaveList()
	a.clearControls()
	a.page = pageDisplay
	d := &a.disp
	d.target = target
	d.held = map[string]bool{}
	d.toolbar = false
	d.buttons = 0
	d.toastSeq = 0
	d.lastAsked = [2]int{}
	a.setTitle(target.Title() + " — " + client.TxtAppName)

	d.tbFull = a.add(kToolbar, client.TxtTbFullscreen, func() { a.setFullscreen(!d.fullscreen); a.focusPicture() })
	d.tbSettings = a.add(kToolbar, client.TxtTbSettings, a.openSettings)
	d.tbClose = a.add(kToolbar, client.TxtTbDisconnect, a.openConfirm)
	d.pinEdit = a.add(kEdit, client.TxtPINLabel, nil)
	win.SendMessageW.Call(d.pinEdit.hwnd, win.EM_SETLIMITTEXT, 12, 0)
	d.btnPIN = a.add(kPrimary, client.TxtContinue, func() {
		d.viewer.SubmitPIN(win.Text(d.pinEdit.hwnd))
	})
	d.btnBack = a.add(kSecondary, client.TxtBack, a.leaveDisplay)
	d.btnRetry = a.add(kPrimary, client.TxtRetry, func() { d.viewer.Retry() })
	a.pageCtls = append([]*ctl(nil), a.ctls...)

	if a.prefs.Fullscreen {
		a.setFullscreen(true)
	}
	a.updateSize()

	v := &client.Viewer{
		Host: target.Host, Port: target.Port, Name: target.Title(), Mode: mode, Agent: userAgent(),
		Prefs: a.prefs, DeviceName: a.prefs.DeviceName(deviceFallbackName()),
	}
	v.Size = func() client.Hello {
		d.sizeMu.Lock()
		defer d.sizeMu.Unlock()
		return d.size
	}
	v.OnChange = func() { win.PostMessageW.Call(a.hwnd, wmStatus, 0, 0) }
	v.OnFrame = func() {
		if d.framePending.CompareAndSwap(false, true) {
			win.PostMessageW.Call(a.hwnd, wmFrame, 0, 0)
		}
	}
	d.viewer = v
	d.input = client.NewInput(v)
	d.st = client.Status{Phase: client.Connecting}
	win.SetTimer.Call(a.hwnd, tStats, 1000, 0)
	win.SetTimer.Call(a.hwnd, tToolbar, 400, 0)
	a.installHook()
	v.Start()
	a.statusChanged()
	a.focusPicture()
}

// leaveDisplay disconnects and goes back to the list.
func (a *app) leaveDisplay() {
	d := &a.disp
	a.releaseAll()
	if d.viewer != nil {
		d.viewer.Close()
	}
	a.removeHook()
	for _, t := range []uintptr{tStats, tToolbar, tResize, tToast, tEdge} {
		win.KillTimer.Call(a.hwnd, t)
	}
	a.setFullscreen(false)
	win.ReleaseCapture.Call()
	a.enterList()
}

func (a *app) focusPicture() { win.SetFocus.Call(a.hwnd) }

// updateSize records the monitor size to ask the computer for: the whole
// monitor in full screen, else the window's client area, capped by the
// resolution setting, with a desktop scale from this display's DPI.
func (a *app) updateSize() {
	d := &a.disp
	var w, h int32
	if d.fullscreen {
		m := a.monitorRect()
		w, h = m.W(), m.H()
	} else {
		cr := a.client()
		w, h = cr.W(), cr.H()
	}
	w, h = max(w, 320), max(h, 240)
	ratio := float64(a.th.dpi) / 96
	cap := map[string]int32{"native": 1 << 30, "balanced": 1920, "light": 1280}[a.prefs.Res]
	if long := max(w, h); long > cap {
		f := float64(cap) / float64(long)
		w, h = int32(float64(w)*f), int32(float64(h)*f)
		ratio *= f
	}
	scale := 1.0
	for _, s := range []float64{1, 1.25, 1.5, 2, 2.5, 3} {
		if s <= ratio+0.05 {
			scale = s
		}
	}
	d.sizeMu.Lock()
	d.size = client.Hello{Width: int(w), Height: int(h), Scale: scale}
	d.sizeMu.Unlock()
}

func (a *app) monitorRect() win.RECT {
	mon := win.MonitorFromWindow.Call(a.hwnd, win.MONITOR_DEFAULTTONEAREST)
	mi := win.MONITORINFO{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	win.GetMonitorInfoW.Call(mon, win.P(&mi))
	return mi.Monitor
}

func (a *app) setFullscreen(on bool) {
	d := &a.disp
	if on == d.fullscreen {
		return
	}
	if on {
		d.placement = win.WINDOWPLACEMENT{}
		d.placement.Length = uint32(unsafe.Sizeof(d.placement))
		win.GetWindowPlacement.Call(a.hwnd, win.P(&d.placement))
		d.style = win.GetWindowLongPtrW.Call(a.hwnd, win.GWL_STYLE)
		win.SetWindowLongPtrW.Call(a.hwnd, win.GWL_STYLE, (d.style&^win.WS_OVERLAPPEDWINDOW)|win.WS_POPUP)
		m := a.monitorRect()
		d.fullscreen = true
		win.SetWindowPos.Call(a.hwnd, win.HWND_TOP, uintptr(m.Left), uintptr(m.Top), uintptr(m.W()), uintptr(m.H()),
			win.SWP_FRAMECHANGED|win.SWP_NOOWNERZORDER)
	} else {
		d.fullscreen = false
		win.SetWindowLongPtrW.Call(a.hwnd, win.GWL_STYLE, d.style)
		win.SetWindowPlacement.Call(a.hwnd, win.P(&d.placement))
		win.SetWindowPos.Call(a.hwnd, 0, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED|win.SWP_NOOWNERZORDER)
	}
	if d.tbFull != nil {
		label := client.TxtTbFullscreen
		if d.fullscreen {
			label = client.TxtTbWindow
		}
		setText(d.tbFull, label)
	}
	a.relayout()
	a.displayResized()
}

// displayResized asks for a matching monitor after the window settles.
func (a *app) displayResized() {
	if a.disp.viewer == nil {
		return
	}
	win.SetTimer.Call(a.hwnd, tResize, 600, 0)
}

func (a *app) maybeResize() {
	d := &a.disp
	a.updateSize()
	if d.st.Phase != client.Live || d.st.Mode != "extend" {
		return
	}
	if !d.fullscreen && !a.prefs.FollowWindow {
		return
	}
	d.sizeMu.Lock()
	s := d.size
	d.sizeMu.Unlock()
	if [2]int{s.Width, s.Height} == d.lastAsked {
		return
	}
	cw, ch := d.viewer.Canvas.Size()
	d.lastAsked = [2]int{s.Width, s.Height}
	if abs(cw-s.Width) <= 8 && abs(ch-s.Height) <= 8 {
		return // already that size, give or take the computer's rounding
	}
	d.viewer.Resize(s.Width, s.Height)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (a *app) statusChanged() {
	d := &a.disp
	if a.page != pageDisplay || d.viewer == nil {
		return
	}
	old := d.st
	d.st = d.viewer.Status()
	if d.st.ToastSeq != d.toastSeq {
		d.toastSeq = d.st.ToastSeq
		d.toast = d.st.Toast
		win.SetTimer.Call(a.hwnd, tToast, 4500, 0)
	}
	if old.Phase != d.st.Phase {
		if d.st.Phase == client.Live {
			a.focusPicture()
			d.lastAsked = [2]int{}
			a.displayResized()
		} else {
			a.releaseAll()
			d.toolbar = false
			if a.sheet != sheetNone {
				a.closeSheet()
			}
		}
		if d.st.Phase == client.NeedPIN {
			win.SetWindowTextW.Call(d.pinEdit.hwnd, win.S(""))
		}
	}
	a.relayout()
	switch {
	case d.st.Phase == client.NeedPIN && old.Phase != client.NeedPIN:
		win.SetFocus.Call(d.pinEdit.hwnd)
	case d.st.Phase == client.Failed && old.Phase != client.Failed:
		win.SetFocus.Call(d.btnRetry.hwnd)
	}
}

func (a *app) frameArrived() {
	d := &a.disp
	d.framePending.Store(false)
	if d.pic.W() > 0 {
		win.InvalidateR(a.hwnd, d.pic)
	} else {
		a.relayout()
	}
	// the first frame or a size change moves the picture
	if cw, ch := d.viewer.Canvas.Size(); fitRect(a.client(), cw, ch) != d.pic {
		a.relayout()
	}
}

// fitRect centres a w×h picture in r, keeping its shape.
func fitRect(r win.RECT, w, h int) win.RECT {
	if w <= 0 || h <= 0 {
		return win.RECT{}
	}
	s := min(float64(r.W())/float64(w), float64(r.H())/float64(h))
	dw, dh := int32(float64(w)*s+0.5), int32(float64(h)*s+0.5)
	x, y := r.Left+(r.W()-dw)/2, r.Top+(r.H()-dh)/2
	return win.RECT{Left: x, Top: y, Right: x + dw, Bottom: y + dh}
}

func (a *app) layoutDisplay() {
	t := a.th
	c := t.c
	d := &a.disp
	cr := a.client()
	live := d.st.Phase == client.Live

	d.pic = win.RECT{}
	if d.viewer != nil {
		if cw, ch := d.viewer.Canvas.Size(); cw > 0 && live {
			d.pic = fitRect(cr, cw, ch)
			a.emit(op{k: oPicture, r: d.pic})
		}
	}

	// status panel
	for _, b := range []*ctl{d.btnRetry, d.btnBack, d.btnPIN} {
		a.hide(b)
	}
	a.hide(d.pinEdit)
	if !live && d.viewer != nil {
		st := d.st
		w := min(t.px(statusW), cr.W()-t.px(space4)*2)
		pad := t.px(space6)
		inner := w - pad*2
		_, textH := t.measure(st.Text, fSm, inner)
		_, errH := t.measure(st.PINError, fSm, inner)
		h := t.lineH(fLgBold)
		if st.Spin {
			h += t.px(spinSize) + t.px(space5)
		}
		if st.Text != "" {
			h += t.px(space2) + textH
		}
		if st.Phase == client.NeedPIN {
			h += t.px(space5) + t.px(controlH)
			if st.PINError != "" {
				h += t.px(space2) + errH
			}
		}
		if st.Retry {
			h += t.px(space5) + t.px(controlH)
		}
		box := rect((cr.W()-w)/2, max(t.px(space4), (cr.H()-h-pad*2)/2), w, h+pad*2)
		a.fill(box, c.Toolbar, t.px(radiusLg))
		y := box.Top + pad
		x := box.Left + pad
		if st.Spin {
			ss := t.px(spinSize)
			a.emit(op{k: oSpin, r: rect(box.Left+(w-ss)/2, y, ss, ss), c: c.Accent, c2: c.ToolbarHover, w: t.px(3)})
			y += ss + t.px(space5)
		}
		a.text(rect(x, y, inner, t.lineH(fLgBold)), st.Title, fLgBold, c.OnToolbar, win.DT_CENTER|win.DT_SINGLELINE)
		y += t.lineH(fLgBold)
		if st.Text != "" {
			y += t.px(space2)
			y += a.para(x, y, inner, st.Text, fSm, c.OnToolbarMuted, win.DT_CENTER)
		}
		if st.Phase == client.NeedPIN {
			y += t.px(space5)
			bw := a.buttonW(d.btnPIN.label)
			fieldW := min(inner-bw-t.px(space2), t.px(220))
			fx := box.Left + (w-fieldW-bw-t.px(space2))/2
			a.field(d.pinEdit, rect(fx, y, fieldW, t.px(controlH)), c.Surface)
			a.place(d.btnPIN, rect(fx+fieldW+t.px(space2)-t.px(focusWidth+space0/2), y, bw, t.px(controlH)), c.Toolbar)
			a.defID = d.btnPIN.id
			y += t.px(controlH)
			if st.PINError != "" {
				y += t.px(space2)
				y += a.para(x, y, inner, st.PINError, fSm, dark.Danger, win.DT_CENTER)
			}
		}
		if st.Retry {
			y += t.px(space5)
			bb, br := a.buttonW(d.btnBack.label), a.buttonW(d.btnRetry.label)
			bx := box.Left + (w-bb-br-t.px(space2))/2
			a.place(d.btnBack, rect(bx, y, bb, t.px(controlH)), c.Toolbar)
			a.place(d.btnRetry, rect(bx+bb+t.px(space2), y, br, t.px(controlH)), c.Toolbar)
			if st.Phase != client.NeedPIN {
				a.defID = d.btnRetry.id
			}
		}
	}

	// toolbar
	tbs := []*ctl{d.tbFull, d.tbSettings, d.tbClose}
	if live && d.toolbar && a.sheet == sheetNone {
		name := d.target.Title()
		nw, _ := t.measure(name, fSmBold, 0)
		nw = min(nw, t.px(220))
		bw := int32(0)
		for _, b := range tbs {
			bw += a.buttonW(b.label)
		}
		pad := t.px(space2)
		w := pad + nw + t.px(space4) + bw + pad
		bar := rect((cr.W()-w)/2, t.px(space2), w, t.px(toolbarH))
		a.fill(bar, c.Toolbar, t.px(radiusMd))
		a.text(rect(bar.Left+pad+t.px(space2), bar.Top, nw, bar.H()), name, fSmBold, c.OnToolbarMuted,
			win.DT_VCENTER|win.DT_SINGLELINE|win.DT_END_ELLIPSIS)
		x := bar.Left + pad + nw + t.px(space4)
		bh := t.px(toolbarBtn)
		for _, b := range tbs {
			bwi := a.buttonW(b.label)
			a.place(b, rect(x, bar.Top+(bar.H()-bh)/2, bwi, bh), c.Toolbar)
			x += bwi
		}
	} else {
		for _, b := range tbs {
			a.hide(b)
		}
	}

	// banner (reconnecting, asleep) under the toolbar area
	if d.st.Banner != "" && d.viewer != nil {
		a.pill(d.st.Banner, cr, t.px(toolbarH)+t.px(space4), c.Warn)
	}
	if d.toast != "" {
		a.pill(d.toast, cr, -t.px(space6), c.Accent)
	}
	if live && a.prefs.Stats && d.stats != "" {
		sw, sh := t.measure(d.stats, fXs, 0)
		r := rect(t.px(space3), cr.H()-sh-t.px(space2)*2-t.px(space3), sw+t.px(space3)*2, sh+t.px(space2)*2)
		a.fill(r, c.Toolbar, t.px(radiusSm))
		a.text(r, d.stats, fXs, c.OnToolbar, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE)
	}
}

// pill draws a centred message bubble at y (negative y counts from the bottom).
func (a *app) pill(s string, cr win.RECT, y int32, dot uint32) {
	t := a.th
	c := t.c
	maxW := min(t.px(560), cr.W()-t.px(space4)*2)
	ds := t.px(space2)
	tw, th := t.measure(s, fSm, maxW-t.px(space4)*2-ds-t.px(space2))
	w := tw + t.px(space4)*2 + ds + t.px(space2)
	h := th + t.px(space3)*2
	if y < 0 {
		y = cr.H() + y - h
	}
	r := rect((cr.W()-w)/2, y, w, h)
	a.fill(r, c.Toolbar, min(h/2, t.px(radiusLg)))
	a.emit(op{k: oDot, r: rect(r.Left+t.px(space4), r.Top+(h-ds)/2, ds, ds), c: dot})
	a.para(r.Left+t.px(space4)+ds+t.px(space2), r.Top+t.px(space3), tw, s, fSm, c.OnToolbar, 0)
}

func (a *app) showToolbar(on bool) {
	d := &a.disp
	if d.toolbar == on || d.st.Phase != client.Live {
		return
	}
	d.toolbar = on
	d.toolbarAt = time.Now()
	a.relayout()
	if !on && a.sheet == sheetNone {
		a.focusPicture()
	}
}

func (a *app) escapeDisplay() {
	d := &a.disp
	switch {
	case d.toolbar:
		a.showToolbar(false)
	case d.st.Phase != client.Live:
		a.leaveDisplay()
	}
}

func (a *app) displayTimer(id uintptr) {
	d := &a.disp
	switch id {
	case tResize:
		win.KillTimer.Call(a.hwnd, tResize)
		a.maybeResize()
	case tToast:
		win.KillTimer.Call(a.hwnd, tToast)
		d.toast = ""
		a.relayout()
	case tStats:
		if d.viewer == nil {
			return
		}
		fr, by := d.viewer.Frames.Swap(0), d.viewer.Bytes.Swap(0)
		cw, ch := d.viewer.Canvas.Size()
		s := fmt.Sprintf("%d×%d  %d fps  %d KB/s", cw, ch, fr, by/1024)
		if a.prefs.Stats && s != d.stats {
			d.stats = s
			a.relayout()
		}
	case tToolbar:
		if !d.toolbar || a.sheet != sheetNone {
			return
		}
		// keep it while the pointer or keyboard focus is on it
		f := win.GetFocus.Call()
		var pt win.POINT
		win.GetCursorPos.Call(win.P(&pt))
		win.ScreenToClient.Call(a.hwnd, win.P(&pt))
		if pt.Y >= 0 && pt.Y < a.th.px(toolbarH+space4) && win.GetForegroundWindow.Call() == a.hwnd {
			d.toolbarAt = time.Now()
		}
		for _, b := range []*ctl{d.tbFull, d.tbSettings, d.tbClose} {
			if f == b.hwnd || a.hover == b.hwnd {
				d.toolbarAt = time.Now()
			}
		}
		if time.Since(d.toolbarAt) > 2500*time.Millisecond {
			a.showToolbar(false)
		}
	case tEdge:
		win.KillTimer.Call(a.hwnd, tEdge)
		if d.edgeOn {
			a.showToolbar(true)
		}
	}
}

func (a *app) displayAfterSheet() {
	d := &a.disp
	d.toolbar = false
}

// ---------------------------------------------------------- settings sheet

func (a *app) openSettings() {
	d := &a.disp
	d.sRes, d.sQuality, d.sFPS = a.prefs.Res, a.prefs.Quality, a.prefs.FPS
	d.sFollow, d.sStats = a.prefs.FollowWindow, a.prefs.Stats
	a.releaseAll()
	a.openSheet(sheetSettings, func() {
		labels := [3]string{client.TxtResNative, client.TxtResBalanced, client.TxtResLight}
		for i := range d.sSegs {
			i := i
			d.sSegs[i] = a.add(kSegment, labels[i], func() { d.sRes = resValues[i]; a.syncSettings() })
		}
		d.sQ = a.add(kSlider, "", nil)
		win.SendMessageW.Call(d.sQ.hwnd, win.TBM_SETRANGE, 1, uintptr(30|95<<16))
		win.SendMessageW.Call(d.sQ.hwnd, win.TBM_SETLINESIZE, 0, 5)
		win.SendMessageW.Call(d.sQ.hwnd, win.TBM_SETPAGESIZE, 0, 10)
		win.SendMessageW.Call(d.sQ.hwnd, win.TBM_SETPOS, 1, uintptr(d.sQuality))
		d.sF = a.add(kSlider, "", nil)
		win.SendMessageW.Call(d.sF.hwnd, win.TBM_SETRANGE, 1, uintptr(10|60<<16))
		win.SendMessageW.Call(d.sF.hwnd, win.TBM_SETLINESIZE, 0, 5)
		win.SendMessageW.Call(d.sF.hwnd, win.TBM_SETPAGESIZE, 0, 10)
		win.SendMessageW.Call(d.sF.hwnd, win.TBM_SETPOS, 1, uintptr(d.sFPS))
		d.sTFollow = a.add(kToggle, client.TxtFollow, func() { d.sFollow = !d.sFollow; a.syncSettings() })
		d.sTStats = a.add(kToggle, client.TxtStats, func() { d.sStats = !d.sStats; a.syncSettings() })
		done := a.add(kPrimary, client.TxtDone, a.applySettings)
		a.defID = done.id
	})
	a.syncSettings()
}

func (a *app) syncSettings() {
	d := &a.disp
	for i, s := range d.sSegs {
		s.on = resValues[i] == d.sRes
	}
	d.sTFollow.on, d.sTStats.on = d.sFollow, d.sStats
	a.relayout()
}

func (a *app) settingsSliderMoved(c *ctl) {
	d := &a.disp
	v := int(win.SendMessageW.Call(c.hwnd, win.TBM_GETPOS, 0, 0))
	if c == d.sQ {
		d.sQuality = v - v%5
	} else if c == d.sF {
		d.sFPS = v - v%5
	}
	a.relayout()
}

func (a *app) applySettings() {
	d := &a.disp
	oldRes := a.prefs.Res
	a.prefs.Res, a.prefs.Quality, a.prefs.FPS = d.sRes, max(30, d.sQuality), max(10, d.sFPS)
	a.prefs.FollowWindow, a.prefs.Stats = d.sFollow, d.sStats
	a.prefs.Save()
	d.viewer.Config(a.prefs.Quality, a.prefs.FPS)
	a.closeSheet()
	if oldRes != a.prefs.Res {
		d.lastAsked = [2]int{}
		a.maybeResize()
	}
	a.focusPicture()
}

func (a *app) layoutSettings() {
	t := a.th
	c := t.c
	d := &a.disp
	cw := min(t.px(sheetW), a.client().W()-t.px(space4)*2) - t.px(space5)*2
	lab := t.lineH(fSmBold)
	ctlH := t.px(controlH)
	_, tipH := t.measure(client.TxtResTip, fSm, cw)
	_, noteH := t.measure(client.TxtWinKeys, fXs, cw)
	h := t.lineH(fLgBold) + t.px(space5) +
		lab + t.px(space2) + ctlH + t.px(space1) + tipH + t.px(space4) +
		(lab+ctlH+t.px(space3))*2 +
		(ctlH+t.px(space1))*2 + t.px(space3) + noteH + t.px(space5) + ctlH
	_, in := a.sheetCard(h)
	x, y, w := in.Left, in.Top, in.W()
	a.text(rect(x, y, w, t.lineH(fLgBold)), client.TxtSettings, fLgBold, c.Text, win.DT_SINGLELINE)
	y += t.lineH(fLgBold) + t.px(space5)
	a.text(rect(x, y, w, lab), client.TxtResolution, fSmBold, c.Text, win.DT_SINGLELINE)
	y += lab + t.px(space2)
	sw := (w - t.px(space2)*2) / 3
	for i, s := range d.sSegs {
		a.place(s, rect(x+int32(i)*(sw+t.px(space2)), y, sw, ctlH), c.Surface)
	}
	y += ctlH + t.px(space1)
	y += a.para(x, y, w, client.TxtResTip, fSm, c.Muted, 0) + t.px(space4)
	slider := func(label string, s *ctl) {
		a.text(rect(x, y, w, lab), label, fSmBold, c.Text, win.DT_SINGLELINE)
		y += lab
		a.place(s, rect(x-t.px(space1), y, w+t.px(space2), ctlH), c.Surface)
		y += ctlH + t.px(space3)
	}
	slider(fmt.Sprintf(client.TxtQualityLabel, d.sQuality), d.sQ)
	slider(fmt.Sprintf(client.TxtFPSLabel, d.sFPS), d.sF)
	a.place(d.sTFollow, rect(x-t.px(space2), y, w+t.px(space2)*2, ctlH), c.Surface)
	y += ctlH + t.px(space1)
	a.place(d.sTStats, rect(x-t.px(space2), y, w+t.px(space2)*2, ctlH), c.Surface)
	y += ctlH + t.px(space1) + t.px(space3)
	y += a.para(x, y, w, client.TxtWinKeys, fXs, c.Faint, 0) + t.px(space5)
	a.buttonRow(x, y, w, nil, a.sheetCtls[len(a.sheetCtls)-1])
}

// ----------------------------------------------------------- confirm sheet

func (a *app) openConfirm() {
	a.releaseAll()
	a.openSheet(sheetConfirm, func() {
		a.add(kSecondary, client.TxtCancel, a.closeSheet)
		yes := a.add(kDanger, client.TxtTbDisconnect, func() { a.closeSheet(); a.leaveDisplay() })
		a.defID = yes.id
	})
	// focus the safe choice first
	win.SetFocus.Call(a.sheetCtls[0].hwnd)
}

func (a *app) layoutConfirm() {
	t := a.th
	c := t.c
	cw := min(t.px(sheetW), a.client().W()-t.px(space4)*2) - t.px(space5)*2
	q := fmt.Sprintf(client.TxtDisconnectQ, a.disp.target.Title())
	_, qh := t.measure(q, fMdBold, cw)
	_, in := a.sheetCard(qh + t.px(space5) + t.px(controlH))
	a.para(in.Left, in.Top, in.W(), q, fMdBold, c.Text, 0)
	a.buttonRow(in.Left, in.Top+qh+t.px(space5), in.W(), nil, a.sheetCtls[0], a.sheetCtls[1])
}

// ---------------------------------------------------------------- input

// forwarding says whether input goes to the computer right now.
func (a *app) forwarding() bool {
	d := &a.disp
	return a.page == pageDisplay && d.st.Phase == client.Live && a.sheet == sheetNone && d.viewer != nil
}

// norm maps client coordinates onto the picture, 0..1.
func (a *app) norm(x, y int32) (float64, float64, bool) {
	p := a.disp.pic
	if p.W() <= 0 || p.H() <= 0 {
		return 0, 0, false
	}
	nx := float64(x-p.Left) / float64(p.W())
	ny := float64(y-p.Top) / float64(p.H())
	inside := nx >= 0 && nx <= 1 && ny >= 0 && ny <= 1
	return min(1, max(0, nx)), min(1, max(0, ny)), inside
}

func (a *app) releaseAll() {
	d := &a.disp
	if d.input == nil {
		return
	}
	for k := range d.held {
		d.input.Key(k, false)
	}
	d.held = map[string]bool{}
	for b := 0; b < 5; b++ {
		if d.buttons&(1<<b) != 0 {
			d.input.Button(b, false)
		}
	}
	d.buttons = 0
}

// displayInput handles mouse, keyboard and pen messages on the display page.
// ok=false passes the message on to the normal handler.
func (a *app) displayInput(m uint32, w, l uintptr) (uintptr, bool) {
	d := &a.disp
	switch m {
	case win.WM_SETCURSOR:
		if win.LOWORD(l) == win.HTCLIENT && a.forwarding() {
			var pt win.POINT
			win.GetCursorPos.Call(win.P(&pt))
			win.ScreenToClient.Call(a.hwnd, win.P(&pt))
			if _, _, in := a.norm(pt.X, pt.Y); in {
				win.SetCursor.Call(0) // the computer draws its own pointer into the picture
				return 1, true
			}
		}
	case win.WM_KILLFOCUS, win.WM_ACTIVATEAPP:
		if m == win.WM_ACTIVATEAPP && w != 0 {
			break
		}
		a.releaseAll()
	case win.WM_MOUSEMOVE:
		x, y := win.XY(l)
		if d.st.Phase == client.Live && a.sheet == sheetNone {
			atEdge := y <= a.th.px(edgeZone)
			if atEdge && !d.edgeOn {
				win.SetTimer.Call(a.hwnd, tEdge, 350, 0)
			}
			d.edgeOn = atEdge
			if d.toolbar && y < a.th.px(toolbarH+space4) {
				d.toolbarAt = time.Now()
			}
		}
		if !a.forwarding() {
			break
		}
		if nx, ny, in := a.norm(x, y); in || d.buttons != 0 {
			d.input.Pointer("move", nx, ny, 0)
		}
		return 0, true
	case win.WM_LBUTTONDOWN, win.WM_LBUTTONDBLCLK, win.WM_RBUTTONDOWN, win.WM_MBUTTONDOWN, win.WM_XBUTTONDOWN,
		win.WM_LBUTTONUP, win.WM_RBUTTONUP, win.WM_MBUTTONUP, win.WM_XBUTTONUP:
		if !a.forwarding() {
			if m == win.WM_LBUTTONDOWN {
				a.focusPicture()
			}
			break
		}
		a.focusPicture()
		if d.toolbar {
			a.showToolbar(false)
		}
		b, down := 0, false
		switch m {
		case win.WM_LBUTTONDOWN, win.WM_LBUTTONDBLCLK:
			b, down = 0, true
		case win.WM_LBUTTONUP:
			b = 0
		case win.WM_RBUTTONDOWN:
			b, down = 2, true
		case win.WM_RBUTTONUP:
			b = 2
		case win.WM_MBUTTONDOWN:
			b, down = 1, true
		case win.WM_MBUTTONUP:
			b = 1
		case win.WM_XBUTTONDOWN, win.WM_XBUTTONUP:
			b, down = 3, m == win.WM_XBUTTONDOWN
			if win.HIWORD(w) != win.XBUTTON1 {
				b = 4
			}
		}
		x, y := win.XY(l)
		nx, ny, in := a.norm(x, y)
		if down && !in {
			return 0, true
		}
		if down {
			d.buttons |= 1 << b
			win.SetCapture.Call(a.hwnd)
			d.input.Pointer("down", nx, ny, b)
		} else if d.buttons&(1<<b) != 0 {
			d.buttons &^= 1 << b
			d.input.Pointer("up", nx, ny, b)
			if d.buttons == 0 {
				win.ReleaseCapture.Call()
			}
		}
		return 0, true
	case win.WM_MOUSEWHEEL, win.WM_MOUSEHWHEEL:
		if !a.forwarding() {
			return 0, true
		}
		delta := int(int16(win.HIWORD(w)))
		if m == win.WM_MOUSEWHEEL {
			d.input.Wheel(0, -delta)
		} else {
			d.input.Wheel(delta, 0)
		}
		return 0, true
	case win.WM_CAPTURECHANGED:
		if d.buttons != 0 && l != a.hwnd {
			a.releaseAll()
		}
	case win.WM_KEYDOWN, win.WM_SYSKEYDOWN, win.WM_KEYUP, win.WM_SYSKEYUP:
		down := m == win.WM_KEYDOWN || m == win.WM_SYSKEYDOWN
		// Alt+Shift+O: the toolbar, for keyboard users (never forwarded)
		if down && w == 'O' && keyHeld(win.VK_MENU) && keyHeld(win.VK_SHIFT) && d.st.Phase == client.Live {
			a.releaseAll()
			a.showToolbar(!d.toolbar)
			if d.toolbar {
				win.SetFocus.Call(d.tbFull.hwnd)
			}
			return 0, true
		}
		if win.GetFocus.Call() != a.hwnd {
			break
		}
		if !a.forwarding() {
			if down && w == win.VK_ESCAPE {
				a.escapeDisplay()
				return 0, true
			}
			break
		}
		code := client.KeyCode(uint32(w), l)
		if code == "" {
			return 0, true
		}
		if down {
			if d.held[code] {
				return 0, true // auto-repeat: the computer repeats keys itself
			}
			d.held[code] = true
		} else {
			if !d.held[code] {
				return 0, true
			}
			delete(d.held, code)
		}
		d.input.Key(code, down)
		return 0, true // also keeps Alt and F10 from opening the window menu
	case win.WM_SYSCHAR, win.WM_CHAR:
		if a.forwarding() && win.GetFocus.Call() == a.hwnd {
			return 0, true
		}
	case win.WM_SYSCOMMAND:
		if w&0xfff0 == win.SC_KEYMENU && a.forwarding() {
			return 0, true
		}
	case win.WM_POINTERDOWN, win.WM_POINTERUPDATE, win.WM_POINTERUP, win.WM_POINTERLEAVE:
		return a.penMessage(m, w)
	}
	return 0, false
}

func keyHeld(vk uintptr) bool { return win.GetKeyState.Call(vk)&0x8000 != 0 }

// penMessage forwards a pen (stylus) with pressure and tilt. Touch and mouse
// pointers fall through to Windows, which turns them into mouse messages.
func (a *app) penMessage(m uint32, w uintptr) (uintptr, bool) {
	if !win.GetPointerType.Available() || !a.forwarding() {
		return 0, false
	}
	id := uintptr(win.LOWORD(w))
	var typ uint32
	if win.GetPointerType.Call(id, win.P(&typ)) == 0 || typ != win.PT_PEN {
		return 0, false
	}
	var pi win.POINTER_PEN_INFO
	if win.GetPointerPenInfo.Call(id, win.P(&pi)) == 0 {
		return 0, false
	}
	pt := pi.PointerInfo.PtPixelLocation
	win.ScreenToClient.Call(a.hwnd, win.P(&pt))
	nx, ny, in := a.norm(pt.X, pt.Y)
	flags := pi.PointerInfo.PointerFlags
	contact := flags&win.POINTER_FLAG_INCONTACT != 0
	e := "hover"
	switch {
	case m == win.WM_POINTERLEAVE || flags&win.POINTER_FLAG_INRANGE == 0:
		e = "leave"
	case m == win.WM_POINTERDOWN:
		e = "down"
	case m == win.WM_POINTERUP:
		e = "up"
	case contact:
		e = "move"
	}
	if !in && e == "hover" {
		e = "leave"
	}
	p := 0.0
	if pi.PenMask&win.PEN_MASK_PRESSURE != 0 {
		p = float64(pi.Pressure) / 1024
	} else if contact {
		p = 0.5
	}
	eraser := pi.PenFlags&(win.PEN_FLAG_ERASER|win.PEN_FLAG_INVERTED) != 0
	a.focusPicture()
	a.disp.input.Pen(e, nx, ny, p, float64(pi.TiltX), float64(pi.TiltY), pi.PenFlags&win.PEN_FLAG_BARREL != 0, eraser)
	return 0, true
}

// The Windows key: a low-level hook passes it to the computer while the
// picture has focus (Hyprland and many Linux desktops use it constantly).
// Windows keeps Win+L and Ctrl+Alt+Del no matter what.
var hookCB uintptr

func (a *app) installHook() {
	if a.disp.hook != 0 {
		return
	}
	if hookCB == 0 {
		hookCB = newHookCallback()
	}
	a.disp.hook = win.SetWindowsHookExW.Call(win.WH_KEYBOARD_LL, hookCB, a.inst, 0)
}

func (a *app) removeHook() {
	if a.disp.hook != 0 {
		win.UnhookWindowsHookEx.Call(a.disp.hook)
		a.disp.hook = 0
	}
}

func hookProc(code int, w, l uintptr) uintptr {
	a := the
	if code == win.HC_ACTION && a != nil && a.forwarding() &&
		win.GetForegroundWindow.Call() == a.hwnd && win.GetFocus.Call() == a.hwnd {
		k := (*win.KBDLLHOOKSTRUCT)(unsafe.Pointer(l))
		if k.VkCode == win.VK_LWIN || k.VkCode == win.VK_RWIN {
			name := "MetaLeft"
			if k.VkCode == win.VK_RWIN {
				name = "MetaRight"
			}
			down := k.Flags&win.LLKHF_UP == 0
			d := &a.disp
			if down != d.held[name] {
				if down {
					d.held[name] = true
				} else {
					delete(d.held, name)
				}
				d.input.Key(name, down)
			}
			return 1
		}
	}
	return win.CallNextHookEx.Call(0, uintptr(code), w, l)
}
