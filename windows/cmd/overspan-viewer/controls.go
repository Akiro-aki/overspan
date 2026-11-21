//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"overspan/viewer/internal/win"
)

// Interactive elements are real child windows (owner-drawn BUTTONs, EDITs
// and trackbars), so Tab, Space, Enter and screen readers work as on any
// Windows dialog, and the window text names each control.
type kind int

const (
	kPrimary kind = iota
	kSecondary
	kDanger     // filled, for destructive confirmations
	kDangerText // text only, for Forget
	kCard
	kSegment
	kToggle
	kToolbar
	kEdit
	kSlider
)

type ctl struct {
	id      int
	hwnd    uintptr
	kind    kind
	label   string // also the window text, minus card details
	sub     string // card: address
	badge   string // card: Ready / Paused / Saved
	tone    int    // card badge tone: 0 plain, 1 ok, 2 warn
	on      bool   // segment selected, toggle on
	bg      uint32 // colour behind the control
	dimmed  bool   // behind an open sheet
	onClick func()
	r       win.RECT
	shown   bool
}

var (
	btnClass   = win.Str("BUTTON")
	editClass  = win.Str("EDIT")
	trackClass = win.Str("msctls_trackbar32")
	oldBtnProc uintptr
	btnProcCB  = syscall.NewCallback(btnProc)
)

func (a *app) add(k kind, label string, onClick func()) *ctl {
	a.nextID++
	c := &ctl{id: a.nextID, kind: k, label: label, onClick: onClick}
	style := uintptr(win.WS_CHILD | win.WS_TABSTOP)
	var cls *uint16
	switch k {
	case kEdit:
		cls = editClass
		style |= win.ES_AUTOHSCROLL
	case kSlider:
		cls = trackClass
		style |= win.TBS_HORZ | win.TBS_NOTICKS | win.TBS_BOTH
	default:
		cls = btnClass
		style |= win.BS_OWNERDRAW | win.BS_NOTIFY
	}
	text := label
	if k == kEdit {
		text = ""
	}
	c.hwnd = win.CreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), win.S(text), style,
		0, 0, 10, 10, a.hwnd, uintptr(c.id), a.inst, 0)
	switch k {
	case kEdit:
		win.SendMessageW.Call(c.hwnd, win.WM_SETFONT, a.th.fonts[fMd], 0)
		if label != "" {
			win.SendMessageW.Call(c.hwnd, win.EM_SETCUEBANNER, 1, win.S(label))
		}
	case kSlider:
	default:
		if oldBtnProc == 0 {
			oldBtnProc = win.GetWindowLongPtrW.Call(c.hwnd, win.GWLP_WNDPROC)
		}
		win.SetWindowLongPtrW.Call(c.hwnd, win.GWLP_WNDPROC, btnProcCB)
	}
	a.ctls = append(a.ctls, c)
	a.byID[c.id] = c
	return c
}

// clearControls destroys every control (page change).
func (a *app) clearControls() {
	for _, c := range a.ctls {
		win.DestroyWindow.Call(c.hwnd)
	}
	a.ctls = nil
	a.byID = map[int]*ctl{}
	a.hover = 0
}

// dropControls destroys some controls (a sheet closing).
func (a *app) dropControls(list []*ctl) {
	for _, c := range list {
		win.DestroyWindow.Call(c.hwnd)
		delete(a.byID, c.id)
	}
	keep := a.ctls[:0]
	for _, c := range a.ctls {
		if _, ok := a.byID[c.id]; ok {
			keep = append(keep, c)
		}
	}
	a.ctls = keep
}

// place positions a control, or hides it when r is empty.
func (a *app) place(c *ctl, r win.RECT, bg uint32) {
	c.bg = bg
	if c.dimmed && r.W() > 0 && r.H() > 0 {
		// Behind an open sheet: child windows would paint over the sheet, so
		// the window hides and the parent paints its picture under the dim layer.
		if c.shown {
			win.ShowWindow.Call(c.hwnd, win.SW_HIDE)
			c.shown = false
		}
		c.r = r
		a.emit(op{k: oControl, r: r, ctl: c})
		return
	}
	if r.W() <= 0 || r.H() <= 0 {
		if c.shown {
			win.ShowWindow.Call(c.hwnd, win.SW_HIDE)
			c.shown = false
		}
		return
	}
	if c.r != r || !c.shown {
		win.SetWindowPos.Call(c.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.W()), uintptr(r.H()),
			win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	}
	c.r = r
	if !c.shown {
		win.ShowWindow.Call(c.hwnd, win.SW_SHOW)
		c.shown = true
	}
	win.InvalidateRect.Call(c.hwnd, 0, 0)
}

func (a *app) hide(c *ctl) { a.place(c, win.RECT{}, 0) }

func enable(c *ctl, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	win.EnableWindow.Call(c.hwnd, v)
}

func setText(c *ctl, s string) {
	c.label = s
	win.SetWindowTextW.Call(c.hwnd, win.S(s))
}

// btnProc adds hover tracking to owner-drawn buttons.
func btnProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case win.WM_MOUSEMOVE:
		if the.hover != h {
			old := the.hover
			the.hover = h
			if old != 0 {
				win.InvalidateRect.Call(old, 0, 0)
			}
			win.InvalidateRect.Call(h, 0, 0)
			t := win.TRACKMOUSEEVENT{Flags: win.TME_LEAVE, Hwnd: h}
			t.Size = uint32(unsafe.Sizeof(t))
			win.TrackMouseEvent.Call(win.P(&t))
		}
	case win.WM_MOUSELEAVE:
		if the.hover == h {
			the.hover = 0
			win.InvalidateRect.Call(h, 0, 0)
		}
	case win.WM_SETCURSOR:
		win.SetCursor.Call(the.handCursor)
		return 1
	case win.WM_ERASEBKGND:
		return 1
	}
	return win.CallWindowProcW.Call(oldBtnProc, h, uintptr(m), w, l)
}

// drawItem paints an owner-drawn button (double-buffered).
func (a *app) drawItem(d *win.DRAWITEMSTRUCT) {
	c := a.byID[int(d.CtlID)]
	if c == nil {
		return
	}
	r := d.Item
	w, h := r.W(), r.H()
	mem := win.CreateCompatibleDC.Call(d.Hdc)
	bmp := win.CreateCompatibleBitmap.Call(d.Hdc, uintptr(w), uintptr(h))
	old := win.SelectObject.Call(mem, bmp)
	p := newPainter(a.th, mem)
	local := win.RECT{Right: w, Bottom: h}
	a.paintControl(p, c, local, d.ItemState)
	p.done()
	win.BitBlt.Call(d.Hdc, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h), mem, 0, 0, win.SRCCOPY)
	win.SelectObject.Call(mem, old)
	win.DeleteObject.Call(bmp)
	win.DeleteDC.Call(mem)
}

func (a *app) paintControl(p *painter, c *ctl, r win.RECT, state uint32) {
	t := a.th
	col := t.c
	hover := a.hover == c.hwnd
	pressed := state&win.ODS_SELECTED != 0
	disabled := state&win.ODS_DISABLED != 0
	focus := state&win.ODS_FOCUS != 0 && state&win.ODS_NOFOCUSRECT == 0
	rs, rm := t.px(radiusSm), t.px(radiusMd)
	fw := t.px(focusWidth)

	p.fill(r, c.bg, 0)
	inner := r
	if c.kind != kCard && c.kind != kToolbar {
		inner = inset(r, fw+t.px(space0)/2)
	}
	center := uint32(win.DT_CENTER | win.DT_VCENTER | win.DT_SINGLELINE | win.DT_END_ELLIPSIS)

	switch c.kind {
	case kPrimary, kDanger:
		bg, fg := col.Accent, col.OnAccent
		if hover || pressed {
			bg = col.AccentHover
		}
		if c.kind == kDanger {
			bg, fg = col.Danger, 0xffffff
			if a.th.dark {
				fg = col.Bg
			}
		}
		if disabled {
			bg, fg = col.Surface2, col.Faint
		}
		p.fill(inner, bg, rs)
		p.text(inner, c.label, fSmBold, fg, center)
	case kSecondary:
		bg := col.Surface
		if hover || pressed {
			bg = col.Surface2
		}
		p.fill(inner, bg, rs)
		p.stroke(inner, col.BorderStrong, rs, t.px(border))
		fg := col.Text
		if disabled {
			fg = col.Faint
		}
		p.text(inner, c.label, fSmBold, fg, center)
	case kDangerText:
		if hover || pressed {
			p.fill(inner, col.DangerSoft, rs)
		}
		p.text(inner, c.label, fSmBold, col.Danger, center)
	case kSegment:
		bg, fg, bd := col.Surface, col.Text, col.Border
		if c.on {
			bg, fg, bd = col.AccentSoft, col.Accent, col.Accent
		} else if hover {
			bg = col.Surface2
		}
		if disabled {
			fg = col.Faint
		}
		p.fill(inner, bg, rs)
		p.stroke(inner, bd, rs, t.px(border))
		f := fSm
		if c.on {
			f = fSmBold
		}
		p.text(inner, c.label, f, fg, center)
	case kToggle:
		if hover {
			p.fill(inner, col.Surface2, rs)
		}
		sw, sh := t.px(switchW), t.px(switchH)
		track := win.RECT{Left: inner.Right - sw - t.px(space2), Top: inner.Top + (inner.H()-sh)/2}
		track.Right, track.Bottom = track.Left+sw, track.Top+sh
		tc := col.BorderStrong
		if c.on {
			tc = col.Accent
		}
		p.fill(track, tc, sh/2)
		kr := float32(sh)/2 - float32(t.px(3))
		kx := float32(track.Left) + float32(sh)/2
		if c.on {
			kx = float32(track.Right) - float32(sh)/2
		}
		p.dot(kx, float32(track.Top)+float32(sh)/2, kr, 0xffffff)
		lr := inner
		lr.Left += t.px(space2)
		lr.Right = track.Left - t.px(space3)
		p.text(lr, c.label, fSm, col.Text, win.DT_VCENTER|win.DT_SINGLELINE|win.DT_END_ELLIPSIS)
	case kToolbar:
		bg := col.Toolbar
		if hover || pressed {
			bg = col.ToolbarHover
		}
		p.fill(inset(r, t.px(space0)), bg, rs)
		p.text(r, c.label, fSmBold, col.OnToolbar, center)
		if focus {
			p.stroke(r, col.OnToolbar, rs, fw)
		}
		focus = false
	case kCard:
		bd := col.Border
		if hover {
			bd = col.BorderStrong
		}
		p.fill(r, col.Surface, rm)
		p.stroke(r, bd, rm, t.px(border))
		pad := t.px(space4)
		ic := t.px(iconSize)
		icon := win.RECT{Left: r.Left + pad, Top: r.Top + (r.H()-ic)/2}
		icon.Right, icon.Bottom = icon.Left+ic, icon.Top+ic
		p.fill(icon, col.AccentSoft, ic/2)
		p.monitorGlyph(inset(icon, t.px(space2)), col.Accent)
		// badge on the right
		bw, bh := t.measure(c.badge, fXs, 0)
		bw += t.px(space3) * 2
		bh += t.px(space1) * 2
		chev := t.px(space4)
		badge := win.RECT{Right: r.Right - pad - chev - t.px(space2), Top: r.Top + (r.H()-bh)/2}
		badge.Left, badge.Bottom = badge.Right-bw, badge.Top+bh
		bbg, bfg := col.Surface2, col.Muted
		switch c.tone {
		case 1:
			bbg, bfg = col.OkSoft, col.Ok
		case 2:
			bbg, bfg = col.WarnSoft, col.Warn
		}
		p.fill(badge, bbg, bh/2)
		p.text(badge, c.badge, fXs, bfg, center)
		// chevron
		cx, cy := float32(r.Right-pad-chev/2), float32(r.Top+r.H()/2)
		s := float32(t.px(4))
		p.line(cx-s/2, cy-s, cx+s/2, cy, col.Faint, float32(t.px(2)))
		p.line(cx+s/2, cy, cx-s/2, cy+s, col.Faint, float32(t.px(2)))
		// name and address
		tx := icon.Right + t.px(space4)
		lh1, lh2 := t.lineH(fMdBold), t.lineH(fSm)
		top := r.Top + (r.H()-lh1-lh2-t.px(space0))/2
		left := uint32(win.DT_SINGLELINE | win.DT_END_ELLIPSIS)
		p.text(win.RECT{Left: tx, Top: top, Right: badge.Left - t.px(space3), Bottom: top + lh1}, c.label, fMdBold, col.Text, left)
		p.text(win.RECT{Left: tx, Top: top + lh1 + t.px(space0), Right: badge.Left - t.px(space3), Bottom: top + lh1 + lh2 + t.px(space0)}, c.sub, fSm, col.Muted, left)
		if focus {
			p.stroke(r, col.Focus, rm, fw)
		}
		focus = false
	}
	if focus {
		p.stroke(r, col.Focus, rs+fw, fw)
	}
}

func inset(r win.RECT, d int32) win.RECT {
	return win.RECT{Left: r.Left + d, Top: r.Top + d, Right: r.Right - d, Bottom: r.Bottom - d}
}

func rect(x, y, w, h int32) win.RECT { return win.RECT{Left: x, Top: y, Right: x + w, Bottom: y + h} }
