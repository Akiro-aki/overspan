package input

import (
	"encoding/binary"
	"unsafe"
)

// Pen events go to a separate virtual graphics tablet so drawing programs
// receive real pressure, tilt and the barrel button. Like the pointer, the
// tablet covers the whole desktop.

const (
	absPressure = 0x18
	absTiltX    = 0x1a
	absTiltY    = 0x1b

	btnToolPen    = 0x140
	btnToolRubber = 0x141
	btnTouch      = 0x14a
	btnStylus     = 0x14b
	btnStylus2    = 0x14c

	// PressureMax is the resolution of the pressure axis.
	PressureMax = 4095
)

func absSetupFull(fd uintptr, code int, min, max, res int32) error {
	var a [28]byte
	binary.LittleEndian.PutUint16(a[0:], uint16(code))
	binary.LittleEndian.PutUint32(a[8:], uint32(min))
	binary.LittleEndian.PutUint32(a[12:], uint32(max))
	binary.LittleEndian.PutUint32(a[24:], uint32(res))
	return ioctl(fd, uiAbsSetup, uintptr(unsafe.Pointer(&a[0])))
}

func openPen() (*device, error) {
	return openDevice("Overspan pen", func(fd uintptr) error {
		if err := setBits(fd, uiSetEvBit, evKey, evAbs, evSyn); err != nil {
			return err
		}
		if err := setBits(fd, uiSetKeyBit, btnToolPen, btnToolRubber, btnTouch, btnStylus, btnStylus2); err != nil {
			return err
		}
		if err := setBits(fd, uiSetAbsBit, absX, absY, absPressure, absTiltX, absTiltY); err != nil {
			return err
		}
		// 100 units per mm keeps libinput happy (it wants a physical size)
		for _, s := range []struct {
			code          int
			min, max, res int32
		}{{absX, 0, AbsMax, 100}, {absY, 0, AbsMax, 100}, {absPressure, 0, PressureMax, 0}, {absTiltX, -90, 90, 57}, {absTiltY, -90, 90, 57}} {
			if err := absSetupFull(fd, s.code, s.min, s.max, s.res); err != nil {
				return err
			}
		}
		return nil
	})
}

// PenState is one pen sample from a viewer.
type PenState struct {
	X, Y         int32 // 0..AbsMax over the desktop
	Pressure     int32 // 0..PressureMax
	TiltX, TiltY int32 // degrees
	InRange      bool  // hovering or touching
	Touching     bool
	Barrel       bool // first side button
	Eraser       bool
}

// Pen sends one pen sample. Proximity, contact and buttons are diffed
// against the previous sample, so callers just report the current state.
func (in *Injector) Pen(s PenState) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.pen == nil {
		return nil
	}
	tool := btnToolPen
	if s.Eraser {
		tool = btnToolRubber
	}
	prev := in.penPrev
	var evs [][3]int32
	if !s.InRange {
		if !prev.InRange {
			return nil
		}
		ptool := btnToolPen
		if prev.Eraser {
			ptool = btnToolRubber
		}
		evs = append(evs, [3]int32{evAbs, absPressure, 0}, [3]int32{evKey, btnTouch, 0},
			[3]int32{evKey, btnStylus, 0}, [3]int32{evKey, int32(ptool), 0})
		in.penPrev = PenState{}
		return in.pen.emit(evs...)
	}
	if !prev.InRange || prev.Eraser != s.Eraser {
		evs = append(evs, [3]int32{evKey, int32(tool), 1})
	}
	evs = append(evs,
		[3]int32{evAbs, absX, clamp(s.X)}, [3]int32{evAbs, absY, clamp(s.Y)},
		[3]int32{evAbs, absPressure, max(0, min(PressureMax, s.Pressure))},
		[3]int32{evAbs, absTiltX, max(-90, min(90, s.TiltX))},
		[3]int32{evAbs, absTiltY, max(-90, min(90, s.TiltY))})
	if s.Touching != prev.Touching {
		evs = append(evs, [3]int32{evKey, btnTouch, b2i(s.Touching)})
	}
	if s.Barrel != prev.Barrel {
		evs = append(evs, [3]int32{evKey, btnStylus, b2i(s.Barrel)})
	}
	in.penPrev = s
	return in.pen.emit(evs...)
}

// penOut lifts the pen if it was left in range (viewer dropped).
func (in *Injector) penOut() {
	if in.pen != nil && in.penPrev.InRange {
		in.pen.emit([3]int32{evAbs, absPressure, 0}, [3]int32{evKey, btnTouch, 0},
			[3]int32{evKey, btnStylus, 0}, [3]int32{evKey, btnToolPen, 0}, [3]int32{evKey, btnToolRubber, 0})
		in.penPrev = PenState{}
	}
}
