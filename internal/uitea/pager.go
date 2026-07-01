package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/render"
)

// pagerModel is a read-only, scrollable transcript view.
type pagerModel struct {
	title  string
	lines  []string
	offset int
}

// newPager renders raw claude transcript JSONL into readable conversation lines
// ("role: text") via render.Line, rather than dumping the raw JSON. Each
// rendered turn may span multiple display lines (its own text is split on \n),
// so long turns still scroll naturally. If nothing renders (e.g. an empty or
// tool-only transcript) it falls back to a placeholder rather than a blank pane.
func newPager(title string, data []byte) pagerModel {
	norm := strings.ReplaceAll(string(data), "\r\n", "\n")
	var lines []string
	for _, raw := range strings.Split(norm, "\n") {
		if out, ok := render.Line(raw); ok {
			lines = append(lines, strings.Split(out, "\n")...)
		}
	}
	if len(lines) == 0 {
		lines = []string{"(no conversation content yet)"}
	}
	return pagerModel{title: title, lines: lines}
}

// window is how many lines fit below the title/hints chrome.
func (p pagerModel) window(h int) int {
	n := h - 4
	if n < 1 {
		n = 1
	}
	return n
}

func (p pagerModel) maxOffset(h int) int {
	max := len(p.lines) - p.window(h)
	if max < 0 {
		return 0
	}
	return max
}

// key scrolls the pager and reports whether Esc/q asked to close it.
func (p pagerModel) key(s string, r rune, h int) (pagerModel, bool) {
	switch {
	case s == "up" || (s == "rune" && r == 'k'):
		if p.offset > 0 {
			p.offset--
		}
	case s == "down" || (s == "rune" && r == 'j'):
		if p.offset < p.maxOffset(h) {
			p.offset++
		}
	case s == "esc" || s == "quit" || (s == "rune" && r == 'q'):
		return p, true // close
	}
	return p, false
}
