package input

import "testing"

func TestKeys(t *testing.T) {
	for code, want := range map[string]int{"KeyA": 30, "Enter": 28, "ArrowLeft": 105, "MetaLeft": 125, "F12": 88} {
		if got := KeyCode(code); got != want {
			t.Errorf("%s = %d, want %d", code, got, want)
		}
	}
	if c, s := CharKey('A'); c != 30 || !s {
		t.Error("A should be shift+KEY_A")
	}
	if c, s := CharKey('?'); c != 53 || !s {
		t.Error("? should be shift+slash")
	}
}
