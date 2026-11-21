// Package input injects pointer and keyboard events through /dev/uinput.
// uinput is a kernel interface, so this works the same under X11, every
// Wayland compositor and the console, on every distribution.
//
// The pointer is an absolute device (like a virtual-machine tablet). libinput
// and X11 map such devices onto the whole desktop, so a position on the
// viewer becomes a position in global layout coordinates.
package input

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	synReport = 0

	absX = 0x00
	absY = 0x01

	relHWheel      = 0x06
	relWheel       = 0x08
	relWheelHiRes  = 0x0b
	relHWheelHiRes = 0x0c

	BtnLeft   = 0x110
	BtnRight  = 0x111
	BtnMiddle = 0x112
	BtnSide   = 0x113
	BtnExtra  = 0x114

	// AbsMax is the resolution of the absolute axes.
	AbsMax = 65535
)

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'U'<<8 | nr }

var (
	uiDevCreate  = ioc(0, 1, 0)
	uiDevDestroy = ioc(0, 2, 0)
	uiDevSetup   = ioc(1, 3, 92)
	uiAbsSetup   = ioc(1, 4, 28)
	uiSetEvBit   = ioc(1, 100, 4)
	uiSetKeyBit  = ioc(1, 101, 4)
	uiSetRelBit  = ioc(1, 102, 4)
	uiSetAbsBit  = ioc(1, 103, 4)
)

// ErrNoAccess explains the usual permission problem.
var ErrNoAccess = errors.New("cannot open /dev/uinput: install the udev rule (overspan install-udev) or add yourself to the input group")

type device struct {
	f *os.File
}

func ioctl(fd uintptr, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

func openDevice(name string, setup func(fd uintptr) error) (*device, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsPermission(err) {
			return nil, ErrNoAccess
		}
		return nil, err
	}
	fd := f.Fd()
	if err := setup(fd); err != nil {
		f.Close()
		return nil, err
	}
	var s [92]byte
	binary.LittleEndian.PutUint16(s[0:], 0x06)   // BUS_VIRTUAL
	binary.LittleEndian.PutUint16(s[2:], 0x4f53) // vendor
	binary.LittleEndian.PutUint16(s[4:], 0x0001) // product
	binary.LittleEndian.PutUint16(s[6:], 1)      // version
	copy(s[8:87], name)
	if err := ioctl(fd, uiDevSetup, uintptr(unsafe.Pointer(&s[0]))); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_SETUP: %w", err)
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}
	return &device{f: f}, nil
}

func (d *device) close() {
	ioctl(d.f.Fd(), uiDevDestroy, 0)
	d.f.Close()
}

// input_event: struct timeval, u16 type, u16 code, s32 value.
var tvSize = int(unsafe.Sizeof(syscall.Timeval{}))

func (d *device) emit(events ...[3]int32) error {
	rec := tvSize + 8
	buf := make([]byte, rec*(len(events)+1))
	for i, ev := range append(events, [3]int32{evSyn, synReport, 0}) {
		o := i*rec + tvSize
		binary.LittleEndian.PutUint16(buf[o:], uint16(ev[0]))
		binary.LittleEndian.PutUint16(buf[o+2:], uint16(ev[1]))
		binary.LittleEndian.PutUint32(buf[o+4:], uint32(ev[2]))
	}
	_, err := d.f.Write(buf)
	return err
}

func setBits(fd uintptr, req uintptr, codes ...int) error {
	for _, c := range codes {
		if err := ioctl(fd, req, uintptr(c)); err != nil {
			return err
		}
	}
	return nil
}

func absSetup(fd uintptr, code int, max int32) error {
	var a [28]byte
	binary.LittleEndian.PutUint16(a[0:], uint16(code))
	binary.LittleEndian.PutUint32(a[12:], uint32(max)) // absinfo.maximum
	return ioctl(fd, uiAbsSetup, uintptr(unsafe.Pointer(&a[0])))
}

// Injector owns one virtual pointer and one virtual keyboard. It is shared
// by every viewer.
type Injector struct {
	mu      sync.Mutex
	ptr     *device
	kbd     *device
	held    map[int]bool // keys and buttons currently down, released on Close/ReleaseAll
	lastPos [2]int32
}

// Open creates the virtual devices.
func Open() (*Injector, error) {
	ptr, err := openDevice("Overspan pointer", func(fd uintptr) error {
		if err := setBits(fd, uiSetEvBit, evKey, evAbs, evRel, evSyn); err != nil {
			return err
		}
		if err := setBits(fd, uiSetKeyBit, BtnLeft, BtnRight, BtnMiddle, BtnSide, BtnExtra); err != nil {
			return err
		}
		if err := setBits(fd, uiSetRelBit, relWheel, relHWheel, relWheelHiRes, relHWheelHiRes); err != nil {
			return err
		}
		if err := setBits(fd, uiSetAbsBit, absX, absY); err != nil {
			return err
		}
		if err := absSetup(fd, absX, AbsMax); err != nil {
			return err
		}
		return absSetup(fd, absY, AbsMax)
	})
	if err != nil {
		return nil, err
	}
	kbd, err := openDevice("Overspan keyboard", func(fd uintptr) error {
		if err := setBits(fd, uiSetEvBit, evKey, evSyn); err != nil {
			return err
		}
		for k := 1; k < 256; k++ {
			if err := ioctl(fd, uiSetKeyBit, uintptr(k)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		ptr.close()
		return nil, err
	}
	// give udev and the compositor a moment to pick the devices up
	time.Sleep(150 * time.Millisecond)
	return &Injector{ptr: ptr, kbd: kbd, held: map[int]bool{}}, nil
}

// MoveAbs moves the pointer; x and y are in 0..AbsMax over the whole desktop.
func (in *Injector) MoveAbs(x, y int32) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	x, y = clamp(x), clamp(y)
	in.lastPos = [2]int32{x, y}
	return in.ptr.emit([3]int32{evAbs, absX, x}, [3]int32{evAbs, absY, y})
}

// Button presses or releases a mouse button (BtnLeft...).
func (in *Injector) Button(code int, down bool) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.held[code] == down {
		return nil
	}
	in.held[code] = down
	return in.ptr.emit([3]int32{evKey, int32(code), b2i(down)})
}

// Wheel scrolls; units are 1/120 of a detent, positive = down / right.
func (in *Injector) Wheel(dx, dy int32) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	var evs [][3]int32
	if dy != 0 {
		evs = append(evs, [3]int32{evRel, relWheelHiRes, -dy})
		if d := dy / 120; d != 0 {
			evs = append(evs, [3]int32{evRel, relWheel, -d})
		}
	}
	if dx != 0 {
		evs = append(evs, [3]int32{evRel, relHWheelHiRes, dx})
		if d := dx / 120; d != 0 {
			evs = append(evs, [3]int32{evRel, relHWheel, d})
		}
	}
	if len(evs) == 0 {
		return nil
	}
	return in.ptr.emit(evs...)
}

// Key presses or releases an evdev key code.
func (in *Injector) Key(code int, down bool) error {
	if code <= 0 || code > 255 {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.held[code] == down && !down {
		return nil
	}
	in.held[code] = down
	return in.kbd.emit([3]int32{evKey, int32(code), b2i(down)})
}

// ReleaseAll lifts every key and button still held, so a viewer that drops
// mid-drag cannot leave a stuck modifier behind.
func (in *Injector) ReleaseAll() {
	in.mu.Lock()
	defer in.mu.Unlock()
	for code, down := range in.held {
		if !down {
			continue
		}
		dev := in.kbd
		if code >= BtnLeft {
			dev = in.ptr
		}
		dev.emit([3]int32{evKey, int32(code), 0})
		in.held[code] = false
	}
}

func (in *Injector) Close() {
	in.ReleaseAll()
	in.ptr.close()
	in.kbd.close()
}

func clamp(v int32) int32 { return max(0, min(AbsMax, v)) }

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
