package uitea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/divkov575/rbg/internal/core"
)

// recOps records calls and returns canned data.
type recOps struct {
	agents   []core.Agent
	ran      string
	killed   string
	adopted  string
	sent     [2]string
	created  core.Agent
	readName string
	readData []byte
}

func (o *recOps) List() ([]core.Agent, error)             { return o.agents, nil }
func (o *recOps) Create(a core.Agent) (core.Agent, error) { o.created = a; return a, nil }
func (o *recOps) Run(n string) error                      { o.ran = n; return nil }
func (o *recOps) Send(n, t string) error                  { o.sent = [2]string{n, t}; return nil }
func (o *recOps) Read(n string) ([]byte, error)           { o.readName = n; return o.readData, nil }
func (o *recOps) Kill(n string) error                     { o.killed = n; return nil }
func (o *recOps) Adopt(n string) error                    { o.adopted = n; return nil }

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

func TestRunKeyDispatchesRunCmd(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("job")}}
	m := loaded(o)
	_, cmd := stepRune(m, 'g')
	if cmd == nil {
		t.Fatal("g should return a run command")
	}
	msg := cmd() // execute the async cmd
	if o.ran != "job" {
		t.Errorf("Run called with %q, want job", o.ran)
	}
	if _, ok := msg.(statusMsg); !ok {
		t.Errorf("run cmd should yield a statusMsg, got %T", msg)
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

func TestEnterReadsTranscriptAndOpensPager(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("job")}, readData: []byte("l1\r\nl2\r\n")}
	m := loaded(o)
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("enter should return a read command")
	}
	msg := cmd()
	tm, ok := msg.(transcriptMsg)
	if !ok || tm.name != "job" {
		t.Fatalf("read cmd should yield transcriptMsg for job, got %#v", msg)
	}
	mm, _ = m.Update(tm)
	m = mm.(Model)
	if m.mode != modePager {
		t.Errorf("transcriptMsg should open the pager, mode=%d", m.mode)
	}
	// CRLF must be normalized.
	for _, ln := range m.pager.lines {
		if len(ln) > 0 && ln[len(ln)-1] == '\r' {
			t.Errorf("pager line has stray CR: %q", ln)
		}
	}
}

func TestCreateFlowCollectsRepoThenTask(t *testing.T) {
	// The name is auto-derived by the engine, so the flow only asks repo → task.
	o := &recOps{}
	m := loaded(o)
	m, _ = stepRune(m, 'n') // open create
	if m.mode != modeInput {
		t.Fatal("n should open the input overlay")
	}
	if m.input.stage != stageRepo {
		t.Fatalf("create should start on the repo stage, got %d", m.input.stage)
	}
	typeStr := func(m Model, s string) Model {
		for _, r := range s {
			m, _ = stepRune(m, r)
		}
		return m
	}
	enter := func(m Model) (Model, tea.Cmd) {
		mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return mm.(Model), cmd
	}
	m = typeStr(m, "app")
	m, _ = enter(m) // repo → task stage
	m = typeStr(m, "do the thing")
	m, cmd := enter(m) // task → submit
	if cmd == nil {
		t.Fatal("final enter should dispatch a create command")
	}
	cmd()
	if o.created.Repo != "app" || o.created.Task != "do the thing" {
		t.Errorf("created spec wrong: %+v", o.created)
	}
	if o.created.Name != "" {
		t.Errorf("UI should not set a name (engine derives it), got %q", o.created.Name)
	}
	if m.mode != modeList {
		t.Errorf("create should return to the list, mode=%d", m.mode)
	}
}

func TestCreateEmptyRepoAdvancesToTask(t *testing.T) {
	// Repo is optional: pressing enter on a blank repo advances to the task stage.
	o := &recOps{}
	m := loaded(o)
	m, _ = stepRune(m, 'n')
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // empty repo
	m = mm.(Model)
	if cmd != nil {
		t.Error("empty repo should not dispatch, just advance")
	}
	if m.mode != modeInput || m.input.stage != stageTask {
		t.Errorf("empty repo should advance to the task stage, got mode=%d stage=%d", m.mode, m.input.stage)
	}
}

func TestCreateEmptyTaskStays(t *testing.T) {
	o := &recOps{}
	m := loaded(o)
	m, _ = stepRune(m, 'n')
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // blank repo → task stage
	m = mm.(Model)
	mm2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // empty task
	m = mm2.(Model)
	if cmd != nil {
		t.Error("empty task should not dispatch")
	}
	if m.mode != modeInput || m.input.stage != stageTask {
		t.Errorf("empty task should stay on the task stage")
	}
}

func TestTabCyclesLens(t *testing.T) {
	o := &recOps{agents: []core.Agent{remote("a")}}
	m := loaded(o)
	// Default is combined (so local + remote agents are visible on open).
	if m.view != viewCombined {
		t.Fatalf("initial view = %v, want combined", m.view)
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(Model)
	if m.view != viewProject {
		t.Errorf("tab from combined should advance to project, got %v", m.view)
	}
}

func TestQuitKey(t *testing.T) {
	o := &recOps{}
	m := loaded(o)
	_, cmd := stepRune(m, 'q')
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
	_, cmd := stepRune(m, 'A') // managed agent → no adopt
	if cmd != nil {
		t.Error("adopt on a managed agent should do nothing")
	}
	o2 := &recOps{agents: []core.Agent{{Name: "wild", Where: core.Remote, State: core.Running, Origin: core.Foreign}}}
	m2 := loaded(o2)
	_, cmd2 := stepRune(m2, 'A')
	if cmd2 == nil {
		t.Fatal("adopt on a foreign agent should dispatch")
	}
	cmd2()
	if o2.adopted != "wild" {
		t.Errorf("Adopt called with %q, want wild", o2.adopted)
	}
}
