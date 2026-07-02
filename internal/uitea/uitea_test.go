package uitea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/divkov575/rbg/internal/core"
)

// errStr is a trivial error for tests.
type errStr string

func (e errStr) Error() string { return string(e) }

// recOps records calls and returns canned data.
type recOps struct {
	agents   []core.Agent
	projects []core.Project
	ran      string
	killed   string
	adopted  string
	sent     [2]string
	created  core.Agent
	readName string
	readData []byte
	repaired bool
}

func (o *recOps) List() ([]core.Agent, error)             { return o.agents, nil }
func (o *recOps) Create(a core.Agent) (core.Agent, error) {
	o.created = a
	if a.Name == "" { // mirror the engine deriving a name from the task
		a.Name = "derived-" + a.Task
	}
	return a, nil
}
func (o *recOps) Run(n string) error                      { o.ran = n; return nil }
func (o *recOps) Send(n, t string) error                  { o.sent = [2]string{n, t}; return nil }
func (o *recOps) Read(n string) ([]byte, error)           { o.readName = n; return o.readData, nil }
func (o *recOps) Kill(n string) error                     { o.killed = n; return nil }
func (o *recOps) Adopt(n string) error                    { o.adopted = n; return nil }
func (o *recOps) RepairRemote() (bool, error)             { o.repaired = true; return true, nil }
func (o *recOps) Projects() []core.Project                { return o.projects }

func remote(name string) core.Agent {
	return core.Agent{Name: name, Where: core.Remote, State: core.Running, Origin: core.Managed}
}

// step feeds a key rune to the model and returns the new model + any command.
func stepRune(m Model, r rune) (Model, tea.Cmd) {
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return mm.(Model), cmd
}

func loaded(o *recOps) Model {
	m := New(o)
	mm, _ := m.Update(agentsMsg{agents: o.agents})
	return mm.(Model)
}

func TestAgentsMsgPopulatesAndClamps(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("a"), remote("b")}}
	m := loaded(o)
	m.cursor = 5
	mm, _ := m.Update(agentsMsg{agents: []core.Agent{remote("only")}})
	m = mm.(Model)
	if len(m.agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(m.agents))
	}
	if m.cursor != 0 {
		t.Errorf("cursor should clamp to 0, got %d", m.cursor)
	}
}

func TestStatusMsgTriggersRefresh(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("a")}}
	m := loaded(o)
	mm, cmd := m.Update(statusMsg{"ran a"})
	m = mm.(Model)
	if m.status != "ran a" {
		t.Errorf("status = %q", m.status)
	}
	if cmd == nil {
		t.Fatal("statusMsg should trigger a refresh cmd")
	}
	if _, ok := cmd().(agentsMsg); !ok {
		t.Errorf("refresh cmd should yield agentsMsg")
	}
}

func TestEnterOpensSessionViewInstantlyThenFills(t *testing.T) {
	// A remote agent with a session: enter opens the session view IMMEDIATELY in
	// a loading state (fast — no blocking read) and fires a read + spinner.
	a := core.Agent{Name: "job", Where: core.Remote, State: core.Running, Origin: core.Managed, Session: "S1"}
	o := &recOps{agents: []core.Agent{a}, readData: []byte("l1\r\nl2\r\n")}
	m := loaded(o)
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.mode != modePager {
		t.Fatalf("enter should open the session view immediately, mode=%d", m.mode)
	}
	if !m.pager.loading {
		t.Error("session view should start in loading state")
	}
	if cmd == nil {
		t.Fatal("enter should fire read + spinner commands")
	}
	// The batched command produces a transcriptMsg (and a spinTick); feed the
	// transcript back to fill the view.
	m2, _ := m.Update(transcriptMsg{name: "job", data: o.readData})
	m = m2.(Model)
	if m.pager.loading {
		t.Error("transcript arrival should clear loading")
	}
	for _, ln := range m.pager.lines {
		if len(ln) > 0 && ln[len(ln)-1] == '\r' {
			t.Errorf("pager line has stray CR: %q", ln)
		}
	}
}

func TestSessionViewSendsFollowup(t *testing.T) {
	a := core.Agent{Name: "job", Where: core.Remote, State: core.Running, Origin: core.Managed, Session: "S1"}
	o := &recOps{agents: []core.Agent{a}, readData: []byte("hi")}
	m := loaded(o)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open session view
	m = mm.(Model)
	m = typeStr(m, "do more") // type into the prompt bar
	if m.pager.prompt != "do more" {
		t.Fatalf("prompt bar should capture typing, got %q", m.pager.prompt)
	}
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // send
	m = mm.(Model)
	if !m.pager.sending {
		t.Error("sending a follow-up should set the sending state")
	}
	if m.pager.prompt != "" {
		t.Error("prompt should clear after send")
	}
	if cmd == nil {
		t.Fatal("send should dispatch a command")
	}
	// drain the batch until we see the sentMsg, then confirm Send was called.
	drainFor(t, cmd, func(msg tea.Msg) bool {
		_, ok := msg.(sentMsg)
		return ok
	})
	if o.sent != [2]string{"job", "do more"} {
		t.Errorf("Send got %v, want [job, do more]", o.sent)
	}
}

// drainFor runs a (possibly batched) command and asserts some produced message
// satisfies want. tea.Batch returns a BatchMsg of sub-commands.
func drainFor(t *testing.T, cmd tea.Cmd, want func(tea.Msg) bool) {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if want(c()) {
				return
			}
		}
		t.Fatal("no batched message satisfied the predicate")
	}
	if !want(msg) {
		t.Fatal("command message did not satisfy the predicate")
	}
}

func enterKey(m Model) (Model, tea.Cmd) {
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return mm.(Model), cmd
}

func typeStr(m Model, s string) Model {
	for _, r := range s {
		m, _ = stepRune(m, r)
	}
	return m
}

func TestPromptBarSpawnsInSelectedProject(t *testing.T) {
	// Typing into the list prompt bar and pressing enter spawns (create+run) a
	// background agent in the selected agent's project on the view's machine.
	sel := core.Agent{Name: "existing", Where: core.Remote, State: core.Running, Origin: core.Managed,
		Repo: "git@github.com:me/app.git", Dir: "/desk/workplace/app", Session: "S1"}
	o := &recOps{agents: []core.Agent{sel}}
	m := loaded(o) // default view = remote; cursor on the one agent
	m = typeStr(m, "add a feature")
	if m.listPrompt != "add a feature" {
		t.Fatalf("prompt bar should capture typing, got %q", m.listPrompt)
	}
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.spawning {
		t.Error("enter with a prompt should set spawning")
	}
	if m.listPrompt != "" {
		t.Error("prompt should clear after spawn")
	}
	if cmd == nil {
		t.Fatal("spawn should dispatch a command")
	}
	drainFor(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(spawnedMsg); return ok })
	// created spec: task from prompt, project (repo+dir) + machine from selection
	if o.created.Task != "add a feature" {
		t.Errorf("spawn task = %q, want 'add a feature'", o.created.Task)
	}
	if o.created.Repo != sel.Repo || o.created.Dir != sel.Dir {
		t.Errorf("spawn should link to the selected project (%q/%q), got %q/%q",
			sel.Repo, sel.Dir, o.created.Repo, o.created.Dir)
	}
	if o.created.Where != core.Remote {
		t.Errorf("remote view spawn should target remote, got %q", o.created.Where)
	}
	// and it was RUN (spawn = create + run)
	if o.ran == "" {
		t.Error("spawn should also run the created agent")
	}
}

func TestPromptBarMachineFollowsView(t *testing.T) {
	sel := core.Agent{Name: "x", Where: core.Remote, State: core.Running, Origin: core.Managed}
	o := &recOps{agents: []core.Agent{sel, {Name: "loc", Where: core.Local, State: core.Running, Origin: core.Managed}}}
	m := loaded(o)
	m.view = viewLocal // local lens → spawn should target local
	m.clampCursor()
	m = typeStr(m, "task")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainFor(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(spawnedMsg); return ok })
	if o.created.Where != core.Local {
		t.Errorf("local view spawn should target local, got %q", o.created.Where)
	}
}

func TestEmptyPromptEnterOpensSelectedInstead(t *testing.T) {
	// With an empty prompt, enter opens the selected agent (no spawn).
	sel := core.Agent{Name: "job", Where: core.Remote, State: core.Running, Origin: core.Managed, Session: "S1"}
	o := &recOps{agents: []core.Agent{sel}}
	m := loaded(o)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.spawning {
		t.Error("empty prompt should not spawn")
	}
	if m.mode != modePager {
		t.Errorf("empty-prompt enter should open the selected agent's session view, mode=%d", m.mode)
	}
}

func TestTabCyclesLens(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("a")}}
	m := loaded(o)
	// Default is the remote lens; tab cycles remote → local → projects.
	if m.view != viewRemote {
		t.Fatalf("initial view = %v, want remote", m.view)
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(Model)
	if m.view != viewLocal {
		t.Errorf("tab from remote should advance to local, got %v", m.view)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(Model)
	if m.view != viewProject {
		t.Errorf("tab from local should advance to projects, got %v", m.view)
	}
}

func TestProjectSelectorSpawns(t *testing.T) {
	// ^n opens the project selector; choose a project, type a task, enter spawns
	// (create+run) there. Index 0 is "(no repo)", 1 is the first suggestion.
	o := &recOps{
		agents:   []core.Agent{remote("a")},
		projects: []core.Project{{Label: "rbg (github)", Repo: "me/rbg", Origin: "github"}},
	}
	m := loaded(o)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN}) // open selector
	m = mm.(Model)
	if m.mode != modePicker {
		t.Fatalf("^n should open the project selector, mode=%d", m.mode)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // move to the github project
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // choose it → task stage
	m = mm.(Model)
	if m.picker.choosing {
		t.Fatal("choosing a project should advance to task entry")
	}
	m = typeStr(m, "add tests")
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // spawn
	m = mm.(Model)
	if !m.spawning {
		t.Error("picker spawn should set spawning")
	}
	drainFor(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(spawnedMsg); return ok })
	if o.created.Repo != "me/rbg" || o.created.Task != "add tests" {
		t.Errorf("picker spawn wrong: %+v", o.created)
	}
	if o.ran == "" {
		t.Error("picker spawn should also run the agent")
	}
}

func TestRepairKeyDispatchesRepair(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("a")}}
	m := loaded(o)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP}) // ^p = repair
	if cmd == nil {
		t.Fatal("ctrl-p should dispatch a repair command")
	}
	drainFor(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(repairedMsg); return ok })
	if !o.repaired {
		t.Error("RepairRemote should have been called")
	}
}

func TestFriendlyErrTranslatesTimeout(t *testing.T) {
	got := friendlyErr(errStr("remote claude agents exited 255: Connection timed out"))
	if !strings.Contains(got, "repair") {
		t.Errorf("timeout error should suggest repair, got %q", got)
	}
}

func TestQuitKey(t *testing.T) {
	o := &recOps{}
	m := loaded(o)
	// q is now typed into the prompt bar; quit is ctrl-c.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
	// tea.Quit is a command returning a quitMsg; just assert non-nil + type name.
	if msg := cmd(); msg == nil {
		t.Error("quit cmd should produce a message")
	}
}

func TestAdoptOnlyForeign(t *testing.T) {
	o := &recOps{agents: []core.Agent{{Name: "mine", Where: core.Remote, State: core.Running, Origin: core.Managed}}}
	m := loaded(o)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA}) // ^a; managed agent → no adopt
	if cmd != nil {
		t.Error("adopt on a managed agent should do nothing")
	}
	o2 := &recOps{agents: []core.Agent{{Name: "wild", Where: core.Remote, State: core.Running, Origin: core.Foreign}}}
	m2 := loaded(o2)
	_, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd2 == nil {
		t.Fatal("adopt on a foreign agent should dispatch")
	}
	cmd2()
	if o2.adopted != "wild" {
		t.Errorf("Adopt called with %q, want wild", o2.adopted)
	}
}
