package qr

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeWithZbar(t *testing.T) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg not installed")
	}
	cases := []string{
		"hi",
		"http://192.168.1.23:7940/",
		"http://192.168.100.200:7940/?k=0123456789abcdef0123456789abcdef",
		strings.Repeat("overspan-", 15), // version 7+, exercises version bits
		strings.Repeat("x", 200),
	}
	dir := t.TempDir()
	for i, text := range cases {
		c, err := Encode(text)
		if err != nil {
			t.Fatal(err)
		}
		scale, q := 8, 4
		n := (c.Size + 2*q) * scale
		img := image.NewGray(image.Rect(0, 0, n, n))
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				mx, my := x/scale-q, y/scale-q
				v := uint8(255)
				if mx >= 0 && my >= 0 && mx < c.Size && my < c.Size && c.Modules[my][mx] {
					v = 0
				}
				img.SetGray(x, y, color.Gray{v})
			}
		}
		p := filepath.Join(dir, "q.png")
		f, _ := os.Create(p)
		png.Encode(f, img)
		f.Close()
		out, err := exec.Command("zbarimg", "--raw", "-q", p).Output()
		if err != nil {
			t.Fatalf("case %d (size %d): zbar could not decode: %v", i, c.Size, err)
		}
		if got := strings.TrimRight(string(out), "\n"); got != text {
			t.Fatalf("case %d: got %q", i, got)
		}
	}
}
