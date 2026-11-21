package client

// Windows reports keys by set-1 scan code plus an "extended" flag. Scan codes
// name physical keys, so the computer applies its own keyboard layout, the
// same as the browser viewer's KeyboardEvent.code.

var scanPlain = map[uint32]string{
	0x01: "Escape", 0x02: "Digit1", 0x03: "Digit2", 0x04: "Digit3", 0x05: "Digit4",
	0x06: "Digit5", 0x07: "Digit6", 0x08: "Digit7", 0x09: "Digit8", 0x0A: "Digit9",
	0x0B: "Digit0", 0x0C: "Minus", 0x0D: "Equal", 0x0E: "Backspace", 0x0F: "Tab",
	0x10: "KeyQ", 0x11: "KeyW", 0x12: "KeyE", 0x13: "KeyR", 0x14: "KeyT", 0x15: "KeyY",
	0x16: "KeyU", 0x17: "KeyI", 0x18: "KeyO", 0x19: "KeyP", 0x1A: "BracketLeft",
	0x1B: "BracketRight", 0x1C: "Enter", 0x1D: "ControlLeft", 0x1E: "KeyA", 0x1F: "KeyS",
	0x20: "KeyD", 0x21: "KeyF", 0x22: "KeyG", 0x23: "KeyH", 0x24: "KeyJ", 0x25: "KeyK",
	0x26: "KeyL", 0x27: "Semicolon", 0x28: "Quote", 0x29: "Backquote", 0x2A: "ShiftLeft",
	0x2B: "Backslash", 0x2C: "KeyZ", 0x2D: "KeyX", 0x2E: "KeyC", 0x2F: "KeyV", 0x30: "KeyB",
	0x31: "KeyN", 0x32: "KeyM", 0x33: "Comma", 0x34: "Period", 0x35: "Slash",
	0x36: "ShiftRight", 0x37: "NumpadMultiply", 0x38: "AltLeft", 0x39: "Space",
	0x3A: "CapsLock", 0x3B: "F1", 0x3C: "F2", 0x3D: "F3", 0x3E: "F4", 0x3F: "F5",
	0x40: "F6", 0x41: "F7", 0x42: "F8", 0x43: "F9", 0x44: "F10", 0x46: "ScrollLock",
	0x47: "Numpad7", 0x48: "Numpad8", 0x49: "Numpad9", 0x4A: "NumpadSubtract",
	0x4B: "Numpad4", 0x4C: "Numpad5", 0x4D: "Numpad6", 0x4E: "NumpadAdd", 0x4F: "Numpad1",
	0x50: "Numpad2", 0x51: "Numpad3", 0x52: "Numpad0", 0x53: "NumpadDecimal",
	0x56: "IntlBackslash", 0x57: "F11", 0x58: "F12", 0x59: "NumpadEqual",
	0x64: "F13", 0x65: "F14", 0x66: "F15", 0x67: "F16", 0x68: "F17", 0x69: "F18",
	0x6A: "F19", 0x6B: "F20", 0x6C: "F21", 0x6D: "F22", 0x6E: "F23", 0x76: "F24",
	0x73: "IntlRo", 0x7D: "IntlYen", 0x7E: "NumpadComma",
}

var scanExt = map[uint32]string{
	0x1C: "NumpadEnter", 0x1D: "ControlRight", 0x35: "NumpadDivide", 0x37: "PrintScreen",
	0x38: "AltRight", 0x45: "NumLock", 0x47: "Home", 0x48: "ArrowUp", 0x49: "PageUp",
	0x4B: "ArrowLeft", 0x4D: "ArrowRight", 0x4F: "End", 0x50: "ArrowDown",
	0x51: "PageDown", 0x52: "Insert", 0x53: "Delete", 0x5B: "MetaLeft", 0x5C: "MetaRight",
	0x5D: "ContextMenu", 0x20: "AudioVolumeMute", 0x2E: "AudioVolumeDown",
	0x30: "AudioVolumeUp", 0x19: "MediaTrackNext", 0x10: "MediaTrackPrevious",
	0x22: "MediaPlayPause",
}

// Virtual-key codes whose scan codes are ambiguous or missing.
var vkSpecial = map[uint32]string{
	0x13: "Pause", 0x2C: "PrintScreen", 0x90: "NumLock",
	0xAD: "AudioVolumeMute", 0xAE: "AudioVolumeDown", 0xAF: "AudioVolumeUp",
	0xB0: "MediaTrackNext", 0xB1: "MediaTrackPrevious", 0xB3: "MediaPlayPause",
	0x5B: "MetaLeft", 0x5C: "MetaRight",
}

// KeyCode names the physical key of a WM_KEYDOWN/WM_KEYUP message. It
// returns "" for keys the computer has no use for.
func KeyCode(vk uint32, lParam uintptr) string {
	if c, ok := vkSpecial[vk]; ok {
		return c
	}
	scan := uint32(lParam>>16) & 0xff
	if lParam&(1<<24) != 0 {
		return scanExt[scan]
	}
	return scanPlain[scan]
}
