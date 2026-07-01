package ui

import "testing"

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Key
	}{
		{"up arrow", []byte{0x1b, '[', 'A'}, KeyUp},
		{"down arrow", []byte{0x1b, '[', 'B'}, KeyDown},
		{"enter", []byte{'\r'}, KeyEnter},
		{"enter-nl", []byte{'\n'}, KeyEnter},
		{"esc", []byte{0x1b}, KeyEsc},
		{"ctrl-s cycles view", []byte{0x13}, KeyCycleView},
		{"ctrl-c quits", []byte{0x03}, KeyQuit},
		{"empty", []byte{}, KeyNone},
	}
	for _, c := range cases {
		got, _ := DecodeKey(c.in)
		if got != c.want {
			t.Errorf("%s: DecodeKey(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestDecodeKeyRune(t *testing.T) {
	// A printable byte returns KeyRune and the rune itself (for text input).
	k, r := DecodeKey([]byte{'x'})
	if k != KeyRune || r != 'x' {
		t.Errorf("DecodeKey('x') = (%v,%q), want (KeyRune,'x')", k, r)
	}
	// Backspace (DEL 0x7f or BS 0x08) maps to KeyBackspace.
	if k, _ := DecodeKey([]byte{0x7f}); k != KeyBackspace {
		t.Errorf("0x7f should be KeyBackspace, got %v", k)
	}
}
