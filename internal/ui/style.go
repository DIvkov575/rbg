package ui

import (
	"fmt"
	"strings"
)

// Styled toggles ANSI styling for the whole package. The Run loop enables it for
// a real terminal; tests leave it off so they assert on plain text. Keeping all
// escape codes behind this one switch is also the seam a future Lip Gloss /
// Bubble Tea migration replaces — swap these helpers for lipgloss.Style and the
// renderers are unchanged.
var Styled bool

// ANSI SGR codes. 256-color foregrounds are used for state/sync badges so the
// palette is stable across terminals.
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiReverse = "\x1b[7m"
)

// sgr wraps s in an SGR code + reset when styling is on, else returns s plain.
func sgr(code, s string) string {
	if !Styled {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string    { return sgr(ansiBold, s) }
func dim(s string) string     { return sgr(ansiDim, s) }
func reverse(s string) string { return sgr(ansiReverse, s) }

// fg colours s with a 256-color foreground (no-op when styling is off).
func fg(n int, s string) string {
	if !Styled {
		return s
	}
	return fmt.Sprintf("\x1b[38;5;%dm%s%s", n, s, ansiReset)
}

// Palette (256-color): kept muted so the list reads as data, not a rainbow.
const (
	colGreen  = 71  // running / aligned
	colYellow = 179 // held / ahead
	colRed    = 167 // dirty / behind
	colGray   = 244 // done / done-ish
	colBlue   = 74  // headers / accents
)

// truncPad forces the PLAIN text s to exactly w display cells: truncate with a
// trailing … when too long, pad with spaces when too short. Callers must apply
// colour AFTER this (colour codes have zero display width, so padding the
// coloured string would misalign columns). w<=0 yields "".
func truncPad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		if w == 1 {
			return "…"
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// pad right-pads s to w cells without truncating (for the last column).
func pad(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
