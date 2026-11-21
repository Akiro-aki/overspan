package wl

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"syscall"
)

// wl_shm formats we know how to read.
const (
	FormatARGB8888 = 0
	FormatXRGB8888 = 1
	FormatABGR8888 = 0x34324241 // 'AB24'
	FormatXBGR8888 = 0x34324258 // 'XB24'
)

// OutputInfo describes a wl_output as advertised by the compositor.
type OutputInfo struct {
	Name        string
	Description string
	Width       int // current mode, physical pixels
	Height      int
}

type output struct {
	OutputInfo
	global uint32
	id     uint32
}

// Frame is one captured image. Data stays valid until the second Capture
// call after the one that returned it (captures alternate two buffers).
type Frame struct {
	Width, Height, Stride int
	Format                uint32
	YInvert               bool
	Data                  []byte
	Damage                [][4]int // x, y, w, h as reported by the compositor
}

type shmBuf struct {
	id     uint32
	pool   uint32
	fd     int
	data   []byte
	w, h   int
	stride int
	format uint32
}

// Screencopy captures outputs through zwlr_screencopy_manager_v1. One
// instance must be used from one goroutine.
type Screencopy struct {
	c       *Conn
	reg     uint32
	shm     uint32
	mgr     uint32
	mgrVer  uint32
	outputs map[uint32]*output // by global name
	bufs    [2]*shmBuf
	turn    int
}

// ErrNoScreencopy means the compositor does not offer wlr-screencopy.
var ErrNoScreencopy = errors.New("compositor does not support wlr-screencopy")

// NewScreencopy connects and binds the globals it needs.
func NewScreencopy() (*Screencopy, error) {
	c, err := Dial()
	if err != nil {
		return nil, err
	}
	s := &Screencopy{c: c, outputs: map[uint32]*output{}}
	s.reg = c.NewID(s.registryEvent)
	if err := c.Send(1, 1, NewWriter().Uint(s.reg)); err != nil {
		c.Close()
		return nil, err
	}
	// two roundtrips: globals, then the output name/mode events
	if err := c.Roundtrip(); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.Roundtrip(); err != nil {
		c.Close()
		return nil, err
	}
	if s.shm == 0 {
		c.Close()
		return nil, errors.New("compositor offers no wl_shm")
	}
	if s.mgr == 0 {
		c.Close()
		return nil, ErrNoScreencopy
	}
	return s, nil
}

func (s *Screencopy) bind(name uint32, iface string, version uint32, h Handler) uint32 {
	id := s.c.NewID(h)
	s.c.Send(s.reg, 0, NewWriter().Uint(name).String(iface).Uint(version).Uint(id))
	return id
}

func (s *Screencopy) registryEvent(e *Event) {
	r := NewReader(e.Args)
	switch e.Opcode {
	case 0: // global
		name := r.Uint()
		iface := r.String()
		ver := r.Uint()
		switch iface {
		case "wl_shm":
			s.shm = s.bind(name, iface, 1, nil)
		case "zwlr_screencopy_manager_v1":
			s.mgrVer = min(ver, 3)
			s.mgr = s.bind(name, iface, s.mgrVer, nil)
		case "wl_output":
			o := &output{global: name}
			o.id = s.bind(name, iface, min(ver, 4), func(e *Event) { outputEvent(o, e) })
			s.outputs[name] = o
		}
	case 1: // global_remove
		name := r.Uint()
		if o, ok := s.outputs[name]; ok {
			delete(s.outputs, name)
			s.c.Send(o.id, 0, nil) // wl_output.release (v3+); harmless if ignored
		}
	}
}

func outputEvent(o *output, e *Event) {
	r := NewReader(e.Args)
	switch e.Opcode {
	case 1: // mode: flags, width, height, refresh
		flags := r.Uint()
		w, h := r.Int(), r.Int()
		if flags&1 != 0 { // current
			o.Width, o.Height = int(w), int(h)
		}
	case 4:
		o.Name = r.String()
	case 5:
		o.Description = r.String()
	}
}

// Outputs returns the outputs the compositor currently advertises.
func (s *Screencopy) Outputs() ([]OutputInfo, error) {
	if err := s.c.Roundtrip(); err != nil {
		return nil, err
	}
	if err := s.c.Roundtrip(); err != nil {
		return nil, err
	}
	var out []OutputInfo
	for _, o := range s.outputs {
		out = append(out, o.OutputInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Screencopy) find(name string) *output {
	for _, o := range s.outputs {
		if o.Name == name {
			return o
		}
	}
	return nil
}

func (s *Screencopy) buffer(w, h, stride int, format uint32) (*shmBuf, error) {
	b := s.bufs[s.turn]
	if b != nil && b.w == w && b.h == h && b.stride == stride && b.format == format {
		return b, nil
	}
	if b != nil {
		s.freeBuf(b)
	}
	size := stride * h
	dir := os.Getenv("XDG_RUNTIME_DIR")
	f, err := os.CreateTemp(dir, "overspan-shm-*")
	if err != nil {
		return nil, err
	}
	os.Remove(f.Name())
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, err
	}
	fd, err := syscall.Dup(int(f.Fd()))
	f.Close()
	if err != nil {
		return nil, err
	}
	data, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	nb := &shmBuf{fd: fd, data: data, w: w, h: h, stride: stride, format: format}
	nb.pool = s.c.NewID(nil)
	if err := s.c.Send(s.shm, 0, NewWriter().Uint(nb.pool).Int(int32(size)), fd); err != nil {
		return nil, err
	}
	nb.id = s.c.NewID(nil)
	s.c.Send(nb.pool, 0, NewWriter().Uint(nb.id).Int(0).Int(int32(w)).Int(int32(h)).Int(int32(stride)).Uint(format))
	s.bufs[s.turn] = nb
	return nb, nil
}

func (s *Screencopy) freeBuf(b *shmBuf) {
	s.c.Send(b.id, 0, nil)   // wl_buffer.destroy
	s.c.Send(b.pool, 1, nil) // wl_shm_pool.destroy
	syscall.Munmap(b.data)
	syscall.Close(b.fd)
}

// Capture grabs one frame of the named output. With waitDamage the call
// blocks until something on the output changed (copy_with_damage), which
// keeps an idle desktop from costing any CPU.
func (s *Screencopy) Capture(outputName string, cursor, waitDamage bool) (*Frame, error) {
	o := s.find(outputName)
	if o == nil {
		if _, err := s.Outputs(); err != nil {
			return nil, err
		}
		if o = s.find(outputName); o == nil {
			return nil, fmt.Errorf("output %q not found", outputName)
		}
	}
	var (
		fr                          Frame
		gotBuffer, bufferDone, done bool
		failed                      bool
		flags                       uint32
	)
	frameID := s.c.NewID(func(e *Event) {
		r := NewReader(e.Args)
		switch e.Opcode {
		case 0: // buffer
			f := r.Uint()
			w, h, st := r.Uint(), r.Uint(), r.Uint()
			if !gotBuffer {
				fr.Format, fr.Width, fr.Height, fr.Stride = f, int(w), int(h), int(st)
				gotBuffer = true
			}
		case 1:
			flags = r.Uint()
		case 2:
			done = true
		case 3:
			failed = true
		case 4:
			x, y, w, h := r.Uint(), r.Uint(), r.Uint(), r.Uint()
			fr.Damage = append(fr.Damage, [4]int{int(x), int(y), int(w), int(h)})
		case 6:
			bufferDone = true
		}
	})
	overlay := int32(0)
	if cursor {
		overlay = 1
	}
	if err := s.c.Send(s.mgr, 0, NewWriter().Uint(frameID).Int(overlay).Uint(o.id)); err != nil {
		return nil, err
	}
	defer s.c.Send(frameID, 1, nil) // destroy
	for !failed && !(gotBuffer && (s.mgrVer < 3 || bufferDone)) {
		if err := s.c.Dispatch(); err != nil {
			return nil, err
		}
	}
	if failed {
		return nil, errors.New("screencopy failed (output gone or busy)")
	}
	switch fr.Format {
	case FormatARGB8888, FormatXRGB8888, FormatABGR8888, FormatXBGR8888:
	default:
		return nil, fmt.Errorf("unsupported shm format 0x%x", fr.Format)
	}
	b, err := s.buffer(fr.Width, fr.Height, fr.Stride, fr.Format)
	if err != nil {
		return nil, err
	}
	op := uint16(0) // copy
	if waitDamage && s.mgrVer >= 2 {
		op = 2 // copy_with_damage
	}
	if err := s.c.Send(frameID, op, NewWriter().Uint(b.id)); err != nil {
		return nil, err
	}
	for !done && !failed {
		if err := s.c.Dispatch(); err != nil {
			return nil, err
		}
	}
	if failed {
		return nil, errors.New("screencopy copy failed")
	}
	s.turn ^= 1
	fr.YInvert = flags&1 != 0
	fr.Data = b.data
	return &fr, nil
}

// Close releases the connection and buffers.
func (s *Screencopy) Close() {
	for _, b := range s.bufs {
		if b != nil {
			syscall.Munmap(b.data)
			syscall.Close(b.fd)
		}
	}
	s.c.Close()
}

// Interrupt unblocks a Capture waiting for damage, from another goroutine.
func (s *Screencopy) Interrupt() { s.c.Close() }
