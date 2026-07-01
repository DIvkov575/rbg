package uitea

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/divkov575/rbg/internal/core"
)

// Update is the Bubble Tea reducer. It folds window-size events, async engine
// results (agentsMsg/statusMsg/transcriptMsg), and keystrokes into new state +
// commands. Engine calls are dispatched as commands so the event loop never
// blocks on SSH.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case agentsMsg:
		m.agents = msg.agents
		if msg.err != nil {
			m.status = "inventory may be incomplete: " + msg.err.Error()
		}
		m.clampCursor()
		return m, nil

	case statusMsg:
		m.status = msg.text
		return m, m.listCmd() // refresh after any mutation

	case transcriptMsg:
		if msg.err != nil {
			m.status = "read failed: " + msg.err.Error()
			return m, nil
		}
		m.pager = newPager("transcript · "+msg.name, msg.data)
		m.mode = modePager
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keystroke by the current mode.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s, r := keyName(msg)
	// ctrl+z closes any screen: from a sub-screen (input/pager/picker) it backs
	// out to the list; from the list it quits the dashboard.
	if s == "close" {
		if m.mode != modeList {
			m.mode = modeList
			return m, nil
		}
		return m, tea.Quit
	}
	switch m.mode {
	case modeInput:
		return m.keyInput(s, r)
	case modePager:
		p, closed := m.pager.key(s, r, m.h)
		m.pager = p
		if closed {
			m.mode = modeList
		}
		return m, nil
	case modePicker:
		return m.keyPicker(s, r)
	default:
		return m.keyList(s, r)
	}
}

// keyList handles keys on the agents list.
func (m Model) keyList(s string, r rune) (tea.Model, tea.Cmd) {
	switch {
	case s == "quit" || (s == "rune" && r == 'q'):
		return m, tea.Quit
	case s == "cycle" || s == "tab" || (s == "rune" && r == '\t'):
		m.view = m.view.next()
		m.clampCursor()
		return m, nil
	case s == "up" || (s == "rune" && r == 'k'):
		m.cursor--
		m.clampCursor()
		return m, nil
	case s == "down" || (s == "rune" && r == 'j'):
		m.cursor++
		m.clampCursor()
		return m, nil
	case s == "enter":
		if a, ok := m.selected(); ok {
			// A LOCAL conversation opens the real interactive claude client (no
			// custom render). A remote one falls back to the read-only pager, since
			// we can't hand a remote TTY to the inline program cleanly.
			if a.Where == core.Local && a.Session != "" {
				return m, m.openClientCmd(a)
			}
			return m, m.readCmd(a.Name)
		}
		return m, nil
	case s == "rune":
		return m.keyListRune(r)
	}
	return m, nil
}

// keyListRune handles action letters on the list.
func (m Model) keyListRune(r rune) (tea.Model, tea.Cmd) {
	switch r {
	case 'r':
		m.status = "refreshing…"
		return m, m.listCmd()
	case 'g': // go: run
		if a, ok := m.selected(); ok {
			m.status = "running " + a.Name + "…"
			return m, m.runCmd(a.Name)
		}
	case 'x': // kill
		if a, ok := m.selected(); ok {
			m.status = "killing " + a.Name + "…"
			return m, m.killCmd(a.Name)
		}
	case 'A': // adopt (foreign only)
		if a, ok := m.selected(); ok && a.IsForeign() {
			return m, m.adoptCmd(a.Name)
		}
	case 's': // send: compose a follow-up
		if a, ok := m.selected(); ok {
			m.input = newSendInput(a.Name)
			m.mode = modeInput
		}
	case 'n': // new: pick a project first, then compose the task
		m.picker = newPicker(m.ops.Projects())
		m.mode = modePicker
	}
	return m, nil
}

// keyPicker handles keys in the project picker. On a choice it stores the repo
// and advances to the task prompt; Esc cancels the create flow.
func (m Model) keyPicker(s string, r rune) (tea.Model, tea.Cmd) {
	pk, done, result := m.picker.key(s, r)
	m.picker = pk
	if !done {
		return m, nil
	}
	if res, ok := result.(pickerDone); ok {
		m.input = newCreateInput(res.repo) // repo chosen; now collect the task
		m.mode = modeInput
	} else {
		m.mode = modeList // cancelled
	}
	return m, nil
}

// keyInput handles keys in the create/send overlay, dispatching the engine
// command when the flow completes.
func (m Model) keyInput(s string, r rune) (tea.Model, tea.Cmd) {
	in, done, result := m.input.key(s, r)
	m.input = in
	if !done {
		return m, nil
	}
	m.mode = modeList
	switch res := result.(type) {
	case createDone:
		m.status = "creating " + res.spec.Name + "…"
		return m, m.createCmd(res.spec)
	case sendDone:
		m.status = "sending to " + res.target + "…"
		return m, m.sendCmd(res.target, res.task)
	default: // cancelled
		return m, nil
	}
}

// keyName maps a Bubble Tea key to a compact (name, rune) pair the sub-models
// switch on. Printable keys are ("rune", r); named keys use their label.
func keyName(k tea.KeyMsg) (string, rune) {
	switch k.Type {
	case tea.KeyCtrlC:
		return "quit", 0
	case tea.KeyCtrlZ:
		return "close", 0
	case tea.KeyCtrlS:
		return "cycle", 0
	case tea.KeyTab:
		return "cycle", 0
	case tea.KeyUp:
		return "up", 0
	case tea.KeyDown:
		return "down", 0
	case tea.KeyEnter:
		return "enter", 0
	case tea.KeyEsc:
		return "esc", 0
	case tea.KeyBackspace:
		return "backspace", 0
	case tea.KeyRunes, tea.KeySpace:
		rs := k.Runes
		if len(rs) == 1 {
			return "rune", rs[0]
		}
		if k.Type == tea.KeySpace {
			return "rune", ' '
		}
	}
	return "", 0
}
