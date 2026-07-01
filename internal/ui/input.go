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

// createStage indexes the fields collected in create mode.
const (
	stageName = iota // required
	stageRepo        // optional (may be blank)
	stageTask        // required
)

// inputScreen collects input, then returns ActCreate (create mode) or ActSend
// (send mode) on Enter, popping back to the list. Esc cancels. Create mode
// collects three fields across stages (name → repo → task); send mode collects
// a single task. It exercises the screen stack — no boolean "inputting" flag on
// the Model.
type inputScreen struct {
	mode   inputMode
	target string // the agent name (send mode)

	stage int    // create-mode field index (stageName/stageRepo/stageTask)
	name  string // committed name (create mode)
	repo  string // committed repo (create mode, may be "")
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
		val := strings.TrimSpace(m.Buffer)
		if s.mode == sendMode {
			if val == "" {
				return Action{} // stay; nothing to submit
			}
			m.Buffer = ""
			m.pop()
			return Action{Kind: ActSend, Name: s.target, Task: val}
		}
		// create mode: name (required) → repo (optional) → task (required).
		switch s.stage {
		case stageName:
			if val == "" {
				return Action{} // stay; name required
			}
			s.name = val
			m.Buffer = ""
			s.stage = stageRepo
			return Action{}
		case stageRepo:
			s.repo = val // optional; "" is allowed
			m.Buffer = ""
			s.stage = stageTask
			return Action{}
		default: // stageTask
			if val == "" {
				return Action{} // stay; task required
			}
			m.Buffer = ""
			m.pop()
			return Action{Kind: ActCreate, Spec: core.Agent{Name: s.name, Repo: s.repo, Task: val}}
		}
	}
	return Action{}
}

func (s *inputScreen) View(m *Model) string {
	if s.mode == sendMode {
		title := fmt.Sprintf("Follow-up to %q", s.target)
		return fmt.Sprintf("%s\n\n> %s\n\n%s\n", title, m.Buffer, s.Hints())
	}
	title := "New held agent (step " + fmt.Sprintf("%d/3", s.stage+1) + ")"
	prompt := "Name"
	switch s.stage {
	case stageRepo:
		prompt = "Repo (optional)"
	case stageTask:
		prompt = "Task"
	}
	return fmt.Sprintf("%s\n\n%s\n> %s\n\n%s\n", title, prompt, m.Buffer, s.Hints())
}

func (s *inputScreen) Hints() string {
	if s.mode == createMode && s.stage != stageTask {
		return "type · enter next · esc cancel"
	}
	return "type · enter submit · esc cancel"
}

var _ Screen = (*inputScreen)(nil)
