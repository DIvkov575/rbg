package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// inputMode is what the input screen composes.
type inputMode int

const (
	createMode inputMode = iota // compose a task for a new held agent
	sendMode                    // compose a follow-up for a running agent
)

// inputScreen collects a task string, then returns ActCreate (create mode) or
// ActSend (send mode) on Enter, popping back to the list. Esc cancels. It
// exercises the screen stack — no boolean "inputting" flag on the Model.
type inputScreen struct {
	mode   inputMode
	target string // the agent name (send mode)
}

func newInputScreen(mode inputMode, target string) *inputScreen {
	return &inputScreen{mode: mode, target: target}
}

func (s *inputScreen) Update(m *Model, k Key, r rune) Action {
	switch k {
	case KeyEsc:
		m.Buffer = ""
		m.pop()
		return Action{}
	case KeyBackspace:
		if n := len(m.Buffer); n > 0 {
			m.Buffer = m.Buffer[:n-1]
		}
		return Action{}
	case KeyRune:
		m.Buffer += string(r)
		return Action{}
	case KeyEnter:
		task := strings.TrimSpace(m.Buffer)
		if task == "" {
			return Action{} // stay; nothing to submit
		}
		m.Buffer = ""
		m.pop()
		if s.mode == sendMode {
			return Action{Kind: ActSend, Name: s.target, Task: task}
		}
		return Action{Kind: ActCreate, Spec: core.Agent{Task: task}}
	}
	return Action{}
}

func (s *inputScreen) View(m *Model) string {
	title := "New task (will be created as a held agent)"
	if s.mode == sendMode {
		title = fmt.Sprintf("Follow-up to %q", s.target)
	}
	return fmt.Sprintf("%s\n\n> %s\n\n%s\n", title, m.Buffer, s.Hints())
}

func (s *inputScreen) Hints() string { return "type · enter submit · esc cancel" }

var _ Screen = (*inputScreen)(nil)
