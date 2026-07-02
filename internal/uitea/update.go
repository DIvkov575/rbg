package uitea

import (
	"strings"

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
		// The session view is already open (loading). Fill it, or show the error
		// on its status line — but only if we're still viewing that agent.
		if m.mode != modePager || m.pager.agent != msg.name {
			return m, nil
		}
		if msg.err != nil {
			m.pager.loading = false
			m.pager.status = "read failed: " + msg.err.Error()
			return m, nil
		}
		m.pager = m.pager.setTranscript(msg.data)
		return m, nil

	case sentMsg:
		// A follow-up finished sending; clear the sending state and re-read the
		// transcript so the new turn appears.
		if m.mode != modePager || m.pager.agent != msg.name {
			return m, nil
		}
		m.pager.sending = false
		if msg.err != nil {
			m.pager.status = "send failed: " + msg.err.Error()
			return m, nil
		}
		m.pager.status = "sent — refreshing…"
		m.pager.loading = true
		return m, m.readCmd(msg.name)

	case spawnedMsg:
		m.spawning = false
		if msg.err != nil {
			m.status = "spawn failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "spawned " + msg.name
		return m, m.listCmd() // refresh so the new agent appears

	case spinTick:
		// Advance the spinner while something is in flight: the session view's
		// load/send, or a list-view spawn. Otherwise let it stop (no re-tick).
		busy := (m.mode == modePager && (m.pager.loading || m.pager.sending)) || m.spawning
		if busy {
			m.pager.spin = (m.pager.spin + 1) % len(spinnerFrames)
			m.spin = (m.spin + 1) % len(spinnerFrames)
			return m, spinCmd()
		}
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
	case modePager:
		p, act := m.pager.key(s, r, m.h)
		m.pager = p
		switch act {
		case pagerClose:
			m.mode = modeList
			return m, nil
		case pagerSend:
			task := strings.TrimSpace(m.pager.prompt)
			m.pager.prompt = ""
			m.pager.sending = true
			m.pager.status = "sending…"
			return m, tea.Batch(m.sendFollowupCmd(m.pager.agent, task), spinCmd())
		}
		return m, nil
	default:
		return m.keyList(s, r)
	}
}

// keyList handles keys on the agents list. The prompt bar is always focused for
// typing (printable runes append to it); enter with a non-empty prompt SPAWNS a
// background agent in the selected project, else opens the selected row. Nav is
// arrows; per-agent actions are ctrl-keys so they don't collide with typing.
func (m Model) keyList(s string, r rune) (tea.Model, tea.Cmd) {
	switch s {
	case "quit":
		return m, tea.Quit
	case "cycle": // tab / ctrl-s
		m.view = m.view.next()
		m.clampCursor()
		return m, nil
	case "up":
		m.cursor--
		m.clampCursor()
		return m, nil
	case "down":
		m.cursor++
		m.clampCursor()
		return m, nil
	case "backspace":
		if n := len(m.listPrompt); n > 0 {
			m.listPrompt = m.listPrompt[:n-1]
		}
		return m, nil
	case "rune":
		m.listPrompt += string(r)
		return m, nil
	case "enter":
		if strings.TrimSpace(m.listPrompt) != "" {
			return m.spawnFromPrompt()
		}
		return m.openSelected()
	case "run":
		if a, ok := m.selected(); ok {
			m.status = "running " + a.Name + "…"
			return m, m.runCmd(a.Name)
		}
	case "kill":
		if a, ok := m.selected(); ok {
			m.status = "killing " + a.Name + "…"
			return m, m.killCmd(a.Name)
		}
	case "adopt":
		if a, ok := m.selected(); ok && a.IsForeign() {
			return m, m.adoptCmd(a.Name)
		}
	case "refresh":
		m.status = "refreshing…"
		return m, m.listCmd()
	}
	return m, nil
}

// openSelected opens the selected agent's conversation (local → real client,
// remote → session view).
func (m Model) openSelected() (tea.Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a.Where == core.Local && a.Session != "" {
		return m, m.openClientCmd(a)
	}
	if a.Session == "" {
		m.status = a.Name + " has not run yet (no transcript)"
		return m, nil
	}
	m.pager = newSessionView("session · "+a.Name, a.Name)
	m.mode = modePager
	return m, tea.Batch(m.readCmd(a.Name), spinCmd())
}

// spawnFromPrompt launches a background agent for the typed task. The machine is
// the view's machine (remote/local); in the project view it follows the selected
// agent. The project (repo+dir) is taken from the selected agent so the new chat
// links to the selected project.
func (m Model) spawnFromPrompt() (tea.Model, tea.Cmd) {
	task := strings.TrimSpace(m.listPrompt)
	sel, hasSel := m.selected()

	where, ok := m.view.machine()
	if !ok { // project view: follow the selected agent
		if hasSel {
			where = sel.Where
		} else {
			where = core.Remote
		}
	}
	spec := core.Agent{Task: task, Where: where}
	if hasSel {
		spec.Repo = sel.Repo // link to the selected agent's project
		spec.Dir = sel.Dir
	}
	m.listPrompt = ""
	m.spawning = true
	m.status = "spawning agent…"
	return m, tea.Batch(m.spawnCmd(spec), spinCmd())
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
	// Per-agent actions are ctrl-keys so the list's prompt bar stays free for
	// typing (g/x/a would otherwise be captured as text).
	case tea.KeyCtrlG:
		return "run", 0
	case tea.KeyCtrlX:
		return "kill", 0
	case tea.KeyCtrlA:
		return "adopt", 0
	case tea.KeyCtrlR:
		return "refresh", 0
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
