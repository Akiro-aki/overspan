//go:build windows

package main

import (
	"math"
	"unsafe"

	"overspan/viewer/internal/win"
)

// painter draws on one HDC: antialiased shapes through GDI+, text through
// GDI (ClearType). Shapes are flushed before any text so the order holds.
type painter struct {
	t   *theme
	hdc uintptr
	g   uintptr // GpGraphics
}

func newPainter(t *theme, hdc uintptr) *painter {
	p := &painter{t: t, hdc: hdc}
	win.GdipCreateFromHDC.Call(hdc, win.P(&p.g))
	win.GdipSetSmoothingMode.Call(p.g, 4)   // anti-alias
	win.GdipSetPixelOffsetMode.Call(p.g, 4) // half: pixel centres line up with GDI
	win.SetBkMode.Call(hdc, win.TRANSPARENT)
	return p
}

func (p *painter) done() {
	if p.g != 0 {
		win.GdipDeleteGraphics.Call(p.g)
		p.g = 0
	}
}

func fl(v float32) uintptr { return uintptr(math.Float32bits(v)) }

func argb(c uint32, a uint8) uintptr { return uintptr(uint32(a)<<24 | c&0xffffff) }

// roundPath builds a rounded rectangle path; the caller deletes it.
func roundPath(r win.RECT, rad int32, inset float32) uintptr {
	var path uintptr
	win.GdipCreatePath.Call(0, win.P(&path))
	x, y := float32(r.Left)+inset, float32(r.Top)+inset
	w, h := float32(r.W())-2*inset, float32(r.H())-2*inset
	d := float32(rad) * 2
	if d > w {
		d = w
	}
	if d > h {
		d = h
	}
	if d <= 0 {
		win.GdipAddPathLine.Call(path, fl(x), fl(y), fl(x+w), fl(y))
		win.GdipAddPathLine.Call(path, fl(x+w), fl(y), fl(x+w), fl(y+h))
		win.GdipAddPathLine.Call(path, fl(x+w), fl(y+h), fl(x), fl(y+h))
	} else {
		win.GdipAddPathArc.Call(path, fl(x), fl(y), fl(d), fl(d), fl(180), fl(90))
		win.GdipAddPathArc.Call(path, fl(x+w-d), fl(y), fl(d), fl(d), fl(270), fl(90))
		win.GdipAddPathArc.Call(path, fl(x+w-d), fl(y+h-d), fl(d), fl(d), fl(0), fl(90))
		win.GdipAddPathArc.Call(path, fl(x), fl(y+h-d), fl(d), fl(d), fl(90), fl(90))
	}
	win.GdipClosePathFigure.Call(path)
	return path
}

// fill paints a rounded rectangle; radius in device pixels.
func (p *painter) fill(r win.RECT, c uint32, rad int32) { p.fillA(r, c, 255, rad) }

func (p *painter) fillA(r win.RECT, c uint32, a uint8, rad int32) {
	if rad == 0 && a == 255 {
		win.FillRect.Call(p.hdc, win.P(&r), p.t.brush(c))
		return
	}
	var b uintptr
	win.GdipCreateSolidFill.Call(argb(c, a), win.P(&b))
	path := roundPath(r, rad, 0)
	win.GdipFillPath.Call(p.g, b, path)
	win.GdipDeletePath.Call(path)
	win.GdipDeleteBrush.Call(b)
}

// stroke outlines a rounded rectangle inside r with a line w device pixels wide.
func (p *painter) stroke(r win.RECT, c uint32, rad, w int32) {
	var pen uintptr
	win.GdipCreatePen1.Call(argb(c, 255), fl(float32(w)), 2, win.P(&pen))
	path := roundPath(r, rad, float32(w)/2)
	win.GdipDrawPath.Call(p.g, pen, path)
	win.GdipDeletePath.Call(path)
	win.GdipDeletePen.Call(pen)
}

func (p *painter) line(x1, y1, x2, y2 float32, c uint32, w float32) {
	var pen uintptr
	win.GdipCreatePen1.Call(argb(c, 255), fl(w), 2, win.P(&pen))
	win.GdipSetPenStartCap.Call(pen, 2) // round
	win.GdipSetPenEndCap.Call(pen, 2)
	win.GdipDrawLine.Call(p.g, pen, fl(x1), fl(y1), fl(x2), fl(y2))
	win.GdipDeletePen.Call(pen)
}

func (p *painter) dot(cx, cy, r float32, c uint32) {
	var b uintptr
	win.GdipCreateSolidFill.Call(argb(c, 255), win.P(&b))
	win.GdipFillEllipse.Call(p.g, b, fl(cx-r), fl(cy-r), fl(2*r), fl(2*r))
	win.GdipDeleteBrush.Call(b)
}

// arc draws the spinner: a quarter-turn arc at angle degrees.
func (p *painter) arc(r win.RECT, c, track uint32, w int32, angle float32) {
	var pen uintptr
	win.GdipCreatePen1.Call(argb(track, 255), fl(float32(w)), 2, win.P(&pen))
	x, y := float32(r.Left)+float32(w)/2, float32(r.Top)+float32(w)/2
	s := float32(r.W()) - float32(w)
	win.GdipDrawArc.Call(p.g, pen, fl(x), fl(y), fl(s), fl(s), fl(0), fl(360))
	win.GdipDeletePen.Call(pen)
	win.GdipCreatePen1.Call(argb(c, 255), fl(float32(w)), 2, win.P(&pen))
	win.GdipSetPenStartCap.Call(pen, 2)
	win.GdipSetPenEndCap.Call(pen, 2)
	win.GdipDrawArc.Call(p.g, pen, fl(x), fl(y), fl(s), fl(s), fl(angle), fl(100))
	win.GdipDeletePen.Call(pen)
}

// text draws s in r. flags are DT_* values.
func (p *painter) text(r win.RECT, s string, f fontID, c uint32, flags uint32) {
	if s == "" {
		return
	}
	win.GdipFlush.Call(p.g, 1)
	old := win.SelectObject.Call(p.hdc, p.t.fonts[f])
	win.SetTextColor.Call(p.hdc, win.RGB(c))
	u := utf16(s)
	win.DrawTextW.Call(p.hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), win.P(&r), uintptr(flags|win.DT_NOPREFIX))
	win.SelectObject.Call(p.hdc, old)
}

// logo draws the app mark: two overlapping screens on an accent tile.
// Geometry is on a 64-unit grid, the same as the icon artwork.
func (p *painter) logo(r win.RECT) {
	c := p.t.c
	u := float32(r.W()) / 64
	x0, y0 := float32(r.Left), float32(r.Top)
	p.fill(r, c.Accent, int32(14*u))
	sw := 4 * u
	white := uint32(0xffffff)
	box := func(x, y, w, h float32, filled bool) {
		rr := win.RECT{Left: int32(x0 + x*u), Top: int32(y0 + y*u), Right: int32(x0 + (x+w)*u), Bottom: int32(y0 + (y+h)*u)}
		if filled {
			p.fill(rr, c.Accent, int32(3*u))
		}
		p.stroke(rr, white, int32(3*u), int32(sw))
	}
	box(8, 15, 32, 24, false)
	box(28, 25, 28, 24, true)
	p.line(x0+18*u, y0+46*u, x0+28*u, y0+46*u, white, sw)
}

// monitorGlyph draws a small screen outline (cards, empty state).
func (p *painter) monitorGlyph(r win.RECT, c uint32) {
	u := float32(r.W()) / 24
	x0, y0 := float32(r.Left), float32(r.Top)
	scr := win.RECT{Left: int32(x0 + 3*u), Top: int32(y0 + 5*u), Right: int32(x0 + 21*u), Bottom: int32(y0 + 17*u)}
	p.stroke(scr, c, int32(2*u), int32(max(1, 2*u)))
	p.line(x0+12*u, y0+17*u, x0+12*u, y0+20*u, c, 2*u)
	p.line(x0+8*u, y0+20.5*u, x0+16*u, y0+20.5*u, c, 2*u)
}

func utf16(s string) []uint16 {
	out := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return append(out, 0)[:len(out)]
}

// measure returns the size s needs in font f, wrapped at width w (0 = one line).
func (t *theme) measure(s string, f fontID, w int32) (int32, int32) {
	if s == "" {
		return 0, 0
	}
	dc := win.GetDC.Call(0)
	defer win.ReleaseDC.Call(0, dc)
	old := win.SelectObject.Call(dc, t.fonts[f])
	defer win.SelectObject.Call(dc, old)
	u := utf16(s)
	r := win.RECT{Right: w}
	flags := uintptr(win.DT_CALCRECT | win.DT_NOPREFIX)
	if w > 0 {
		flags |= win.DT_WORDBREAK
	} else {
		flags |= win.DT_SINGLELINE
	}
	win.DrawTextW.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), win.P(&r), flags)
	return r.W(), r.H()
}

// lineH is the height of one line of text in f.
func (t *theme) lineH(f fontID) int32 {
	_, h := t.measure("Ag", f, 0)
	return h
}
