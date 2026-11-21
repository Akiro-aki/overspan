// Package capture produces raw frames from one monitor.
package capture

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"overspan/internal/encode"
	"overspan/internal/vdisplay"
	"overspan/internal/wl"
)

// Capturer yields frames of one output. Next may block until the screen
// changes. Close (from any goroutine) unblocks it.
type Capturer interface {
	Next(full bool) (*encode.Raw, error)
	Close()
}

// Open picks the capturer that fits the backend.
func Open(kind vdisplay.CaptureKind, out vdisplay.Output, fps int, cursor bool) (Capturer, error) {
	switch kind {
	case vdisplay.CaptureScreencopy:
		s, err := wl.NewScreencopy()
		if err != nil {
			return nil, err
		}
		return &screencopy{s: s, output: out.Name, cursor: cursor}, nil
	case vdisplay.CaptureX11:
		return openX11(out, fps, cursor)
	}
	return nil, errors.New("no capture method for this desktop")
}

type screencopy struct {
	s      *wl.Screencopy
	output string
	cursor bool
	first  bool
	mu     sync.Mutex
	closed bool
}

func (c *screencopy) Next(full bool) (*encode.Raw, error) {
	// The first frame and forced refreshes must not wait for damage, or a
	// still screen would never produce its first picture.
	wait := c.first && !full
	c.first = true
	f, err := c.s.Capture(c.output, c.cursor, wait)
	if err != nil {
		return nil, err
	}
	layout := encode.LayoutBGRX
	if f.Format == wl.FormatABGR8888 || f.Format == wl.FormatXBGR8888 {
		layout = encode.LayoutRGBX
	}
	return &encode.Raw{Width: f.Width, Height: f.Height, Stride: f.Stride, Layout: layout, YInvert: f.YInvert, Data: f.Data}, nil
}

func (c *screencopy) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		c.s.Interrupt()
	}
}

// x11 reads raw frames from ffmpeg's x11grab, which ships with ffmpeg on
// every distribution.
type x11 struct {
	cmd  *exec.Cmd
	r    *bufio.Reader
	w, h int
	buf  []byte
}

func openX11(out vdisplay.Output, fps int, cursor bool) (Capturer, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("X11 capture needs ffmpeg installed")
	}
	disp := os.Getenv("DISPLAY")
	mouse := "0"
	if cursor {
		mouse = "1"
	}
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-f", "x11grab", "-draw_mouse", mouse,
		"-framerate", fmt.Sprint(fps), "-video_size", fmt.Sprintf("%dx%d", out.PxW, out.PxH),
		"-i", fmt.Sprintf("%s+%d,%d", disp, out.X, out.Y),
		"-f", "rawvideo", "-pix_fmt", "bgr0", "pipe:1")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &x11{cmd: cmd, r: bufio.NewReaderSize(stdout, 1<<20), w: out.PxW, h: out.PxH, buf: make([]byte, out.PxW*out.PxH*4)}, nil
}

func (c *x11) Next(bool) (*encode.Raw, error) {
	if _, err := io.ReadFull(c.r, c.buf); err != nil {
		return nil, fmt.Errorf("x11grab stopped: %w", err)
	}
	return &encode.Raw{Width: c.w, Height: c.h, Stride: c.w * 4, Layout: encode.LayoutBGRX, Data: c.buf}, nil
}

func (c *x11) Close() {
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
}
