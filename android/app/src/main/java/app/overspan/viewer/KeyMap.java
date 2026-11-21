package app.overspan.viewer;

import android.util.SparseArray;
import android.view.KeyEvent;

/**
 * Maps Android key codes to the physical key names the server understands
 * (the same names browsers use for KeyboardEvent.code).
 */
final class KeyMap {
    private static final SparseArray<String> MAP = new SparseArray<>();

    static {
        for (int i = 0; i < 26; i++) MAP.put(KeyEvent.KEYCODE_A + i, "Key" + (char) ('A' + i));
        for (int i = 0; i < 10; i++) {
            MAP.put(KeyEvent.KEYCODE_0 + i, "Digit" + i);
            MAP.put(KeyEvent.KEYCODE_NUMPAD_0 + i, "Numpad" + i);
        }
        for (int i = 0; i < 12; i++) MAP.put(KeyEvent.KEYCODE_F1 + i, "F" + (i + 1));
        put(KeyEvent.KEYCODE_ENTER, "Enter");
        put(KeyEvent.KEYCODE_DEL, "Backspace");
        put(KeyEvent.KEYCODE_FORWARD_DEL, "Delete");
        put(KeyEvent.KEYCODE_TAB, "Tab");
        put(KeyEvent.KEYCODE_SPACE, "Space");
        put(KeyEvent.KEYCODE_ESCAPE, "Escape");
        put(KeyEvent.KEYCODE_DPAD_UP, "ArrowUp");
        put(KeyEvent.KEYCODE_DPAD_DOWN, "ArrowDown");
        put(KeyEvent.KEYCODE_DPAD_LEFT, "ArrowLeft");
        put(KeyEvent.KEYCODE_DPAD_RIGHT, "ArrowRight");
        put(KeyEvent.KEYCODE_SHIFT_LEFT, "ShiftLeft");
        put(KeyEvent.KEYCODE_SHIFT_RIGHT, "ShiftRight");
        put(KeyEvent.KEYCODE_CTRL_LEFT, "ControlLeft");
        put(KeyEvent.KEYCODE_CTRL_RIGHT, "ControlRight");
        put(KeyEvent.KEYCODE_ALT_LEFT, "AltLeft");
        put(KeyEvent.KEYCODE_ALT_RIGHT, "AltRight");
        put(KeyEvent.KEYCODE_META_LEFT, "MetaLeft");
        put(KeyEvent.KEYCODE_META_RIGHT, "MetaRight");
        put(KeyEvent.KEYCODE_MINUS, "Minus");
        put(KeyEvent.KEYCODE_EQUALS, "Equal");
        put(KeyEvent.KEYCODE_LEFT_BRACKET, "BracketLeft");
        put(KeyEvent.KEYCODE_RIGHT_BRACKET, "BracketRight");
        put(KeyEvent.KEYCODE_BACKSLASH, "Backslash");
        put(KeyEvent.KEYCODE_SEMICOLON, "Semicolon");
        put(KeyEvent.KEYCODE_APOSTROPHE, "Quote");
        put(KeyEvent.KEYCODE_GRAVE, "Backquote");
        put(KeyEvent.KEYCODE_COMMA, "Comma");
        put(KeyEvent.KEYCODE_PERIOD, "Period");
        put(KeyEvent.KEYCODE_SLASH, "Slash");
        put(KeyEvent.KEYCODE_MOVE_HOME, "Home");
        put(KeyEvent.KEYCODE_MOVE_END, "End");
        put(KeyEvent.KEYCODE_PAGE_UP, "PageUp");
        put(KeyEvent.KEYCODE_PAGE_DOWN, "PageDown");
        put(KeyEvent.KEYCODE_INSERT, "Insert");
        put(KeyEvent.KEYCODE_CAPS_LOCK, "CapsLock");
        put(KeyEvent.KEYCODE_NUM_LOCK, "NumLock");
        put(KeyEvent.KEYCODE_SCROLL_LOCK, "ScrollLock");
        put(KeyEvent.KEYCODE_SYSRQ, "PrintScreen");
        put(KeyEvent.KEYCODE_BREAK, "Pause");
        put(KeyEvent.KEYCODE_MENU, "ContextMenu");
        put(KeyEvent.KEYCODE_NUMPAD_ADD, "NumpadAdd");
        put(KeyEvent.KEYCODE_NUMPAD_SUBTRACT, "NumpadSubtract");
        put(KeyEvent.KEYCODE_NUMPAD_MULTIPLY, "NumpadMultiply");
        put(KeyEvent.KEYCODE_NUMPAD_DIVIDE, "NumpadDivide");
        put(KeyEvent.KEYCODE_NUMPAD_DOT, "NumpadDecimal");
        put(KeyEvent.KEYCODE_NUMPAD_ENTER, "NumpadEnter");
        put(KeyEvent.KEYCODE_VOLUME_UP, "AudioVolumeUp");
        put(KeyEvent.KEYCODE_VOLUME_DOWN, "AudioVolumeDown");
        put(KeyEvent.KEYCODE_VOLUME_MUTE, "AudioVolumeMute");
        put(KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, "MediaPlayPause");
        put(KeyEvent.KEYCODE_MEDIA_NEXT, "MediaTrackNext");
        put(KeyEvent.KEYCODE_MEDIA_PREVIOUS, "MediaTrackPrevious");
    }

    private static void put(int k, String v) { MAP.put(k, v); }

    static String code(int keyCode) { return MAP.get(keyCode); }

    private KeyMap() { }
}
