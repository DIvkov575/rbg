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
	case 'k':
		return KeyUp
	case 'j':
		return KeyDown
	case '\r', '\n', 'v':
		return KeyView
	case 'a':
		return KeyAttach
	case 'r':
		return KeyRefresh
	case 'q', 0x03: // q or Ctrl-C
		return KeyQuit
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
