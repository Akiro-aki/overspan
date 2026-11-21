package encode

import (
	"bytes"
	"image/jpeg"
	"testing"
)

func frame(w, h int) *Raw {
	return &Raw{Width: w, Height: h, Stride: w * 4, Layout: LayoutBGRX, Data: make([]byte, w*h*4)}
}

func TestOnlyChangedTilesAreSent(t *testing.T) {
	f := frame(256, 256)
	e := New(70)
	ps, err := e.Encode(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 4 || ps[0].W != 256 {
		t.Fatalf("first frame should be 4 full-width strips, got %d", len(ps))
	}
	if ps, _ := e.Encode(f, false); len(ps) != 0 {
		t.Fatalf("unchanged frame sent %d patches", len(ps))
	}
	// a 16x16 red block at (130, 70): strip 64..128, tile 128..192
	for y := 70; y < 86; y++ {
		for x := 130; x < 146; x++ {
			f.Data[(y*256+x)*4+2] = 255
		}
	}
	ps, _ = e.Encode(f, false)
	if len(ps) != 1 {
		t.Fatalf("want 1 patch, got %d", len(ps))
	}
	p := ps[0]
	if p.X != 128 || p.Y != 64 || p.W != 64 || p.H != 64 {
		t.Fatalf("patch %+v", p)
	}
	img, err := jpeg.Decode(bytes.NewReader(p.JPEG))
	if err != nil || img.Bounds().Dx() != 64 {
		t.Fatalf("patch is not a 64px JPEG: %v", err)
	}
	r, _, b, _ := img.At(10, 14).RGBA() // inside the block; BGRX byte 2 is red
	if r>>8 < 200 || b>>8 > 60 {
		t.Fatalf("colour order wrong: red=%d blue=%d", r>>8, b>>8)
	}
}

func TestYInvert(t *testing.T) {
	f := frame(64, 128)
	f.YInvert = true
	for i := 0; i < 64*16; i++ { // top 16 rows in memory = bottom rows on screen
		f.Data[i*4+2] = 255
	}
	ps, _ := New(90).Encode(f, true)
	last := ps[len(ps)-1]
	img, _ := jpeg.Decode(bytes.NewReader(last.JPEG))
	if r, _, _, _ := img.At(8, last.H-4).RGBA(); r>>8 < 128 {
		t.Fatal("y-inverted frame not flipped")
	}
}

func TestPackLayout(t *testing.T) {
	b := Pack(7, 100, 50, true, []Patch{{X: 1, Y: 2, W: 3, H: 4, JPEG: []byte{9, 9}}})
	if b[0] != 'F' || b[1] != 1 || b[2] != 7 || b[6] != 100 || b[8] != 50 || b[10] != 1 || len(b) != 12+12+2 {
		t.Fatalf("bad header % x", b)
	}
}
