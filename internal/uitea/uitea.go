// Package uitea is rbg's Bubble Tea dashboard: an interactive agents view
// modelled on Claude Code's `claude agents` screen. It drives the same engine
// as the scriptable CLI through the Ops interface, and runs every engine call
// as an async tea.Cmd so the UI never blocks on SSH. The pure domain types come
// from internal/core; nothing here performs I/O except through Ops.
package uitea

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/divkov575/rbg/internal/core"
)

// Ops is the engine surface the dashboard drives. *engine.Engine satisfies it
// (identical to cli.Ops), so the UI is testable with a fake — no real SSH.
type Ops interface {
	List() ([]core.Agent, error)
	Create(spec core.Agent) (core.Agent, error)
	Run(name string) error
	Send(name, task string) error
	Read(name string) ([]byte, error)
	Kill(name string) error
	Adopt(name string) error
}

// mode is which screen the model is showing.
type mode int

const (
	modeList  mode = iota // the agents list
	modeInput             // composing a create or send
	modePager             // reading a transcript
)

// --- messages (results of async engine work) ---

// agentsMsg carries a refreshed inventory (err is a degradation warning; the
// list is still usable).
type agentsMsg struct {
	agents []core.Agent
	err    error
}

// statusMsg reports the outcome of a mutating op and requests a refresh.
type statusMsg struct{ text string }

// transcriptMsg carries a fetched transcript for the pager.
type transcriptMsg struct {
	name string
	data []byte
	err  error
}

// Model is the whole dashboard state.
type Model struct {
	ops    Ops
	agents []core.Agent
	cursor int
	view   viewMode // which lens (remote/local/combined/project)
	w, h   int
	status string
	mode   mode

	input inputModel
	pager pagerModel
}

// New builds a dashboard model over ops.
func New(ops Ops) Model {
	// Default to the combined lens: most agents (and every newly-created managed
	// one) live locally, so defaulting to the remote-only lens hid them and made
	// the dashboard look empty. Combined shows both machines at once.
	return Model{ops: ops, view: viewCombined}
}

// Init kicks off the first inventory load.
func (m Model) Init() tea.Cmd { return m.listCmd() }

// --- async commands ---

func (m Model) listCmd() tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		agents, err := ops.List()
		return agentsMsg{agents: agents, err: err}
	}
}

func (m Model) runCmd(name string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		if err := ops.Run(name); err != nil {
			return statusMsg{"run failed: " + err.Error()}
		}
		return statusMsg{"ran " + name}
	}
}

func (m Model) killCmd(name string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		if err := ops.Kill(name); err != nil {
			return statusMsg{"kill failed: " + err.Error()}
		}
		return statusMsg{"killed " + name}
	}
}

func (m Model) adoptCmd(name string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		if err := ops.Adopt(name); err != nil {
			return statusMsg{"adopt failed: " + err.Error()}
		}
		return statusMsg{"adopted " + name}
	}
}

func (m Model) sendCmd(name, task string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		if err := ops.Send(name, task); err != nil {
			return statusMsg{"send failed: " + err.Error()}
		}
		return statusMsg{"sent to " + name}
	}
}

func (m Model) createCmd(spec core.Agent) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		if _, err := ops.Create(spec); err != nil {
			return statusMsg{"create failed: " + err.Error()}
		}
		return statusMsg{"created " + spec.Name}
	}
}

func (m Model) readCmd(name string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		data, err := ops.Read(name)
		return transcriptMsg{name: name, data: data, err: err}
	}
}

// selected returns the agent under the cursor within the current lens.
func (m Model) selected() (core.Agent, bool) {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return core.Agent{}, false
	}
	return vis[m.cursor], true
}

// clampCursor keeps the cursor within the visible bounds.
func (m *Model) clampCursor() {
	n := len(m.visible())
	switch {
	case n == 0:
		m.cursor = 0
	case m.cursor < 0:
		m.cursor = 0
	case m.cursor >= n:
		m.cursor = n - 1
	}
}
