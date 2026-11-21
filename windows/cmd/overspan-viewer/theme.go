//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"overspan/viewer/internal/win"
)

// Design tokens: the same roles and values as the web tokens.css and the
// Android colors.xml. Components use only these; no raw colours or sizes
// anywhere else in the UI code.
type palette struct {
	Bg, Surface, Surface2, Border, BorderStrong                    uint32
	Text, Muted, Faint                                             uint32
	Accent, AccentHover, AccentSoft, OnAccent                      uint32
	Ok, OkSoft, Warn, WarnSoft, Danger, DangerSoft                 uint32
	Focus, Stage, Toolbar, ToolbarHover, OnToolbar, OnToolbarMuted uint32
	Overlay                                                        uint32 // drawn at OverlayAlpha
	OverlayAlpha                                                   uint8
}

var light = palette{
	Bg: 0xf4f5f8, Surface: 0xffffff, Surface2: 0xeceef3, Border: 0xd5d9e2, BorderStrong: 0xb4bac8,
	Text: 0x161a23, Muted: 0x596174, Faint: 0x666d80,
	Accent: 0x2f55e4, AccentHover: 0x2445c4, AccentSoft: 0xe2e8fd, OnAccent: 0xffffff,
	Ok: 0x187a4a, OkSoft: 0xdcf3e6, Warn: 0x9a5b00, WarnSoft: 0xfbedd3, Danger: 0xc22b3a, DangerSoft: 0xfbe1e4,
	Focus: 0x2f55e4, Stage: 0x000000, Toolbar: 0x161a23, ToolbarHover: 0x2c3240, OnToolbar: 0xf1f3f8, OnToolbarMuted: 0xa3abbd,
	Overlay: 0x0a0c12, OverlayAlpha: 140,
}

var dark = palette{
	Bg: 0x0f1218, Surface: 0x171b23, Surface2: 0x1e232d, Border: 0x2a303c, BorderStrong: 0x3d4555,
	Text: 0xe9ecf3, Muted: 0xa3abbd, Faint: 0x8a93a7,
	Accent: 0x6d8bff, AccentHover: 0x8aa2ff, AccentSoft: 0x1d2747, OnAccent: 0x0b1020,
	Ok: 0x4cc98a, OkSoft: 0x12301f, Warn: 0xf0b04a, WarnSoft: 0x3a2a0e, Danger: 0xff6b78, DangerSoft: 0x3c161b,
	Focus: 0x6d8bff, Stage: 0x000000, Toolbar: 0x161a23, ToolbarHover: 0x2c3240, OnToolbar: 0xf1f3f8, OnToolbarMuted: 0xa3abbd,
	Overlay: 0x000000, OverlayAlpha: 150,
}

// Sizes in device-independent pixels (1/96 inch).
const (
	space0 = 2
	space1 = 4
	space2 = 8
	space3 = 12
	space4 = 16
	space5 = 24
	space6 = 32
	space7 = 48

	radiusSm   = 6
	radiusMd   = 10
	radiusLg   = 16
	focusWidth = 2
	border     = 1

	controlH   = 40
	toolbarH   = 48
	toolbarBtn = 34
	cardH      = 76
	logoSize   = 44
	iconSize   = 40
	spinSize   = 36
	switchW    = 40
	switchH    = 22
	contentMax = 720
	sheetW     = 460
	statusW    = 480
	edgeZone   = 3 // top-edge strip that reveals the toolbar

	textXs = 12
	textSm = 14
	textMd = 16
	textLg = 20
	textXl = 28

	minWinW = 560
	minWinH = 500
	winW    = 960
	winH    = 700
)

// Fonts, created per DPI.
type fontID int

const (
	fXs fontID = iota
	fSm
	fSmBold
	fMd
	fMdBold
	fLgBold
	fXlBold
	fontCount
)

type theme struct {
	c       palette
	dark    bool
	dpi     int
	fonts   [fontCount]uintptr
	brushes map[uint32]uintptr
}

// px converts DIPs to device pixels at the current DPI.
func (t *theme) px(dip int) int32 { return int32((dip*t.dpi + 48) / 96) }

func newTheme(dpi int) *theme {
	t := &theme{dpi: dpi, brushes: map[uint32]uintptr{}}
	t.dark = systemDark()
	t.c = light
	if t.dark {
		t.c = dark
	}
	spec := [fontCount]struct{ size, weight int }{
		fXs: {textXs, win.FW_NORMAL}, fSm: {textSm, win.FW_NORMAL}, fSmBold: {textSm, win.FW_SEMIBOLD},
		fMd: {textMd, win.FW_NORMAL}, fMdBold: {textMd, win.FW_SEMIBOLD},
		fLgBold: {textLg, win.FW_SEMIBOLD}, fXlBold: {textXl, win.FW_BOLD},
	}
	for i, s := range spec {
		t.fonts[i] = win.CreateFontW.Call(uintptr(-t.px(s.size)), 0, 0, 0, uintptr(s.weight), 0, 0, 0,
			win.DEFAULT_CHARSET, 0, 0, win.CLEARTYPE_QUALITY, 0, win.S("Segoe UI"))
	}
	return t
}

func (t *theme) release() {
	for _, f := range t.fonts {
		win.DeleteObject.Call(f)
	}
	for _, b := range t.brushes {
		win.DeleteObject.Call(b)
	}
}

// brush returns a cached GDI brush for c.
func (t *theme) brush(c uint32) uintptr {
	if b, ok := t.brushes[c]; ok {
		return b
	}
	b := win.CreateSolidBrush.Call(win.RGB(c))
	t.brushes[c] = b
	return b
}

// systemDark reads Windows' app colour mode (Settings > Personalisation > Colours).
func systemDark() bool {
	var key syscall.Handle
	path, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, syscall.KEY_READ, &key) != nil {
		return false
	}
	defer syscall.RegCloseKey(key)
	var v, typ uint32
	n := uint32(4)
	name, _ := syscall.UTF16PtrFromString("AppsUseLightTheme")
	if syscall.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&v)), &n) != nil {
		return false
	}
	return v == 0
}
