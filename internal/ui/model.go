package ui

import "github.com/divkov575/rbg/internal/core"

// ViewMode is which lens the list screen shows. ctrl-s cycles through them.
type ViewMode int

const (
	ViewRemote ViewMode = iota
	ViewLocal
	ViewCombined
	ViewProject
)

// Next returns the next view in the cycle (wraps Project → Remote).
func (v ViewMode) Next() ViewMode { return (v + 1) % 4 }

// String names the view for the header/hints.
func (v ViewMode) String() string {
	switch v {
	case ViewRemote:
		return "remote"
	case ViewLocal:
		return "local"
	case ViewCombined:
		return "combined"
	case ViewProject:
		return "project"
	}
	return "?"
}

// ActionKind is an intent the I/O loop fulfills (slice 2). The pure UI never
// performs I/O; it returns an Action describing what the user asked for.
type ActionKind int

const (
	ActNone ActionKind = iota
	ActQuit
	ActRefresh
	ActRun    // launch/run the selected agent
	ActSend   // send Task to the selected agent
	ActKill   // stop the selected agent
	ActRead   // read the selected agent's transcript
	ActAdopt  // adopt the selected (foreign) agent
	ActCreate // create a new held agent from Spec
)

// Action is what an Update returns for the loop to fulfill. Name/Task/Spec carry
// the operands the chosen ActionKind needs.
type Action struct {
	Kind ActionKind
	Name string
	Task string
	Spec core.Agent
}

// Screen is one interactive view. Update mutates the model and returns an Action
// for the loop; View renders the whole screen; Hints is the key-legend footer.
// Screens push/pop children via the Model's stack — no boolean mode-flags.
type Screen interface {
	Update(m *Model, k Key, r rune) Action
	View(m *Model) string
	Hints() string
}

// Model is the whole dashboard state: the reconciled inventory, the current
// view, cursor, terminal size, a status line, a text buffer (for input
// screens), and the screen stack.
type Model struct {
	Agents []core.Agent
	View   ViewMode
	Cursor int
	W, H   int
	Status string
	Buffer string
	stack  []Screen
}

// NewModel builds a model over the given inventory (no screens pushed yet).
func NewModel(agents []core.Agent) *Model {
	return &Model{Agents: agents, View: ViewRemote}
}

// Top returns the current (top-of-stack) screen, or nil if the stack is empty
// (the loop treats a nil Top as "quit").
func (m *Model) Top() Screen {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

func (m *Model) push(s Screen) { m.stack = append(m.stack, s) }

func (m *Model) pop() {
	if len(m.stack) > 0 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// Visible returns the agents shown by the current view — the pure lens applied
// to the inventory. Project/Combined show all (grouping/sectioning is a render
// concern); Local/Remote filter by machine.
func (m *Model) Visible() []core.Agent {
	switch m.View {
	case ViewLocal:
		return core.OnMachine(m.Agents, core.Local)
	case ViewRemote:
		return core.OnMachine(m.Agents, core.Remote)
	default: // Combined, Project
		return m.Agents
	}
}

// Selected returns the agent under the cursor within the current view, or
// (zero,false) if the cursor is out of range or the view is empty.
func (m *Model) Selected() (core.Agent, bool) {
	vis := m.Visible()
	if m.Cursor < 0 || m.Cursor >= len(vis) {
		return core.Agent{}, false
	}
	return vis[m.Cursor], true
}

// inputScreen's behavior is added in Task 5; declared here (with the fields it
// will carry) so the stack and list.go compile. The stub methods are replaced
// with real bodies in input.go.
type inputScreen struct {
	mode   inputMode
	target string
}

func (s *inputScreen) Update(m *Model, k Key, r rune) Action { return Action{} }
func (s *inputScreen) View(m *Model) string                  { return "" }
func (s *inputScreen) Hints() string                         { return "" }
