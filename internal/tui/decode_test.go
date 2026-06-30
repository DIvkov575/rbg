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
		{[]byte("C"), KeyConfig},
		{[]byte("Q"), KeyQueue},
	}
	for _, c := range cases {
		if got := decodeKey(c.in); got != c.want {
			t.Errorf("decodeKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeKeySave(t *testing.T) {
	if decodeKey([]byte("s")) != KeySave {
		t.Error("'s' should decode to KeySave in normal mode")
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

func TestDecodeKeyBrowse(t *testing.T) {
	cases := []struct {
		in   []byte
		want Key
	}{
		{[]byte("\x1b[A"), KeyUp},
		{[]byte("\x1b[B"), KeyDown},
		{[]byte("k"), KeyUp},
		{[]byte("j"), KeyDown},
		{[]byte("\r"), KeyEnter},
		{[]byte("h"), KeyParent},
		{[]byte("c"), KeyChoose},
		{[]byte("m"), KeyMkdir},
		{[]byte("\x1b"), KeyEsc},
		{[]byte("\x03"), KeyEsc},
		{[]byte("z"), KeyNone},
	}
	for _, c := range cases {
		if got := decodeKeyBrowse(c.in); got != c.want {
			t.Errorf("decodeKeyBrowse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeQueueKeys(t *testing.T) {
	if decodeKey([]byte("Q")) != KeyQueue {
		t.Error("'Q' → KeyQueue")
	}
}

func TestDecodeKeyQueue(t *testing.T) {
	checks := map[byte]Key{'a': KeyQueueAdd, 'd': KeyDispatch, 'x': KeyRemove, 'j': KeyDown, 'k': KeyUp}
	for b, want := range checks {
		if got := decodeKeyQueue([]byte{b}); got != want {
			t.Errorf("decodeKeyQueue(%q) = %v, want %v", string(b), got, want)
		}
	}
	if decodeKeyQueue([]byte{0x1b}) != KeyEsc {
		t.Error("esc → KeyEsc")
	}
}
