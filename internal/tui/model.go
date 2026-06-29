// Package tui holds the rbg dashboard. model.go is the PURE state machine —
// no terminal, no I/O — so it is fully unit-testable. term*.go drives it.
package tui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/session"
)

// Key is an abstract key event fed to Update (decoded from raw bytes by the
// terminal layer).
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyView    // ⏎ or v: load selected transcript
	KeyAttach  // a
	KeyRefresh // r
	KeyQuit    // q
)

// Action is what the terminal loop must do after an Update (the model itself
// performs no I/O).
type Action int

const (
	ActionNone Action = iota
	ActionLoadTranscript
	ActionAttach
	ActionRefresh
	ActionQuit
)

// Model is the dashboard state.
type Model struct {
	Sessions   []session.Session
	Selected   int
	Transcript string // rendered text of the currently-shown transcript
}

// New builds a model from a session list.
func New(sessions []session.Session) Model {
	return Model{Sessions: sessions}
}

// SelectedName returns the highlighted agent's name, or "" if none.
func (m Model) SelectedName() string {
	if len(m.Sessions) == 0 || m.Selected < 0 || m.Selected >= len(m.Sessions) {
		return ""
	}
	return m.Sessions[m.Selected].Name
}

// SetSessions replaces the list (after a refresh), clamping Selected.
func (m Model) SetSessions(s []session.Session) Model {
	m.Sessions = s
	if m.Selected >= len(s) {
		m.Selected = len(s) - 1
	}
	if m.Selected < 0 {
		m.Selected = 0
	}
	return m
}

// SetTranscript stores rendered transcript text for the right pane.
func (m Model) SetTranscript(text string) Model {
	m.Transcript = text
	return m
}

// Update applies a key, returning the new model and an Action for the loop.
func Update(m Model, k Key) (Model, Action) {
	switch k {
	case KeyUp:
		if m.Selected > 0 {
			m.Selected--
		}
	case KeyDown:
		if m.Selected < len(m.Sessions)-1 {
			m.Selected++
		}
	case KeyView:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionLoadTranscript
	case KeyAttach:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionAttach
	case KeyRefresh:
		return m, ActionRefresh
	case KeyQuit:
		return m, ActionQuit
	}
	return m, ActionNone
}

// View renders the two-pane dashboard to a string.
func View(m Model) string {
	var b strings.Builder
	b.WriteString("agents:\n")
	for i, s := range m.Sessions {
		cursor := "  "
		if i == m.Selected {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, s.Name))
	}
	b.WriteString("\n[↑/↓ move  ⏎/v view  a attach  r refresh  q quit]\n")
	b.WriteString("\ntranscript:\n")
	if m.Transcript == "" {
		b.WriteString("(press ⏎ to load)\n")
	} else {
		b.WriteString(m.Transcript)
	}
	return b.String()
}
