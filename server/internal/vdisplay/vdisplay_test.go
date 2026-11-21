package vdisplay

import "testing"

func TestPlace(t *testing.T) {
	outs := []Output{{X: 0, Y: 0, W: 1920, H: 1080}, {X: 1920, Y: 0, W: 1536, H: 864}}
	cases := map[string][2]int{"right": {3456, 0}, "left": {-800, 0}, "above": {0, -1280}, "below": {0, 1080}}
	for side, want := range cases {
		x, y := Place(outs, 800, 1280, side)
		if x != want[0] || y != want[1] {
			t.Errorf("%s: got %d,%d want %v", side, x, y, want)
		}
	}
	if x, y, w, h := Bounds(outs); x != 0 || y != 0 || w != 3456 || h != 1080 {
		t.Errorf("bounds %d %d %d %d", x, y, w, h)
	}
}

func TestLogicalSize(t *testing.T) {
	if w, h := LogicalSize(2560, 1600, 2); w != 1280 || h != 800 {
		t.Fatalf("%d %d", w, h)
	}
}

func TestModeline(t *testing.T) {
	clock, tm := Modeline(1920, 1080, 60)
	// CVT reduced blanking for 1080p60 is about 138.5 MHz, 2080 x 1111
	if clock < 135 || clock > 142 || tm[3] != 2080 || tm[7] < 1105 || tm[7] > 1115 {
		t.Fatalf("clock %.2f timings %v", clock, tm)
	}
}
