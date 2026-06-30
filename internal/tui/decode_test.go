package tui

import "testing"

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   []byte
		want Key
	}{
		{[]byte("\x1b[A"), KeyUp},
		{[]byte("\x1b[B"), KeyDown},
		{[]byte("k"), KeyKill},
		{[]byte("j"), KeyDown},
		{[]byte("\r"), KeyNone}, // bare CR in normal mode is no longer a binding
		{[]byte("v"), KeyNone},  // v no longer maps to anything
		{[]byte("n"), KeyNew},
		{[]byte("a"), KeyAttach},
		{[]byte("r"), KeyRefresh},
		{[]byte("q"), KeyQuit},
		{[]byte("\x03"), KeyQuit}, // Ctrl-C
		{[]byte("\x1b"), KeyNone}, // bare ESC (len 1) is not a binding
		{[]byte("x"), KeyNone},
	}
	for _, c := range cases {
		if got := decodeKey(c.in); got != c.want {
			t.Errorf("decodeKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeKeyInput(t *testing.T) {
	if k, _, _ := decodeKeyInput([]byte("\r")); k != KeyEnter {
		t.Error("CR should be KeyEnter in input mode")
	}
	if k, _, _ := decodeKeyInput([]byte("\x1b")); k != KeyEsc {
		t.Error("ESC should be KeyEsc in input mode")
	}
	if k, _, _ := decodeKeyInput([]byte("\x7f")); k != KeyBackspace {
		t.Error("DEL should be KeyBackspace")
	}
	if k, r, isR := decodeKeyInput([]byte("z")); !isR || r != 'z' || k != KeyNone {
		t.Errorf("printable should return rune: k=%v r=%q isR=%v", k, r, isR)
	}
}
