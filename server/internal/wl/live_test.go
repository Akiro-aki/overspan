package wl

import (
	"os"
	"testing"
	"time"
)

// TestLiveCapture runs only inside a wlroots-style session with OVERSPAN_LIVE=1.
func TestLiveCapture(t *testing.T) {
	if os.Getenv("OVERSPAN_LIVE") == "" {
		t.Skip("set OVERSPAN_LIVE=1 inside a Wayland session")
	}
	s, err := NewScreencopy()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	outs, err := s.Outputs()
	if err != nil || len(outs) == 0 {
		t.Fatalf("outputs: %v %v", outs, err)
	}
	t.Logf("outputs: %+v", outs)
	start := time.Now()
	for i := 0; i < 10; i++ {
		f, err := s.Capture(outs[0].Name, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			t.Logf("frame %dx%d stride %d fmt %x yinv %v", f.Width, f.Height, f.Stride, f.Format, f.YInvert)
		}
	}
	t.Logf("10 captures in %v", time.Since(start))
}
