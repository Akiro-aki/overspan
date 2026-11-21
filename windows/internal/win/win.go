//go:build windows

// Package win is the slice of the Win32 API the viewer uses, called through
// the standard library's syscall package (no cgo, no third-party modules).
package win

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdiplus  = syscall.NewLazyDLL("gdiplus.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
)

// Proc wraps a lazily loaded export. Optional exports (newer Windows only)
// are checked with Available before use.
type Proc struct{ p *syscall.LazyProc }

func (p Proc) Call(a ...uintptr) uintptr { r, _, _ := p.p.Call(a...); return r }

// Call2 also returns the error from GetLastError.
func (p Proc) Call2(a ...uintptr) (uintptr, error) { r, _, e := p.p.Call(a...); return r, e }

func (p Proc) Available() bool { return p.p.Find() == nil }

func u(n string) Proc  { return Proc{user32.NewProc(n)} }
func g(n string) Proc  { return Proc{gdi32.NewProc(n)} }
func k(n string) Proc  { return Proc{kernel32.NewProc(n)} }
func gp(n string) Proc { return Proc{gdiplus.NewProc(n)} }

var (
	RegisterClassExW              = u("RegisterClassExW")
	CreateWindowExW               = u("CreateWindowExW")
	DestroyWindow                 = u("DestroyWindow")
	DefWindowProcW                = u("DefWindowProcW")
	GetMessageW                   = u("GetMessageW")
	TranslateMessage              = u("TranslateMessage")
	DispatchMessageW              = u("DispatchMessageW")
	PostMessageW                  = u("PostMessageW")
	SendMessageW                  = u("SendMessageW")
	PostQuitMessage               = u("PostQuitMessage")
	ShowWindow                    = u("ShowWindow")
	UpdateWindow                  = u("UpdateWindow")
	InvalidateRect                = u("InvalidateRect")
	BeginPaint                    = u("BeginPaint")
	EndPaint                      = u("EndPaint")
	GetClientRect                 = u("GetClientRect")
	GetWindowRect                 = u("GetWindowRect")
	SetWindowPos                  = u("SetWindowPos")
	MoveWindow                    = u("MoveWindow")
	SetWindowTextW                = u("SetWindowTextW")
	GetWindowTextW                = u("GetWindowTextW")
	GetWindowTextLengthW          = u("GetWindowTextLengthW")
	SetFocus                      = u("SetFocus")
	GetFocus                      = u("GetFocus")
	SetCapture                    = u("SetCapture")
	ReleaseCapture                = u("ReleaseCapture")
	LoadCursorW                   = u("LoadCursorW")
	LoadImageW                    = u("LoadImageW")
	SetCursor                     = u("SetCursor")
	SetTimer                      = u("SetTimer")
	KillTimer                     = u("KillTimer")
	IsDialogMessageW              = u("IsDialogMessageW")
	GetWindowLongPtrW             = u("GetWindowLongPtrW")
	SetWindowLongPtrW             = u("SetWindowLongPtrW")
	CallWindowProcW               = u("CallWindowProcW")
	GetDpiForWindow               = u("GetDpiForWindow")
	SetProcessDpiAwarenessContext = u("SetProcessDpiAwarenessContext")
	SetProcessDPIAware            = u("SetProcessDPIAware")
	MonitorFromWindow             = u("MonitorFromWindow")
	GetMonitorInfoW               = u("GetMonitorInfoW")
	GetWindowPlacement            = u("GetWindowPlacement")
	SetWindowPlacement            = u("SetWindowPlacement")
	ScreenToClient                = u("ScreenToClient")
	ClientToScreen                = u("ClientToScreen")
	GetForegroundWindow           = u("GetForegroundWindow")
	SetForegroundWindow           = u("SetForegroundWindow")
	SetWindowsHookExW             = u("SetWindowsHookExW")
	UnhookWindowsHookEx           = u("UnhookWindowsHookEx")
	CallNextHookEx                = u("CallNextHookEx")
	EnableWindow                  = u("EnableWindow")
	IsWindowEnabled               = u("IsWindowEnabled")
	IsWindowVisible               = u("IsWindowVisible")
	GetKeyState                   = u("GetKeyState")
	TrackMouseEvent               = u("TrackMouseEvent")
	GetPointerType                = u("GetPointerType")
	GetPointerPenInfo             = u("GetPointerPenInfo")
	DrawTextW                     = u("DrawTextW")
	FillRect                      = u("FillRect")
	GetDC                         = u("GetDC")
	ReleaseDC                     = u("ReleaseDC")
	PrintWindow                   = u("PrintWindow")
	EnumChildWindows              = u("EnumChildWindows")
	GetClassNameW                 = u("GetClassNameW")
	FindWindowW                   = u("FindWindowW")
	GetDlgCtrlID                  = u("GetDlgCtrlID")
	IsWindow                      = u("IsWindow")
	GetSystemMetrics              = u("GetSystemMetrics")
	MessageBeep                   = u("MessageBeep")
	GetCursorPos                  = u("GetCursorPos")
	WindowFromPoint               = u("WindowFromPoint")

	CreateCompatibleDC     = g("CreateCompatibleDC")
	CreateCompatibleBitmap = g("CreateCompatibleBitmap")
	CreateDIBSection       = g("CreateDIBSection")
	SelectObject           = g("SelectObject")
	DeleteObject           = g("DeleteObject")
	DeleteDC               = g("DeleteDC")
	BitBlt                 = g("BitBlt")
	StretchDIBits          = g("StretchDIBits")
	SetStretchBltMode      = g("SetStretchBltMode")
	SetBrushOrgEx          = g("SetBrushOrgEx")
	CreateSolidBrush       = g("CreateSolidBrush")
	CreateFontW            = g("CreateFontW")
	SetTextColor           = g("SetTextColor")
	SetBkMode              = g("SetBkMode")
	SetBkColor             = g("SetBkColor")
	GetTextExtentPoint32W  = g("GetTextExtentPoint32W")
	GetTextMetricsW        = g("GetTextMetricsW")
	GetDIBits              = g("GetDIBits")

	GetModuleHandleW = k("GetModuleHandleW")

	GdiplusStartup         = gp("GdiplusStartup")
	GdipCreateFromHDC      = gp("GdipCreateFromHDC")
	GdipDeleteGraphics     = gp("GdipDeleteGraphics")
	GdipSetSmoothingMode   = gp("GdipSetSmoothingMode")
	GdipSetPixelOffsetMode = gp("GdipSetPixelOffsetMode")
	GdipCreatePath         = gp("GdipCreatePath")
	GdipDeletePath         = gp("GdipDeletePath")
	GdipAddPathArc         = gp("GdipAddPathArc")
	GdipAddPathLine        = gp("GdipAddPathLine")
	GdipClosePathFigure    = gp("GdipClosePathFigure")
	GdipCreateSolidFill    = gp("GdipCreateSolidFill")
	GdipDeleteBrush        = gp("GdipDeleteBrush")
	GdipFillPath           = gp("GdipFillPath")
	GdipCreatePen1         = gp("GdipCreatePen1")
	GdipDeletePen          = gp("GdipDeletePen")
	GdipSetPenStartCap     = gp("GdipSetPenStartCap")
	GdipSetPenEndCap       = gp("GdipSetPenEndCap")
	GdipDrawPath           = gp("GdipDrawPath")
	GdipDrawArc            = gp("GdipDrawArc")
	GdipDrawLine           = gp("GdipDrawLine")
	GdipFillEllipse        = gp("GdipFillEllipse")
	GdipFlush              = gp("GdipFlush")

	InitCommonControlsEx   = Proc{comctl32.NewProc("InitCommonControlsEx")}
	DwmSetWindowAttribute  = Proc{dwmapi.NewProc("DwmSetWindowAttribute")}
	SetProcessDpiAwareness = Proc{shcore.NewProc("SetProcessDpiAwareness")}
)

// Window messages and constants.
const (
	WM_CREATE            = 0x0001
	WM_DESTROY           = 0x0002
	WM_SIZE              = 0x0005
	WM_SETFOCUS          = 0x0007
	WM_KILLFOCUS         = 0x0008
	WM_ENABLE            = 0x000A
	WM_SETTEXT           = 0x000C
	WM_GETTEXT           = 0x000D
	WM_PAINT             = 0x000F
	WM_CLOSE             = 0x0010
	WM_ERASEBKGND        = 0x0014
	WM_SETTINGCHANGE     = 0x001A
	WM_ACTIVATEAPP       = 0x001C
	WM_SETCURSOR         = 0x0020
	WM_GETMINMAXINFO     = 0x0024
	WM_DRAWITEM          = 0x002B
	WM_SETFONT           = 0x0030
	WM_NOTIFY            = 0x004E
	WM_COPYDATA          = 0x004A
	WM_PRINT             = 0x0317
	WM_GETDLGCODE        = 0x0087
	WM_KEYDOWN           = 0x0100
	WM_KEYUP             = 0x0101
	WM_CHAR              = 0x0102
	WM_SYSKEYDOWN        = 0x0104
	WM_SYSKEYUP          = 0x0105
	WM_SYSCHAR           = 0x0106
	WM_COMMAND           = 0x0111
	WM_SYSCOMMAND        = 0x0112
	WM_TIMER             = 0x0113
	WM_HSCROLL           = 0x0114
	WM_CTLCOLOREDIT      = 0x0133
	WM_CTLCOLORSTATIC    = 0x0138
	WM_CTLCOLORBTN       = 0x0135
	WM_MOUSEMOVE         = 0x0200
	WM_LBUTTONDOWN       = 0x0201
	WM_LBUTTONUP         = 0x0202
	WM_LBUTTONDBLCLK     = 0x0203
	WM_RBUTTONDOWN       = 0x0204
	WM_RBUTTONUP         = 0x0205
	WM_MBUTTONDOWN       = 0x0207
	WM_MBUTTONUP         = 0x0208
	WM_MOUSEWHEEL        = 0x020A
	WM_XBUTTONDOWN       = 0x020B
	WM_XBUTTONUP         = 0x020C
	WM_MOUSEHWHEEL       = 0x020E
	WM_POINTERUPDATE     = 0x0245
	WM_POINTERDOWN       = 0x0246
	WM_POINTERUP         = 0x0247
	WM_POINTERENTER      = 0x0249
	WM_POINTERLEAVE      = 0x024A
	WM_CAPTURECHANGED    = 0x0215
	WM_EXITSIZEMOVE      = 0x0232
	WM_DPICHANGED        = 0x02E0
	WM_MOUSELEAVE        = 0x02A3
	WM_PRINTCLIENT       = 0x0318
	WM_THEMECHANGED      = 0x031A
	WM_USER              = 0x0400
	WM_APP               = 0x8000
	DM_GETDEFID          = WM_USER + 0
	DC_HASDEFID          = 0x534B
	IDOK                 = 1
	IDCANCEL             = 2
	BN_CLICKED           = 0
	BN_SETFOCUS          = 6
	EN_SETFOCUS          = 0x0100
	EN_KILLFOCUS         = 0x0200
	EN_CHANGE            = 0x0300
	EM_SETSEL            = 0x00B1
	EM_SETMARGINS        = 0x00D3
	EM_SETLIMITTEXT      = 0x00C5
	EM_SETCUEBANNER      = 0x1501
	BM_CLICK             = 0x00F5
	TBM_GETPOS           = WM_USER
	TBM_SETPOS           = WM_USER + 5
	TBM_SETRANGE         = WM_USER + 6
	TBM_SETPAGESIZE      = WM_USER + 21
	TBM_SETLINESIZE      = WM_USER + 23
	ICC_BAR_CLASSES      = 0x4
	ICC_STANDARD_CLASSES = 0x4000

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_POPUP            = 0x80000000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_DISABLED         = 0x08000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_TABSTOP          = 0x00010000
	WS_GROUP            = 0x00020000
	WS_EX_CONTROLPARENT = 0x00010000
	BS_OWNERDRAW        = 0x0000000B
	BS_NOTIFY           = 0x00004000
	ES_AUTOHSCROLL      = 0x0080
	ES_PASSWORD         = 0x0020
	ES_NUMBER           = 0x2000
	TBS_HORZ            = 0x0000
	TBS_NOTICKS         = 0x0010
	TBS_BOTH            = 0x0008

	// GetWindowLongPtr indexes are negative; these are their uintptr forms.
	GWL_STYLE      = ^uintptr(15) // -16
	GWLP_WNDPROC   = ^uintptr(3)  // -4
	SW_SHOW        = 5
	SW_HIDE        = 0
	SW_SHOWDEFAULT = 10

	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOZORDER      = 0x0004
	SWP_NOACTIVATE    = 0x0010
	SWP_FRAMECHANGED  = 0x0020
	SWP_NOOWNERZORDER = 0x0200
	HWND_TOP          = 0

	CS_HREDRAW     = 0x0002
	CS_VREDRAW     = 0x0001
	CS_DBLCLKS     = 0x0008
	IDC_ARROW      = 32512
	IDC_HAND       = 32649
	IDC_IBEAM      = 32513
	IMAGE_ICON     = 1
	LR_DEFAULTSIZE = 0x40
	LR_SHARED      = 0x8000
	HTCLIENT       = 1

	ODS_SELECTED    = 0x0001
	ODS_DISABLED    = 0x0004
	ODS_FOCUS       = 0x0010
	ODS_NOFOCUSRECT = 0x0200

	MONITOR_DEFAULTTONEAREST = 2
	WH_KEYBOARD_LL           = 13
	HC_ACTION                = 0
	TME_LEAVE                = 0x2
	PT_PEN                   = 3
	POINTER_FLAG_INRANGE     = 0x2
	POINTER_FLAG_INCONTACT   = 0x4
	PEN_FLAG_BARREL          = 0x1
	PEN_FLAG_INVERTED        = 0x2
	PEN_FLAG_ERASER          = 0x4
	PEN_MASK_PRESSURE        = 0x1
	PEN_MASK_TILT_X          = 0x4
	PEN_MASK_TILT_Y          = 0x8

	DT_LEFT         = 0x0
	DT_CENTER       = 0x1
	DT_RIGHT        = 0x2
	DT_VCENTER      = 0x4
	DT_WORDBREAK    = 0x10
	DT_SINGLELINE   = 0x20
	DT_NOPREFIX     = 0x800
	DT_CALCRECT     = 0x400
	DT_END_ELLIPSIS = 0x8000

	TRANSPARENT       = 1
	HALFTONE          = 4
	SRCCOPY           = 0x00CC0020
	DIB_RGB_COLORS    = 0
	FW_NORMAL         = 400
	FW_SEMIBOLD       = 600
	FW_BOLD           = 700
	CLEARTYPE_QUALITY = 5
	DEFAULT_CHARSET   = 1

	VK_BACK    = 0x08
	VK_TAB     = 0x09
	VK_RETURN  = 0x0D
	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_MENU    = 0x12
	VK_ESCAPE  = 0x1B
	VK_LWIN    = 0x5B
	VK_RWIN    = 0x5C
	VK_F4      = 0x73
	VK_F10     = 0x79
	KF_REPEAT  = 0x4000
	LLKHF_UP   = 0x80

	MK_LBUTTON  = 0x1
	MK_RBUTTON  = 0x2
	MK_MBUTTON  = 0x10
	MK_XBUTTON1 = 0x20
	MK_XBUTTON2 = 0x40
	XBUTTON1    = 1

	SC_KEYMENU           = 0xF100
	SM_CXSCREEN          = 0
	SM_CYSCREEN          = 1
	PW_CLIENTONLY        = 1
	PW_RENDERFULLCONTENT = 2
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
const DpiPerMonitorV2 = ^uintptr(3) // (HANDLE)-4

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

func (r RECT) W() int32 { return r.Right - r.Left }
func (r RECT) H() int32 { return r.Bottom - r.Top }

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type PAINTSTRUCT struct {
	Hdc       uintptr
	Erase     int32
	Paint     RECT
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

type DRAWITEMSTRUCT struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	Hdc        uintptr
	Item       RECT
	ItemData   uintptr
}

type MONITORINFO struct {
	Size    uint32
	Monitor RECT
	Work    RECT
	Flags   uint32
}

type WINDOWPLACEMENT struct {
	Length    uint32
	Flags     uint32
	ShowCmd   uint32
	MinPos    POINT
	MaxPos    POINT
	NormalPos RECT
}

type MINMAXINFO struct {
	Reserved     POINT
	MaxSize      POINT
	MaxPosition  POINT
	MinTrackSize POINT
	MaxTrackSize POINT
}

type KBDLLHOOKSTRUCT struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type TRACKMOUSEEVENT struct {
	Size      uint32
	Flags     uint32
	Hwnd      uintptr
	HoverTime uint32
}

type SIZE struct{ CX, CY int32 }

type TEXTMETRIC struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading, AveCharWidth, MaxCharWidth, Weight, Overhang, DigitizedAspectX, DigitizedAspectY int32
	FirstChar, LastChar, DefaultChar, BreakChar                                                                                                 uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet                                                                                      byte
}

type POINTER_INFO struct {
	PointerType           uint32
	PointerID             uint32
	FrameID               uint32
	PointerFlags          uint32
	SourceDevice          uintptr
	HwndTarget            uintptr
	PtPixelLocation       POINT
	PtHimetricLocation    POINT
	PtPixelLocationRaw    POINT
	PtHimetricLocationRaw POINT
	Time                  uint32
	HistoryCount          uint32
	InputData             int32
	KeyStates             uint32
	PerformanceCount      uint64
	ButtonChangeType      int32
}

type POINTER_PEN_INFO struct {
	PointerInfo POINTER_INFO
	PenFlags    uint32
	PenMask     uint32
	Pressure    uint32
	Rotation    uint32
	TiltX       int32
	TiltY       int32
}

type INITCOMMONCONTROLSEX struct{ Size, ICC uint32 }

type COPYDATASTRUCT struct {
	Data uintptr
	Size uint32
	Ptr  uintptr
}

type GdiplusStartupInput struct {
	Version                  uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

// Str returns a NUL-terminated UTF-16 pointer for s.
func Str(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

// S is Str as a uintptr argument.
func S(s string) uintptr { return uintptr(unsafe.Pointer(Str(s))) }

// P turns a pointer into a call argument.
func P[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

func LOWORD(v uintptr) uint16 { return uint16(v) }
func HIWORD(v uintptr) uint16 { return uint16(v >> 16) }

// XY splits a mouse lParam into signed client coordinates.
func XY(l uintptr) (int32, int32) { return int32(int16(l)), int32(int16(l >> 16)) }

// Text reads a window's text.
func Text(h uintptr) string {
	n := GetWindowTextLengthW.Call(h)
	buf := make([]uint16, n+1)
	GetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

// ClassName reads a window's class name.
func ClassName(h uintptr) string {
	buf := make([]uint16, 128)
	GetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 128)
	return syscall.UTF16ToString(buf)
}

// ClientRect returns a window's client rectangle.
func ClientRect(h uintptr) RECT {
	var r RECT
	GetClientRect.Call(h, P(&r))
	return r
}

// WindowRect returns a window's screen rectangle.
func WindowRect(h uintptr) RECT {
	var r RECT
	GetWindowRect.Call(h, P(&r))
	return r
}

// Invalidate repaints a whole window.
func Invalidate(h uintptr) { InvalidateRect.Call(h, 0, 0) }

// InvalidateR repaints part of a window.
func InvalidateR(h uintptr, r RECT) { InvalidateRect.Call(h, P(&r), 0) }

// RGB makes a GDI COLORREF from 0xRRGGBB.
func RGB(c uint32) uintptr { return uintptr((c>>16)&0xff | (c & 0xff00) | (c&0xff)<<16) }
