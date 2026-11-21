package client

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestParseAddress(t *testing.T) {
	cases := map[string]string{
		"192.168.1.20":              "192.168.1.20:7940",
		" 10.0.0.5:8000 ":           "10.0.0.5:8000",
		"http://my-pc.local:7940/x": "my-pc.local:7940",
		"desk":                      "desk:7940",
	}
	for in, want := range cases {
		c, ok := ParseAddress(in)
		if !ok || c.Key() != want {
			t.Errorf("ParseAddress(%q) = %q %v, want %q", in, c.Key(), ok, want)
		}
	}
	for _, bad := range []string{"", "not an address", "host:0", "host:99999", "a:b:c", "x:port"} {
		if _, ok := ParseAddress(bad); ok {
			t.Errorf("ParseAddress(%q) accepted", bad)
		}
	}
}

func TestKeyCode(t *testing.T) {
	type tc struct {
		vk   uint32
		scan uint32
		ext  bool
		want string
	}
	for _, c := range []tc{
		{0x41, 0x1E, false, "KeyA"}, // A (QWERTY) — scan code decides, not the VK
		{0x51, 0x1E, false, "KeyA"}, // AZERTY gives VK_Q on the same physical key
		{0x0D, 0x1C, true, "NumpadEnter"},
		{0x25, 0x4B, true, "ArrowLeft"},
		{0x64, 0x4B, false, "Numpad4"},
		{0x13, 0x45, false, "Pause"},
		{0x90, 0x45, true, "NumLock"},
		{0x5B, 0x5B, true, "MetaLeft"},
		{0xA5, 0x38, true, "AltRight"},
	} {
		l := uintptr(c.scan) << 16
		if c.ext {
			l |= 1 << 24
		}
		if got := KeyCode(c.vk, l); got != c.want {
			t.Errorf("KeyCode(vk %#x, scan %#x ext %v) = %q, want %q", c.vk, c.scan, c.ext, got, c.want)
		}
	}
}

func jpegOf(t *testing.T, w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		copy(img.Pix[i*4:], []byte{c.R, c.G, c.B, 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func frameMsg(seq uint32, w, h int, patches ...[]any) []byte {
	le := binary.LittleEndian
	m := []byte{'F', 1}
	m = le.AppendUint32(m, seq)
	m = le.AppendUint16(m, uint16(w))
	m = le.AppendUint16(m, uint16(h))
	m = le.AppendUint16(m, uint16(len(patches)))
	for _, p := range patches {
		x, y, pw, ph, data := p[0].(int), p[1].(int), p[2].(int), p[3].(int), p[4].([]byte)
		for _, v := range []int{x, y, pw, ph} {
			m = le.AppendUint16(m, uint16(v))
		}
		m = le.AppendUint32(m, uint32(len(data)))
		m = append(m, data...)
	}
	return m
}

func px(c *Canvas, x, y int) (b, g, r byte) {
	i := (y*c.W + x) * 4
	return c.Pix[i], c.Pix[i+1], c.Pix[i+2]
}

func near(a, b byte) bool { return a > b-12 && a < b+12 || a == b }

func TestCanvasApply(t *testing.T) {
	var c Canvas
	red := jpegOf(t, 64, 64, color.RGBA{220, 30, 30, 255})
	blue := jpegOf(t, 32, 32, color.RGBA{20, 40, 230, 255})
	seq, ok := c.Apply(frameMsg(7, 128, 64, []any{0, 0, 64, 64, red}, []any{64, 32, 32, 32, blue}))
	if !ok || seq != 7 || c.W != 128 || c.H != 64 {
		t.Fatalf("Apply = %d %v, size %dx%d", seq, ok, c.W, c.H)
	}
	if b, g, r := px(&c, 30, 30); !near(r, 220) || !near(g, 30) || !near(b, 30) {
		t.Errorf("red patch pixel = %d,%d,%d (rgb)", r, g, b)
	}
	if b, g, r := px(&c, 80, 48); !near(r, 20) || !near(g, 40) || !near(b, 230) {
		t.Errorf("blue patch pixel = %d,%d,%d (rgb)", r, g, b)
	}
	if b, g, r := px(&c, 100, 10); r != 0 || g != 0 || b != 0 {
		t.Errorf("untouched pixel = %d,%d,%d, want black", r, g, b)
	}
	// a second frame only changes one patch; the rest persists
	c.Apply(frameMsg(8, 128, 64, []any{64, 32, 32, 32, red[:0]}))
	if b, _, r := px(&c, 80, 48); !near(b, 230) || !near(r, 20) {
		t.Errorf("patch did not persist across frames")
	}
	if _, ok := c.Apply([]byte("junk")); ok {
		t.Error("junk accepted")
	}
}

func TestPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Overspan", "viewer.json")
	p := LoadPrefs(path)
	if len(p.Device) != 32 || p.Mode != "extend" || p.Quality != 70 || !p.Fullscreen {
		t.Fatalf("defaults: %+v", p)
	}
	p.Remember(Computer{Host: "10.0.0.2", Port: 7940, Name: "Desk"})
	p.Remember(Computer{Host: "10.0.0.3", Port: 7940})
	p.Remember(Computer{Host: "10.0.0.2", Port: 7940, Name: "Desk"})
	q := LoadPrefs(path)
	if q.Device != p.Device || len(q.Saved) != 2 || q.Saved[0].Host != "10.0.0.2" {
		t.Fatalf("reload: %+v", q.Saved)
	}
	q.Forget(Computer{Host: "10.0.0.2", Port: 7940})
	if s := LoadPrefs(path).SavedComputers(); len(s) != 1 || s[0].Host != "10.0.0.3" || !s[0].Saved {
		t.Fatalf("forget: %+v", s)
	}
	os.WriteFile(path, []byte(`{"quality":500,"fps":1,"mode":"weird","res":"x"}`), 0o600)
	r := LoadPrefs(path)
	if r.Quality != 95 || r.FPS != 10 || r.Mode != "extend" || r.Res != "balanced" {
		t.Fatalf("clamping: %+v", r)
	}
}
