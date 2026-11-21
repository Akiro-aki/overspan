package client

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"sync"
)

// Canvas holds the remote picture as top-down BGRX pixels, the layout a
// Windows DIB section wants. Frames only carry changed patches, so the canvas
// persists between frames.
type Canvas struct {
	mu  sync.Mutex
	W   int
	H   int
	Pix []byte
}

// Lock gives the painter exclusive access to W, H and Pix.
func (c *Canvas) Lock()   { c.mu.Lock() }
func (c *Canvas) Unlock() { c.mu.Unlock() }

// Size returns the current picture size, 0×0 before the first frame.
func (c *Canvas) Size() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.W, c.H
}

// Clear forgets the picture.
func (c *Canvas) Clear() {
	c.mu.Lock()
	c.W, c.H, c.Pix = 0, 0, nil
	c.mu.Unlock()
}

type patch struct {
	x, y int
	data []byte
}

// Apply decodes one frame message:
//
//	'F' | flags u8 | seq u32 | w u16 | h u16 | count u16 | count × (x,y,w,h u16, len u32, jpeg)
//
// all little-endian. It returns the sequence number to acknowledge.
func (c *Canvas) Apply(msg []byte) (seq uint32, ok bool) {
	if len(msg) < 12 || msg[0] != 'F' {
		return 0, false
	}
	le := binary.LittleEndian
	seq = le.Uint32(msg[2:])
	w, h, n := int(le.Uint16(msg[6:])), int(le.Uint16(msg[8:])), int(le.Uint16(msg[10:]))
	if w == 0 || h == 0 {
		return 0, false
	}
	off := 12
	patches := make([]patch, 0, n)
	for i := 0; i < n && off+12 <= len(msg); i++ {
		x, y := int(le.Uint16(msg[off:])), int(le.Uint16(msg[off+2:]))
		l := int(le.Uint32(msg[off+8:]))
		start := off + 12
		if l < 0 || start+l > len(msg) {
			break
		}
		patches = append(patches, patch{x, y, msg[start : start+l]})
		off = start + l
	}

	c.mu.Lock()
	if c.W != w || c.H != h || c.Pix == nil {
		c.W, c.H, c.Pix = w, h, make([]byte, w*h*4)
	}
	c.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, p := range patches {
		wg.Add(1)
		sem <- struct{}{}
		go func(p patch) {
			defer func() { <-sem; wg.Done() }()
			img, err := jpeg.Decode(bytes.NewReader(p.data))
			if err != nil {
				return
			}
			c.blit(img, p.x, p.y, w, h)
		}(p)
	}
	wg.Wait()
	return seq, true
}

// blit converts a decoded patch into BGRX and copies it in at (px, py).
// Patches never overlap, so each one only takes the lock to check the size.
func (c *Canvas) blit(img image.Image, px, py, fw, fh int) {
	b := img.Bounds()
	w, h := min(b.Dx(), fw-px), min(b.Dy(), fh-py)
	if w <= 0 || h <= 0 {
		return
	}
	row := make([]byte, w*4)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.W != fw || c.H != fh {
		return // resized while decoding; the next full frame repaints
	}
	stride := c.W * 4
	switch m := img.(type) {
	case *image.YCbCr:
		for y := 0; y < h; y++ {
			yi := m.YOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				ci := m.COffset(b.Min.X+x, b.Min.Y+y)
				r, g, bl := ycc(m.Y[yi+x], m.Cb[ci], m.Cr[ci])
				row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = bl, g, r, 0xff
			}
			copy(c.Pix[(py+y)*stride+px*4:], row)
		}
	case *image.Gray:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := m.Pix[(y)*m.Stride+x]
				row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = v, v, v, 0xff
			}
			copy(c.Pix[(py+y)*stride+px*4:], row)
		}
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = byte(bl>>8), byte(g>>8), byte(r>>8), 0xff
			}
			copy(c.Pix[(py+y)*stride+px*4:], row)
		}
	}
}

// ycc is the JFIF YCbCr to RGB conversion in 16.16 fixed point.
func ycc(y, cb, cr uint8) (uint8, uint8, uint8) {
	yy := int32(y) << 16
	b1 := int32(cb) - 128
	r1 := int32(cr) - 128
	r := (yy + 91881*r1 + 1<<15) >> 16
	g := (yy - 22554*b1 - 46802*r1 + 1<<15) >> 16
	b := (yy + 116130*b1 + 1<<15) >> 16
	return clamp8(r), clamp8(g), clamp8(b)
}

func clamp8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
