// Package uitea is rbg's Bubble Tea dashboard: an interactive agents view
// modelled on Claude Code's `claude agents` screen. It drives the same engine
// as the scriptable CLI through the Ops interface, and runs every engine call
// as an async tea.Cmd so the UI never blocks on SSH. The pure domain types come
// from internal/core; nothing here performs I/O except through Ops.
package uitea

import (
	"os/exec"
	"time"

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
	Projects() []core.Project
}

// mode is which screen the model is showing.
type mode int

const (
	modeList  mode = iota // the agents list (with the spawn prompt bar)
	modePager             // the remote session view (transcript + prompt bar)
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

// sentMsg reports a follow-up send from the session view completed.
type sentMsg struct {
	name string
	err  error
}

// spinTick advances the processing spinner.
type spinTick struct{}

// spawnedMsg reports a prompt-bar spawn (create+run) completed.
type spawnedMsg struct {
	name string
	err  error
}

// Model is the whole dashboard state.
type Model struct {
	ops    Ops
	agents []core.Agent
	cursor int
	view   viewMode // which lens (remote/local/project)
	w, h   int
	status string
	mode   mode

	listPrompt string // the list view's prompt bar (spawns a bg agent on enter)
	spawning   bool   // a spawn is in flight (spinner)
	spin       int    // spinner frame

	pager pagerModel
}

// New builds a dashboard model over ops.
func New(ops Ops) Model {
	// Default to the remote lens (the primary use case: delegating to the
	// desktop). tab cycles remote → local → projects.
	return Model{ops: ops, view: viewRemote}
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

// sendFollowupCmd sends a follow-up from the session view (distinct from the
// list's sendCmd: it yields a sentMsg so the view re-reads its own transcript
// rather than refreshing the list).
func (m Model) sendFollowupCmd(name, task string) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		return sentMsg{name: name, err: ops.Send(name, task)}
	}
}

// spawnCmd creates a held agent from spec and immediately runs it — a one-shot
// "spawn a background agent" from the list prompt bar. Yields a spawnedMsg.
func (m Model) spawnCmd(spec core.Agent) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		created, err := ops.Create(spec)
		if err != nil {
			return spawnedMsg{err: err}
		}
		if err := ops.Run(created.Name); err != nil {
			return spawnedMsg{name: created.Name, err: err}
		}
		return spawnedMsg{name: created.Name}
	}
}

// spinCmd schedules the next spinner frame ~every 100ms.
func spinCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinTick{} })
}

// selected returns the agent under the cursor within the current lens.
func (m Model) selected() (core.Agent, bool) {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return core.Agent{}, false
	}
	return vis[m.cursor], true
}

// openClientCmd suspends the dashboard and hands the terminal to the real
// interactive `claude --resume <session>` client, run in the agent's working
// dir. On exit the dashboard resumes and the inventory refreshes. This is why a
// local conversation opens the genuine client instead of a custom render.
func (m Model) openClientCmd(a core.Agent) tea.Cmd {
	c := exec.Command("claude", "--resume", a.Session)
	if a.Dir != "" {
		c.Dir = a.Dir
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		// whatever happened, re-pull the inventory so state reflects the session.
		agents, lerr := m.ops.List()
		return agentsMsg{agents: agents, err: lerr}
	})
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
