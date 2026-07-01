// Package ui is rbg's pure dashboard presentation layer: a Screen interface and
// a screen stack over a Model of the reconciled agent inventory, with the four
// lens views. Everything here is a pure function of (Model, Key) — no I/O. The
// engine wiring and the raw-terminal loop live in a separate layer.
package ui

// Key is a decoded input event. Screens interpret the same Key by context
// (there are no per-mode decoders); printable input arrives as KeyRune + a rune.
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyEsc
	KeyCycleView // ctrl-s (0x13): cycle the list view
	KeyRefresh   // r
	KeyRune      // a printable rune (carried alongside)
	KeyBackspace
	KeyQuit // q or ctrl-c
)

// DecodeKey maps one raw input chunk to a Key (and, for KeyRune, the rune). A
// nil/empty chunk is KeyNone. This is the single decoder; screens decide meaning.
func DecodeKey(b []byte) (Key, rune) {
	if len(b) == 0 {
		return KeyNone, 0
	}
	// arrow escape sequences: ESC [ A/B
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp, 0
		case 'B':
			return KeyDown, 0
		}
		return KeyNone, 0
	}
	if len(b) == 1 {
		switch b[0] {
		case '\r', '\n':
			return KeyEnter, 0
		case 0x1b:
			return KeyEsc, 0
		case 0x13: // ctrl-s
			return KeyCycleView, 0
		case 0x03: // ctrl-c
			return KeyQuit, 0
		case 0x7f, 0x08: // DEL / BS
			return KeyBackspace, 0
		}
	}
	// single printable byte → rune (used for text input and letter shortcuts)
	if len(b) == 1 && b[0] >= 0x20 && b[0] < 0x7f {
		return KeyRune, rune(b[0])
	}
	return KeyNone, 0
}
