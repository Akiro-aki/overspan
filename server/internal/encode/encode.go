// Package encode turns raw captured frames into compact update messages.
// The frame is cut into horizontal strips; only strips whose pixels changed
// since the last frame are JPEG-encoded, and only across the changed columns.
// Strips are encoded in parallel, so a full-screen change uses every core.
package encode

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"runtime"
	"sync"
)

// Pixel layouts of the raw input.
const (
	LayoutBGRX = iota // little-endian XRGB/ARGB: bytes B, G, R, X
	LayoutRGBX        // little-endian XBGR/ABGR: bytes R, G, B, X
)

// Raw is one input frame.
type Raw struct {
	Width, Height, Stride int
	Layout                int
	YInvert               bool
	Data                  []byte
}

// Patch is one encoded rectangle.
type Patch struct {
	X, Y, W, H int
	JPEG       []byte
}

// Encoder keeps the previous frame to find what changed.
type Encoder struct {
	Quality int
	Strip   int // strip height in rows
	Tile    int // column granularity in pixels

	prev    []byte // tightly packed BGRX/RGBX copy of the last frame
	w, h    int
	layout  int
	workers int
}

func New(quality int) *Encoder {
	return &Encoder{Quality: quality, Strip: 64, Tile: 64, workers: runtime.NumCPU()}
}

// Reset forces the next Encode to send the whole frame.
func (e *Encoder) Reset() { e.prev = nil }

type job struct {
	x0, x1, y0, y1 int
}

// Encode returns the patches that changed. full forces a complete frame.
func (e *Encoder) Encode(f *Raw, full bool) ([]Patch, error) {
	rowBytes := f.Width * 4
	if e.prev == nil || e.w != f.Width || e.h != f.Height || e.layout != f.Layout {
		e.prev = make([]byte, rowBytes*f.Height)
		e.w, e.h, e.layout = f.Width, f.Height, f.Layout
		full = true
	}
	srcRow := func(y int) []byte {
		sy := y
		if f.YInvert {
			sy = f.Height - 1 - y
		}
		return f.Data[sy*f.Stride : sy*f.Stride+rowBytes]
	}

	var jobs []job
	tilesX := (f.Width + e.Tile - 1) / e.Tile
	for y0 := 0; y0 < f.Height; y0 += e.Strip {
		y1 := min(y0+e.Strip, f.Height)
		lo, hi := tilesX, -1
		if full {
			lo, hi = 0, tilesX-1
		} else {
			for y := y0; y < y1; y++ {
				cur := srcRow(y)
				old := e.prev[y*rowBytes : (y+1)*rowBytes]
				if bytes.Equal(cur, old) {
					continue
				}
				// only scan tiles outside the range already known dirty
				for t := 0; t < tilesX; t++ {
					if t >= lo && t <= hi {
						t = hi
						continue
					}
					a, b := t*e.Tile*4, min((t+1)*e.Tile, f.Width)*4
					if !bytes.Equal(cur[a:b], old[a:b]) {
						lo = min(lo, t)
						hi = max(hi, t)
					}
				}
			}
		}
		if hi >= lo {
			jobs = append(jobs, job{lo * e.Tile, min((hi+1)*e.Tile, f.Width), y0, y1})
		}
	}
	for y := 0; y < f.Height; y++ {
		copy(e.prev[y*rowBytes:], srcRow(y))
	}
	if len(jobs) == 0 {
		return nil, nil
	}

	patches := make([]Patch, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	next := make(chan int, len(jobs))
	for i := range jobs {
		next <- i
	}
	close(next)
	for w := 0; w < min(e.workers, len(jobs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				patches[i], errs[i] = e.encodeRect(jobs[i])
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return patches, nil
}

func (e *Encoder) encodeRect(j job) (Patch, error) {
	w, h := j.x1-j.x0, j.y1-j.y0
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBytes := e.w * 4
	for y := 0; y < h; y++ {
		src := e.prev[(j.y0+y)*rowBytes+j.x0*4 : (j.y0+y)*rowBytes+j.x1*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+w*4]
		if e.layout == LayoutBGRX {
			for i := 0; i < len(src); i += 4 {
				dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 255
			}
		} else {
			for i := 0; i < len(src); i += 4 {
				dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i], src[i+1], src[i+2], 255
			}
		}
	}
	var buf bytes.Buffer
	buf.Grow(w * h / 4)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: e.Quality}); err != nil {
		return Patch{}, err
	}
	return Patch{X: j.x0, Y: j.y0, W: w, H: h, JPEG: buf.Bytes()}, nil
}

// Message type bytes on the wire (first byte of a binary WebSocket message).
const (
	MsgFrame = 'F'
	MsgAudio = 'A'
)

// Pack builds one binary frame message:
//
//	'F' | flags u8 | seq u32 | width u16 | height u16 | count u16 |
//	count × ( x u16 | y u16 | w u16 | h u16 | len u32 | jpeg bytes )
//
// All integers little-endian. flags bit 0 = this frame replaces everything.
func Pack(seq uint32, width, height int, full bool, patches []Patch) []byte {
	size := 12
	for _, p := range patches {
		size += 12 + len(p.JPEG)
	}
	b := make([]byte, 0, size)
	var flags byte
	if full {
		flags = 1
	}
	b = append(b, MsgFrame, flags)
	b = binary.LittleEndian.AppendUint32(b, seq)
	b = binary.LittleEndian.AppendUint16(b, uint16(width))
	b = binary.LittleEndian.AppendUint16(b, uint16(height))
	b = binary.LittleEndian.AppendUint16(b, uint16(len(patches)))
	for _, p := range patches {
		b = binary.LittleEndian.AppendUint16(b, uint16(p.X))
		b = binary.LittleEndian.AppendUint16(b, uint16(p.Y))
		b = binary.LittleEndian.AppendUint16(b, uint16(p.W))
		b = binary.LittleEndian.AppendUint16(b, uint16(p.H))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(p.JPEG)))
		b = append(b, p.JPEG...)
	}
	return b
}
