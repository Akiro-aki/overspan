//go:build windows

// Overspan viewer for Windows: shows a monitor from an Overspan computer in a
// window or full screen, and sends mouse, keyboard and pen input back.
//
//	overspan-viewer.exe [address]
//
// With an address (host or host:port) it opens the connect sheet for it.
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"overspan/viewer/internal/client"
	"overspan/viewer/internal/win"
)

const version = "0.1.0"

// Custom messages from worker goroutines to the UI thread.
const (
	wmFound  = win.WM_APP + 1
	wmInfo   = win.WM_APP + 2
	wmStatus = win.WM_APP + 3
	wmFrame  = win.WM_APP + 4
	wmClick  = win.WM_APP + 5
)

// Timers.
const (
	tSpin = iota + 1
	tPrune
	tToolbar
	tResize
	tToast
	tStats
	tEdge
)

type page int

const (
	pageList page = iota
	pageDisplay
)

type sheetKind int

const (
	sheetNone sheetKind = iota
	sheetAdd
	sheetConnect
	sheetSettings
	sheetConfirm
)

// op is one entry of the display list a layout pass produces and paint replays.
type opKind int

const (
	oFill opKind = iota
	oFillA
	oStroke
	oText
	oLogo
	oGlyph
	oSpin
	oPicture
	oDot
	oControl // a page control painted under an open sheet
)

type op struct {
	k      opKind
	r      win.RECT
	c, c2  uint32
	a      uint8
	rad, w int32
	s      string
	f      fontID
	flags  uint32
	ctl    *ctl
}

type app struct {
	hwnd, inst  uintptr
	th          *theme
	prefs       *client.Prefs
	page        page
	sheet       sheetKind
	ctls        []*ctl
	byID        map[int]*ctl
	nextID      int
	hover       uintptr
	ops         []op
	handCursor  uintptr
	arrowCursor uintptr
	spinAngle   float32
	spinning    bool
	back        uintptr // back buffer bitmap
	backW       int32
	backH       int32
	testHooks   bool
	ready       bool // set once the first page exists

	pageCtls  []*ctl
	sheetCtls []*ctl
	defID     int
	focusBack uintptr

	list listState
	disp displayState

	mu      sync.Mutex
	pending []client.Computer
}

var the *app

func main() {
	runtime.LockOSThread()
	a := &app{byID: map[int]*ctl{}, nextID: 100, testHooks: os.Getenv("OVERSPAN_TEST_HOOKS") == "1"}
	the = a
	cfg, err := os.UserConfigDir()
	if err != nil {
		cfg = "."
	}
	a.prefs = client.LoadPrefs(filepath.Join(cfg, "Overspan", "viewer.json"))

	// Per-monitor DPI awareness, with fallbacks for older Windows.
	switch {
	case win.SetProcessDpiAwarenessContext.Available():
		win.SetProcessDpiAwarenessContext.Call(win.DpiPerMonitorV2)
	case win.SetProcessDpiAwareness.Available():
		win.SetProcessDpiAwareness.Call(2)
	default:
		win.SetProcessDPIAware.Call()
	}
	icc := win.INITCOMMONCONTROLSEX{ICC: win.ICC_BAR_CLASSES | win.ICC_STANDARD_CLASSES}
	icc.Size = uint32(unsafe.Sizeof(icc))
	win.InitCommonControlsEx.Call(win.P(&icc))
	var token uintptr
	in := win.GdiplusStartupInput{Version: 1}
	win.GdiplusStartup.Call(win.P(&token), win.P(&in), 0)

	a.inst = win.GetModuleHandleW.Call(0)
	a.arrowCursor = win.LoadCursorW.Call(0, win.IDC_ARROW)
	a.handCursor = win.LoadCursorW.Call(0, win.IDC_HAND)
	icon := win.LoadImageW.Call(a.inst, 1, win.IMAGE_ICON, 0, 0, win.LR_DEFAULTSIZE|win.LR_SHARED)
	wc := win.WNDCLASSEX{
		Style: win.CS_HREDRAW | win.CS_VREDRAW | win.CS_DBLCLKS, WndProc: syscall.NewCallback(wndProc),
		Instance: a.inst, Icon: icon, IconSm: icon, Cursor: a.arrowCursor, ClassName: win.Str("OverspanViewer"),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	win.RegisterClassExW.Call(win.P(&wc))

	a.th = newTheme(96)
	a.hwnd = win.CreateWindowExW.Call(win.WS_EX_CONTROLPARENT, win.S("OverspanViewer"), win.S(client.TxtAppName),
		win.WS_OVERLAPPEDWINDOW|win.WS_CLIPCHILDREN, 0x80000000, 0x80000000, 0, 0, 0, 0, a.inst, 0)
	a.retheme()
	// size the window for its monitor's DPI
	wr := win.WindowRect(a.hwnd)
	win.SetWindowPos.Call(a.hwnd, 0, uintptr(wr.Left), uintptr(wr.Top), uintptr(a.th.px(winW)), uintptr(a.th.px(winH)), win.SWP_NOZORDER)

	a.ready = true
	a.enterList()
	if len(os.Args) > 1 {
		if c, ok := client.ParseAddress(os.Args[len(os.Args)-1]); ok {
			a.openConnect(c)
		}
	}
	win.ShowWindow.Call(a.hwnd, win.SW_SHOWDEFAULT)
	win.UpdateWindow.Call(a.hwnd)

	var m win.MSG
	for {
		r := win.GetMessageW.Call(win.P(&m), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		if a.dialogNav() && win.IsDialogMessageW.Call(a.hwnd, win.P(&m)) != 0 {
			continue
		}
		win.TranslateMessage.Call(win.P(&m))
		win.DispatchMessageW.Call(win.P(&m))
	}
	if a.disp.viewer != nil {
		a.disp.viewer.Close()
		time.Sleep(150 * time.Millisecond) // let the goodbye reach the computer
	}
}

// dialogNav says whether Tab/Enter/Esc/arrows act on controls (true) or go
// to the computer as key presses (false: the picture has focus).
func (a *app) dialogNav() bool {
	if a.page == pageList || a.sheet != sheetNone {
		return true
	}
	f := win.GetFocus.Call()
	return f != a.hwnd && f != 0
}

func (a *app) retheme() {
	dpi := 96
	if win.GetDpiForWindow.Available() {
		if d := int(win.GetDpiForWindow.Call(a.hwnd)); d > 0 {
			dpi = d
		}
	}
	if a.th != nil {
		a.th.release()
	}
	a.th = newTheme(dpi)
	if win.DwmSetWindowAttribute.Available() {
		v := int32(0)
		if a.th.dark {
			v = 1
		}
		win.DwmSetWindowAttribute.Call(a.hwnd, 20, win.P(&v), 4)
	}
	for _, c := range a.ctls {
		if c.kind == kEdit {
			win.SendMessageW.Call(c.hwnd, win.WM_SETFONT, a.th.fonts[fMd], 1)
		}
	}
}

func (a *app) client() win.RECT { return win.ClientRect(a.hwnd) }

func (a *app) relayout() {
	if !a.ready {
		return
	}
	a.ops = a.ops[:0]
	switch a.page {
	case pageList:
		a.layoutList()
	case pageDisplay:
		a.layoutDisplay()
	}
	if a.sheet != sheetNone {
		a.layoutSheet()
	}
	spin := false
	for _, o := range a.ops {
		if o.k == oSpin {
			spin = true
		}
	}
	if spin != a.spinning {
		a.spinning = spin
		if spin {
			win.SetTimer.Call(a.hwnd, tSpin, 40, 0)
		} else {
			win.KillTimer.Call(a.hwnd, tSpin)
		}
	}
	win.Invalidate(a.hwnd)
}

// ---------------------------------------------------------------- painting

func (a *app) emit(o op) { a.ops = append(a.ops, o) }

func (a *app) fill(r win.RECT, c uint32, rad int32) { a.emit(op{k: oFill, r: r, c: c, rad: rad}) }
func (a *app) strokeR(r win.RECT, c uint32, rad, w int32) {
	a.emit(op{k: oStroke, r: r, c: c, rad: rad, w: w})
}
func (a *app) text(r win.RECT, s string, f fontID, c uint32, flags uint32) {
	a.emit(op{k: oText, r: r, s: s, f: f, c: c, flags: flags})
}

// para lays out wrapped text at (x, y) within width w and returns its height.
func (a *app) para(x, y, w int32, s string, f fontID, c uint32, flags uint32) int32 {
	if s == "" {
		return 0
	}
	_, h := a.th.measure(s, f, w)
	a.text(rect(x, y, w, h), s, f, c, flags|win.DT_WORDBREAK)
	return h
}

func (a *app) paint(hdc uintptr) {
	cr := a.client()
	w, h := cr.W(), cr.H()
	if w <= 0 || h <= 0 {
		return
	}
	mem := win.CreateCompatibleDC.Call(hdc)
	if a.back == 0 || a.backW != w || a.backH != h {
		if a.back != 0 {
			win.DeleteObject.Call(a.back)
		}
		a.back = win.CreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
		a.backW, a.backH = w, h
	}
	old := win.SelectObject.Call(mem, a.back)
	a.render(mem, cr)
	win.BitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, win.SRCCOPY)
	win.SelectObject.Call(mem, old)
	win.DeleteDC.Call(mem)
}

func (a *app) render(hdc uintptr, cr win.RECT) {
	p := newPainter(a.th, hdc)
	defer p.done()
	bg := a.th.c.Bg
	if a.page == pageDisplay {
		bg = a.th.c.Stage
	}
	p.fill(cr, bg, 0)
	for _, o := range a.ops {
		switch o.k {
		case oFill:
			p.fill(o.r, o.c, o.rad)
		case oFillA:
			p.fillA(o.r, o.c, o.a, o.rad)
		case oStroke:
			p.stroke(o.r, o.c, o.rad, o.w)
		case oText:
			p.text(o.r, o.s, o.f, o.c, o.flags)
		case oLogo:
			p.logo(o.r)
		case oGlyph:
			p.monitorGlyph(o.r, o.c)
		case oSpin:
			p.arc(o.r, o.c, o.c2, o.w, a.spinAngle)
		case oDot:
			p.dot(float32(o.r.Left+o.r.Right)/2, float32(o.r.Top+o.r.Bottom)/2, float32(o.r.W())/2, o.c)
		case oPicture:
			win.GdipFlush.Call(p.g, 1)
			a.paintPicture(hdc, o.r)
		case oControl:
			a.paintControl(p, o.ctl, o.r, 0)
		}
	}
}

func (a *app) paintPicture(hdc uintptr, dst win.RECT) {
	cv := &a.disp.viewer.Canvas
	cv.Lock()
	defer cv.Unlock()
	if cv.W == 0 || cv.Pix == nil {
		return
	}
	bi := win.BITMAPINFO{Header: win.BITMAPINFOHEADER{Width: int32(cv.W), Height: -int32(cv.H), Planes: 1, BitCount: 32}}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	mode := uintptr(3) // COLORONCOLOR when the sizes match
	if dst.W() != int32(cv.W) || dst.H() != int32(cv.H) {
		mode = win.HALFTONE
	}
	win.SetStretchBltMode.Call(hdc, mode)
	win.SetBrushOrgEx.Call(hdc, 0, 0, 0)
	win.StretchDIBits.Call(hdc, uintptr(dst.Left), uintptr(dst.Top), uintptr(dst.W()), uintptr(dst.H()),
		0, 0, uintptr(cv.W), uintptr(cv.H), uintptr(unsafe.Pointer(&cv.Pix[0])), win.P(&bi), win.DIB_RGB_COLORS, win.SRCCOPY)
}

// ------------------------------------------------------------------ sheets

func (a *app) openSheet(k sheetKind, build func()) {
	if a.sheet != sheetNone {
		a.closeSheet()
	}
	a.focusBack = win.GetFocus.Call()
	a.sheet = k
	for _, c := range a.pageCtls {
		c.dimmed = true
		enable(c, false)
	}
	a.defID = 0
	before := len(a.ctls)
	build()
	a.sheetCtls = append([]*ctl(nil), a.ctls[before:]...)
	a.relayout()
	for _, c := range a.sheetCtls {
		if c.shown && win.IsWindowEnabled.Call(c.hwnd) != 0 {
			win.SetFocus.Call(c.hwnd)
			break
		}
	}
}

func (a *app) closeSheet() {
	if a.sheet == sheetNone {
		return
	}
	a.dropControls(a.sheetCtls)
	a.sheetCtls = nil
	a.sheet = sheetNone
	a.defID = 0
	for _, c := range a.pageCtls {
		c.dimmed = false
		enable(c, true)
	}
	a.afterSheet()
	a.relayout()
	if a.focusBack != 0 && win.IsWindow.Call(a.focusBack) != 0 && win.IsWindowVisible.Call(a.focusBack) != 0 {
		win.SetFocus.Call(a.focusBack)
	} else {
		win.SetFocus.Call(a.hwnd)
	}
}

// layoutSheet dims the page and centres the sheet card.
func (a *app) layoutSheet() {
	cr := a.client()
	c := a.th.c
	a.emit(op{k: oFillA, r: cr, c: c.Overlay, a: c.OverlayAlpha})
	switch a.sheet {
	case sheetAdd:
		a.layoutAdd()
	case sheetConnect:
		a.layoutConnect()
	case sheetSettings:
		a.layoutSettings()
	case sheetConfirm:
		a.layoutConfirm()
	}
}

// sheetCard reserves a centred card of content height h and returns its
// inner content rectangle. Call it first, then lay out inside.
func (a *app) sheetCard(h int32) (card, content win.RECT) {
	t := a.th
	cr := a.client()
	w := min(t.px(sheetW), cr.W()-t.px(space4)*2)
	pad := t.px(space5)
	ch := h + pad*2
	x := (cr.W() - w) / 2
	y := max(t.px(space4), (cr.H()-ch)/2)
	card = rect(x, y, w, ch)
	a.fill(card, t.c.Surface, t.px(radiusLg))
	a.strokeR(card, t.c.Border, t.px(radiusLg), t.px(border))
	return card, rect(x+pad, y+pad, w-pad*2, h)
}

// buttonRow places buttons right-aligned at y; lead (optional) goes left.
func (a *app) buttonRow(x, y, w int32, lead *ctl, btns ...*ctl) {
	t := a.th
	h := t.px(controlH)
	right := x + w
	for i := len(btns) - 1; i >= 0; i-- {
		b := btns[i]
		bw := a.buttonW(b.label)
		a.place(b, rect(right-bw, y, bw, h), t.c.Surface)
		right -= bw + t.px(space2)
	}
	if lead != nil {
		a.place(lead, rect(x-t.px(space2), y, a.buttonW(lead.label), h), t.c.Surface)
	}
}

func (a *app) buttonW(label string) int32 {
	w, _ := a.th.measure(label, fSmBold, 0)
	return w + a.th.px(space5)*2
}

// field draws a text box frame around an EDIT control and places it.
func (a *app) field(c *ctl, r win.RECT, bg uint32) {
	t := a.th
	focused := win.GetFocus.Call() == c.hwnd
	bd, bw := t.c.BorderStrong, t.px(border)
	if focused {
		bd, bw = t.c.Focus, t.px(focusWidth)
	}
	a.fill(r, t.c.Surface, t.px(radiusSm))
	a.strokeR(r, bd, t.px(radiusSm), bw)
	lh := t.lineH(fMd)
	pad := t.px(space3)
	a.place(c, rect(r.Left+pad, r.Top+(r.H()-lh)/2, r.W()-pad*2, lh), bg)
}

// --------------------------------------------------------------- messages

func wndProc(h uintptr, m uint32, w, l uintptr) uintptr {
	a := the
	if a == nil || (a.hwnd != 0 && h != a.hwnd) {
		return win.DefWindowProcW.Call(h, uintptr(m), w, l)
	}
	if a.hwnd == 0 {
		a.hwnd = h
	}
	if a.page == pageDisplay {
		if r, ok := a.displayInput(m, w, l); ok {
			return r
		}
	}
	switch m {
	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint.Call(h, win.P(&ps))
		a.paint(hdc)
		win.EndPaint.Call(h, win.P(&ps))
		return 0
	case win.WM_PRINTCLIENT:
		a.render(w, a.client())
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_SIZE:
		if a.th != nil {
			a.relayout()
			if a.page == pageDisplay {
				a.displayResized()
			}
		}
		return 0
	case win.WM_GETMINMAXINFO:
		if a.th != nil {
			mm := (*win.MINMAXINFO)(unsafe.Pointer(l))
			mm.MinTrackSize = win.POINT{X: a.th.px(minWinW), Y: a.th.px(minWinH)}
		}
		return 0
	case win.WM_DPICHANGED:
		a.retheme()
		r := (*win.RECT)(unsafe.Pointer(l))
		win.SetWindowPos.Call(h, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.W()), uintptr(r.H()), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
		a.relayout()
		return 0
	case win.WM_SETTINGCHANGE, win.WM_THEMECHANGED:
		if dark := systemDark(); dark != a.th.dark {
			a.retheme()
			a.relayout()
			for _, c := range a.ctls {
				win.InvalidateRect.Call(c.hwnd, 0, 1)
			}
		}
	case win.WM_DRAWITEM:
		a.drawItem((*win.DRAWITEMSTRUCT)(unsafe.Pointer(l)))
		return 1
	case win.WM_CTLCOLOREDIT, win.WM_CTLCOLORSTATIC:
		hdc := w
		win.SetTextColor.Call(hdc, win.RGB(a.th.c.Text))
		bg := a.th.c.Surface
		if c := a.ctlByHwnd(l); c != nil && c.kind == kSlider {
			bg = c.bg
		}
		win.SetBkColor.Call(hdc, win.RGB(bg))
		return a.th.brush(bg)
	case win.DM_GETDEFID:
		if a.defID != 0 {
			return uintptr(a.defID) | win.DC_HASDEFID<<16
		}
		return 0
	case win.WM_COMMAND:
		id, code := int(win.LOWORD(w)), win.HIWORD(w)
		switch {
		case id == win.IDCANCEL:
			a.cancel()
		case id == win.IDOK:
			if a.defID != 0 {
				win.PostMessageW.Call(h, wmClick, uintptr(a.defID), 0)
			}
		default:
			c := a.byID[id]
			if c == nil {
				break
			}
			switch {
			case c.kind == kEdit && (code == win.EN_SETFOCUS || code == win.EN_KILLFOCUS):
				a.relayout()
			case c.kind == kEdit && code == win.EN_CHANGE:
				a.editChanged(c)
			case c.kind == kCard && code == win.BN_SETFOCUS:
				a.ensureVisible(c)
			case code == win.BN_CLICKED:
				win.PostMessageW.Call(h, wmClick, uintptr(id), 0)
			}
		}
		return 0
	case wmClick:
		if c := a.byID[int(w)]; c != nil && c.onClick != nil && win.IsWindowEnabled.Call(c.hwnd) != 0 {
			c.onClick()
		}
		return 0
	case win.WM_HSCROLL:
		if c := a.ctlByHwnd(l); c != nil {
			a.sliderMoved(c)
		}
		return 0
	case win.WM_MOUSEWHEEL:
		if a.page == pageList && a.sheet == sheetNone {
			a.scrollList(int32(int16(win.HIWORD(w))))
		}
		return 0
	case win.WM_TIMER:
		a.timer(w)
		return 0
	case wmFound:
		a.takeFound()
		return 0
	case wmInfo:
		a.infoArrived()
		return 0
	case wmStatus:
		a.statusChanged()
		return 0
	case wmFrame:
		a.frameArrived()
		return 0
	case win.WM_COPYDATA:
		return a.testHook(l)
	case win.WM_CLOSE:
		win.DestroyWindow.Call(h)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage.Call(0)
		return 0
	}
	return win.DefWindowProcW.Call(h, uintptr(m), w, l)
}

func (a *app) ctlByHwnd(h uintptr) *ctl {
	for _, c := range a.ctls {
		if c.hwnd == h {
			return c
		}
	}
	return nil
}

func (a *app) cancel() {
	switch {
	case a.sheet != sheetNone:
		a.closeSheet()
	case a.page == pageDisplay:
		a.escapeDisplay()
	}
}

func (a *app) timer(id uintptr) {
	switch id {
	case tSpin:
		a.spinAngle += 12
		if a.spinAngle >= 360 {
			a.spinAngle -= 360
		}
		for _, o := range a.ops {
			if o.k == oSpin {
				win.InvalidateR(a.hwnd, o.r)
			}
		}
	case tPrune:
		a.pruneList()
	default:
		a.displayTimer(id)
	}
}

func (a *app) editChanged(c *ctl) {
	if a.sheet == sheetAdd && a.list.addErr {
		a.list.addErr = false
		a.relayout()
	}
}

func (a *app) sliderMoved(c *ctl) {
	if a.sheet == sheetSettings {
		a.settingsSliderMoved(c)
	}
}

func (a *app) afterSheet() {
	if a.page == pageDisplay {
		a.displayAfterSheet()
	}
}

func (a *app) setTitle(s string) { win.SetWindowTextW.Call(a.hwnd, win.S(s)) }

func deviceFallbackName() string {
	n, _ := os.Hostname()
	return n
}

func userAgent() string { return "Overspan-Windows/" + version + " (Windows)" }
