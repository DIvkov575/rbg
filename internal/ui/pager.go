package ui

import (
	"fmt"
	"strings"
)

// pagerScreen is a read-only, scrollable text view used to display a transcript
// (fulfilling ActRead). It owns its own lines and scroll offset; Esc/q pops it
// off the stack back to the list. It performs no I/O — the loop hands it the
// already-fetched text.
type pagerScreen struct {
	title  string
	lines  []string
	offset int
}

func newPagerScreen(title string, lines []string) *pagerScreen {
	return &pagerScreen{title: title, lines: lines}
}

// window is how many text lines fit below the title/hints chrome.
func (s *pagerScreen) window(m *Model) int {
	h := m.H - 4 // title line + blank + hints + margin
	if h < 1 {
		h = 1
	}
	return h
}

// maxOffset is the largest first-line index that still shows a full window
// (or 0 when everything fits).
func (s *pagerScreen) maxOffset(m *Model) int {
	max := len(s.lines) - s.window(m)
	if max < 0 {
		return 0
	}
	return max
}

func (s *pagerScreen) Update(m *Model, k Key, r rune) Action {
	switch {
	case k == KeyUp || (k == KeyRune && r == 'k'):
		if s.offset > 0 {
			s.offset--
		}
	case k == KeyDown || (k == KeyRune && r == 'j'):
		if s.offset < s.maxOffset(m) {
			s.offset++
		}
	case k == KeyEsc || k == KeyQuit || (k == KeyRune && r == 'q'):
		m.pop()
	}
	return Action{}
}

func (s *pagerScreen) View(m *Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", fg(colBlue, bold(s.title)))
	win := s.window(m)
	end := s.offset + win
	if end > len(s.lines) {
		end = len(s.lines)
	}
	for i := s.offset; i < end; i++ {
		b.WriteString(s.lines[i])
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%s\n", dim(s.Hints()))
	return b.String()
}

func (s *pagerScreen) Hints() string { return "j/k scroll · esc back · q back" }

var _ Screen = (*pagerScreen)(nil)
