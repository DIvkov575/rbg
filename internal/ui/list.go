package ui

import (
	"fmt"
	"strings"
)

// listScreen is the base screen: the agent list under the current view lens,
// with cursor navigation, ctrl-s view cycling, and per-agent action keys.
type listScreen struct{}

// Update interprets keys for the list. Navigation/cycle mutate the model and
// return ActNone; action keys return an Action for the loop, carrying the
// selected agent's name. `n` pushes an input screen to compose a new task.
func (s *listScreen) Update(m *Model, k Key, r rune) Action {
	switch k {
	case KeyQuit:
		return Action{Kind: ActQuit}
	case KeyRefresh:
		return Action{Kind: ActRefresh}
	case KeyCycleView:
		m.View = m.View.Next()
		m.clampCursor()
		return Action{}
	case KeyUp:
		m.moveCursor(-1)
		return Action{}
	case KeyDown:
		m.moveCursor(1)
		return Action{}
	case KeyEnter:
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActRead, Name: a.Name}
		}
		return Action{}
	case KeyRune:
		return s.rune(m, r)
	}
	return Action{}
}

// rune interprets a printable key for the list (nav letters + action letters).
func (s *listScreen) rune(m *Model, r rune) Action {
	switch r {
	case 'j':
		m.moveCursor(1)
	case 'k':
		m.moveCursor(-1)
	case 'q':
		return Action{Kind: ActQuit}
	case 'r':
		return Action{Kind: ActRefresh}
	case 'g': // go: run the selected agent
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActRun, Name: a.Name}
		}
	case 'x': // kill
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActKill, Name: a.Name}
		}
	case 's': // send: compose a follow-up (push input in send mode)
		if a, ok := m.Selected(); ok {
			m.push(newInputScreen(sendMode, a.Name))
		}
	case 'A': // adopt (foreign only)
		if a, ok := m.Selected(); ok && a.IsForeign() {
			return Action{Kind: ActAdopt, Name: a.Name}
		}
	case 'n': // new: compose a task to create
		m.push(newInputScreen(createMode, ""))
	}
	return Action{}
}

func (s *listScreen) Hints() string {
	return "j/k move · ctrl-s view · enter read · g run · s send · x kill · A adopt · n new · r refresh · q quit"
}

// View renders the list screen: a header (view name + counts), the body (the
// current lens), a status line, and the hints footer.
func (s *listScreen) View(m *Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "rbg — %s view  (%d agents)\n\n", m.View, len(m.Agents))
	b.WriteString(renderList(m))
	if m.Status != "" {
		fmt.Fprintf(&b, "\n%s\n", m.Status)
	}
	fmt.Fprintf(&b, "\n%s\n", s.Hints())
	return b.String()
}

// moveCursor moves by delta and clamps to the current view's bounds.
func (m *Model) moveCursor(delta int) {
	m.Cursor += delta
	m.clampCursor()
}

// clampCursor keeps the cursor within [0, len(visible)-1], or 0 when empty.
func (m *Model) clampCursor() {
	n := len(m.Visible())
	if n == 0 {
		m.Cursor = 0
		return
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= n {
		m.Cursor = n - 1
	}
}

var _ Screen = (*listScreen)(nil)

// (temporary until Task 5 defines the real input screen)
type inputMode int

const (
	createMode inputMode = iota
	sendMode
)

func newInputScreen(mode inputMode, target string) *inputScreen {
	return &inputScreen{mode: mode, target: target}
}
