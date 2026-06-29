package tui

import "testing"

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   []byte
		want Key
	}{
		{[]byte("\x1b[A"), KeyUp},
		{[]byte("\x1b[B"), KeyDown},
		{[]byte("k"), KeyUp},
		{[]byte("j"), KeyDown},
		{[]byte("\r"), KeyView},
		{[]byte("v"), KeyView},
		{[]byte("a"), KeyAttach},
		{[]byte("r"), KeyRefresh},
		{[]byte("q"), KeyQuit},
		{[]byte("\x03"), KeyQuit}, // Ctrl-C
		{[]byte("x"), KeyNone},
	}
	for _, c := range cases {
		if got := decodeKey(c.in); got != c.want {
			t.Errorf("decodeKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
