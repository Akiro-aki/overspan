//go:build windows

package main

import (
	"image"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"overspan/viewer/internal/win"
)

func newHookCallback() uintptr {
	return syscall.NewCallback(func(code, w, l uintptr) uintptr { return hookProc(int(int32(code)), w, l) })
}

// testHook answers WM_COPYDATA from tools/winprobe, only when the app was
// started with OVERSPAN_TEST_HOOKS=1. dwData 1 = save a PNG of the window's
// client area (picture, sheets and every control) to the UTF-16 path given.
func (a *app) testHook(l uintptr) uintptr {
	if !a.testHooks {
		return 0
	}
	cd := (*win.COPYDATASTRUCT)(unsafe.Pointer(l))
	if cd.Data != 1 || cd.Size < 2 {
		return 0
	}
	path := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(cd.Ptr)), cd.Size/2))
	if err := a.snapshot(path); err != nil {
		return 0
	}
	return 1
}

func (a *app) snapshot(path string) error {
	cr := a.client()
	w, h := cr.W(), cr.H()
	screen := win.GetDC.Call(0)
	defer win.ReleaseDC.Call(0, screen)
	mem := win.CreateCompatibleDC.Call(screen)
	defer win.DeleteDC.Call(mem)
	bi := win.BITMAPINFO{Header: win.BITMAPINFOHEADER{Width: w, Height: -h, Planes: 1, BitCount: 32}}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	var bits uintptr
	dib := win.CreateDIBSection.Call(mem, win.P(&bi), win.DIB_RGB_COLORS, win.P(&bits), 0, 0)
	if dib == 0 {
		return os.ErrInvalid
	}
	defer win.DeleteObject.Call(dib)
	old := win.SelectObject.Call(mem, dib)
	defer win.SelectObject.Call(mem, old)
	a.render(mem, cr)

	focus := win.GetFocus.Call()
	for _, c := range a.ctls {
		if !c.shown || c.r.W() <= 0 {
			continue
		}
		cw, ch := c.r.W(), c.r.H()
		tmp := win.CreateCompatibleDC.Call(screen)
		bmp := win.CreateCompatibleBitmap.Call(screen, uintptr(cw), uintptr(ch))
		o := win.SelectObject.Call(tmp, bmp)
		switch c.kind {
		case kEdit, kSlider:
			win.FillRect.Call(tmp, win.P(&win.RECT{Right: cw, Bottom: ch}), a.th.brush(c.bg))
			win.SendMessageW.Call(c.hwnd, win.WM_PRINT, tmp, 0x4|0x8|0x10) // client, erase, children
		default:
			var st uint32
			if win.IsWindowEnabled.Call(c.hwnd) == 0 {
				st |= win.ODS_DISABLED
			}
			if focus == c.hwnd {
				st |= win.ODS_FOCUS
			}
			p := newPainter(a.th, tmp)
			a.paintControl(p, c, win.RECT{Right: cw, Bottom: ch}, st)
			p.done()
		}
		win.BitBlt.Call(mem, uintptr(c.r.Left), uintptr(c.r.Top), uintptr(cw), uintptr(ch), tmp, 0, 0, win.SRCCOPY)
		win.SelectObject.Call(tmp, o)
		win.DeleteObject.Call(bmp)
		win.DeleteDC.Call(tmp)
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(bits)), int(w*h*4))
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < int(w*h); i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = src[i*4+2], src[i*4+1], src[i*4], 255
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
