package tui

import (
	"fmt"
	"io"
	"os"
)

// decodeKey maps a raw input chunk to an abstract Key. Arrow keys arrive as the
// 3-byte escape sequences ESC [ A/B; we also accept vi-style and letter keys.
//
// Assumption: in raw mode (VMIN=1, VTIME=0) the terminal delivers a full arrow
// escape in a single read, so b holds all 3 bytes at once. A bare ESC (len 1,
// just 0x1b) is not a recognized binding and falls through to KeyNone below.
func decodeKey(b []byte) Key {
	if len(b) == 0 {
		return KeyNone
	}
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp
		case 'B':
			return KeyDown
		}
		return KeyNone
	}
	switch b[0] {
	case 'j':
		return KeyDown
	case 'n':
		return KeyNew
	case 'k':
		return KeyKill
	case 'a':
		return KeyAttach
	case 'r':
		return KeyRefresh
	case 'C':
		return KeyConfig
	case 's':
		return KeySave
	case 'q', 0x03: // q or Ctrl-C
		return KeyQuit
	}
	return KeyNone
}

// decodeKeyInput decodes a raw chunk while the dashboard is in task-input mode.
// It returns (key, rune, isRune): control keys map to KeyEnter/KeyEsc/KeyNone;
// printable bytes return as a rune for InputRune. Backspace maps to KeyNone with
// a sentinel handled by the loop via isBackspace.
func decodeKeyInput(b []byte) (Key, rune, bool) {
	if len(b) == 0 {
		return KeyNone, 0, false
	}
	switch b[0] {
	case '\r', '\n':
		return KeyEnter, 0, false
	case 0x1b: // ESC
		return KeyEsc, 0, false
	case 0x7f, 0x08: // DEL / Backspace
		return KeyBackspace, 0, false
	case 0x03: // Ctrl-C cancels input
		return KeyEsc, 0, false
	}
	if b[0] >= 0x20 && b[0] < 0x7f { // printable ASCII
		return KeyNone, rune(b[0]), true
	}
	return KeyNone, 0, false
}

// decodeKeyBrowse decodes a raw chunk while the dashboard is in the directory-
// browser mode: arrows / j / k navigate, Enter descends, 'h' goes to the parent,
// 'c' chooses the current dir, and ESC / Ctrl-C cancel the flow.
func decodeKeyBrowse(b []byte) Key {
	if len(b) == 0 {
		return KeyNone
	}
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp
		case 'B':
			return KeyDown
		}
		return KeyNone
	}
	switch b[0] {
	case 'k':
		return KeyUp
	case 'j':
		return KeyDown
	case '\r', '\n':
		return KeyEnter
	case 'h':
		return KeyParent
	case 'c':
		return KeyChoose
	case 'm':
		return KeyMkdir
	case 0x1b, 0x03: // ESC or Ctrl-C
		return KeyEsc
	}
	return KeyNone
}

// decodeKeyConfig decodes keys while the config field list (not edit-mode) is
// showing: arrows / j / k navigate, Enter begins editing the selected field,
// 's' saves, and ESC / Ctrl-C close the screen.
func decodeKeyConfig(b []byte) Key {
	if len(b) == 0 {
		return KeyNone
	}
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp
		case 'B':
			return KeyDown
		}
		return KeyNone
	}
	switch b[0] {
	case 'k':
		return KeyUp
	case 'j':
		return KeyDown
	case '\r', '\n':
		return KeyEnter
	case 's':
		return KeySave
	case 0x1b, 0x03: // ESC or Ctrl-C
		return KeyEsc
	}
	return KeyNone
}

const clearScreen = "\x1b[2J\x1b[H" // clear + cursor home

// draw renders the model to w, clearing first.
func draw(w io.Writer, m Model) {
	fmt.Fprint(w, clearScreen)
	fmt.Fprint(w, View(m))
}

// readKey reads one key event from r (a raw-mode fd). Returns KeyNone on EOF.
// readRaw reads one input chunk; nil/empty means EOF (treated as quit).
func readRaw(r io.Reader) []byte {
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}

func readKey(r io.Reader) Key {
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n == 0 {
		return KeyQuit // treat read failure/EOF as quit
	}
	return decodeKey(buf[:n])
}

// Stdio bundles the loop's I/O endpoints (injectable for clarity/testing).
type Stdio struct {
	In  io.Reader
	Out io.Writer
}

// DefaultStdio uses the process terminal.
func DefaultStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout} }
