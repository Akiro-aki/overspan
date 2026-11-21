package input

// KeyCode maps a browser KeyboardEvent.code to a Linux evdev key code. Using
// physical codes (not characters) lets the desktop's own keyboard layout
// decide what each key types, exactly like a real keyboard.
func KeyCode(code string) int { return keymap[code] }

var keymap = map[string]int{
	"Escape": 1, "Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5, "Digit5": 6,
	"Digit6": 7, "Digit7": 8, "Digit8": 9, "Digit9": 10, "Digit0": 11, "Minus": 12,
	"Equal": 13, "Backspace": 14, "Tab": 15, "KeyQ": 16, "KeyW": 17, "KeyE": 18,
	"KeyR": 19, "KeyT": 20, "KeyY": 21, "KeyU": 22, "KeyI": 23, "KeyO": 24,
	"KeyP": 25, "BracketLeft": 26, "BracketRight": 27, "Enter": 28, "ControlLeft": 29,
	"KeyA": 30, "KeyS": 31, "KeyD": 32, "KeyF": 33, "KeyG": 34, "KeyH": 35,
	"KeyJ": 36, "KeyK": 37, "KeyL": 38, "Semicolon": 39, "Quote": 40, "Backquote": 41,
	"ShiftLeft": 42, "Backslash": 43, "KeyZ": 44, "KeyX": 45, "KeyC": 46, "KeyV": 47,
	"KeyB": 48, "KeyN": 49, "KeyM": 50, "Comma": 51, "Period": 52, "Slash": 53,
	"ShiftRight": 54, "NumpadMultiply": 55, "AltLeft": 56, "Space": 57, "CapsLock": 58,
	"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66,
	"F9": 67, "F10": 68, "NumLock": 69, "ScrollLock": 70, "Numpad7": 71, "Numpad8": 72,
	"Numpad9": 73, "NumpadSubtract": 74, "Numpad4": 75, "Numpad5": 76, "Numpad6": 77,
	"NumpadAdd": 78, "Numpad1": 79, "Numpad2": 80, "Numpad3": 81, "Numpad0": 82,
	"NumpadDecimal": 83, "IntlBackslash": 86, "F11": 87, "F12": 88, "IntlRo": 89,
	"NumpadEnter": 96, "ControlRight": 97, "NumpadDivide": 98, "PrintScreen": 99,
	"AltRight": 100, "Home": 102, "ArrowUp": 103, "PageUp": 104, "ArrowLeft": 105,
	"ArrowRight": 106, "End": 107, "ArrowDown": 108, "PageDown": 109, "Insert": 110,
	"Delete": 111, "AudioVolumeMute": 113, "AudioVolumeDown": 114, "AudioVolumeUp": 115,
	"Power": 116, "NumpadEqual": 117, "Pause": 119, "NumpadComma": 121, "IntlYen": 124,
	"MetaLeft": 125, "MetaRight": 126, "OSLeft": 125, "OSRight": 126, "ContextMenu": 127,
	"MediaTrackNext": 163, "MediaPlayPause": 164, "MediaTrackPrevious": 165,
	"F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188,
	"F19": 189, "F20": 190, "F21": 191, "F22": 192, "F23": 193, "F24": 194,
}

// ShiftedChars maps characters that a soft keyboard sends without a physical
// code to (code, needsShift) on a US layout. Used for on-screen keyboards on
// phones, which report KeyboardEvent.key but an empty or bogus code.
func CharKey(ch rune) (code int, shift bool) {
	switch {
	case ch >= 'a' && ch <= 'z':
		return keymap["Key"+string(ch-32)], false
	case ch >= 'A' && ch <= 'Z':
		return keymap["Key"+string(ch)], true
	case ch >= '1' && ch <= '9':
		return keymap["Digit"+string(ch)], false
	case ch == '0':
		return keymap["Digit0"], false
	}
	if c, ok := punct[ch]; ok {
		return c.code, c.shift
	}
	return 0, false
}

type pk struct {
	code  int
	shift bool
}

var punct = map[rune]pk{
	' ': {57, false}, '\n': {28, false}, '\t': {15, false},
	'-': {12, false}, '_': {12, true}, '=': {13, false}, '+': {13, true},
	'[': {26, false}, '{': {26, true}, ']': {27, false}, '}': {27, true},
	';': {39, false}, ':': {39, true}, '\'': {40, false}, '"': {40, true},
	'`': {41, false}, '~': {41, true}, '\\': {43, false}, '|': {43, true},
	',': {51, false}, '<': {51, true}, '.': {52, false}, '>': {52, true},
	'/': {53, false}, '?': {53, true},
	'!': {2, true}, '@': {3, true}, '#': {4, true}, '$': {5, true}, '%': {6, true},
	'^': {7, true}, '&': {8, true}, '*': {9, true}, '(': {10, true}, ')': {11, true},
}
