package uitea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/divkov575/rbg/internal/core"
)

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
}

func (o *recOps) List() ([]core.Agent, error)             { return o.agents, nil }
func (o *recOps) Create(a core.Agent) (core.Agent, error) { o.created = a; return a, nil }
func (o *recOps) Run(n string) error                      { o.ran = n; return nil }
func (o *recOps) Send(n, t string) error                  { o.sent = [2]string{n, t}; return nil }
func (o *recOps) Read(n string) ([]byte, error)           { o.readName = n; return o.readData, nil }
func (o *recOps) Kill(n string) error                     { o.killed = n; return nil }
func (o *recOps) Adopt(n string) error                    { o.adopted = n; return nil }
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

func TestCreateFlowPicksProjectThenTask(t *testing.T) {
	// n opens the project picker; choosing a project then asks for the task; the
	// engine auto-derives the name. Picker index 0 is "(no repo)", index 1 is the
	// first suggestion.
	o := &recOps{projects: []core.Project{{Label: "app (local)", Repo: "/w/app", Origin: "local"}}}
	m := loaded(o)
	m, _ = stepRune(m, 'n')
	if m.mode != modePicker {
		t.Fatalf("n should open the project picker, mode=%d", m.mode)
	}
	// move to the first real suggestion (index 1) and choose it
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	m, _ = enterKey(m)
	if m.mode != modeInput {
		t.Fatalf("choosing a project should advance to the task input, mode=%d", m.mode)
	}
	m = typeStr(m, "do the thing")
	m, cmd := enterKey(m)
	if cmd == nil {
		t.Fatal("final enter should dispatch a create command")
	}
	cmd()
	if o.created.Repo != "/w/app" || o.created.Task != "do the thing" {
		t.Errorf("created spec wrong: %+v", o.created)
	}
	if o.created.Name != "" {
		t.Errorf("UI should not set a name (engine derives it), got %q", o.created.Name)
	}
	if m.mode != modeList {
		t.Errorf("create should return to the list, mode=%d", m.mode)
	}
}

func TestCreateNoRepoOption(t *testing.T) {
	// Choosing the first picker entry ("no repo") yields a repo-less create.
	o := &recOps{projects: []core.Project{{Label: "app (local)", Repo: "/w/app"}}}
	m := loaded(o)
	m, _ = stepRune(m, 'n')
	m, _ = enterKey(m) // cursor at 0 = "(no repo)"
	if m.mode != modeInput {
		t.Fatalf("no-repo choice should advance to the task input, mode=%d", m.mode)
	}
	m = typeStr(m, "just a task")
	m, cmd := enterKey(m)
	cmd()
	if o.created.Repo != "" || o.created.Task != "just a task" {
		t.Errorf("no-repo create wrong: %+v", o.created)
	}
}

func TestPickerFilters(t *testing.T) {
	o := &recOps{projects: []core.Project{
		{Label: "alpha (local)", Repo: "/w/alpha"},
		{Label: "beta (github)", Repo: "me/beta"},
	}}
	m := loaded(o)
	m, _ = stepRune(m, 'n')
	m = typeStr(m, "beta") // filter
	matches := m.picker.matches()
	// "(no repo)" always matches + "beta" → 2
	if len(matches) != 2 {
		t.Fatalf("filter 'beta' should match no-repo + beta, got %d: %+v", len(matches), matches)
	}
	m, _ = enterKey(m) // cursor 0 = no-repo (still first); choose beta explicitly
	// after filtering, choose the beta row: move down then enter
	// (re-open to test choosing the filtered suggestion)
	m2 := loaded(o)
	m2, _ = stepRune(m2, 'n')
	m2 = typeStr(m2, "beta")
	mm, _ := m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 = mm.(Model)
	m2, _ = enterKey(m2)
	m2 = typeStr(m2, "t")
	_, cmd := enterKey(m2)
	cmd()
	if o.created.Repo != "me/beta" {
		t.Errorf("filtered choice should pick me/beta, got %q", o.created.Repo)
	}
}

func TestCreateEmptyTaskStays(t *testing.T) {
	o := &recOps{}
	m := loaded(o)
	m, _ = stepRune(m, 'n')     // picker
	m, _ = enterKey(m)          // choose no-repo → task input
	mm, cmd := enterKey(m)      // empty task
	m = mm
	if cmd != nil {
		t.Error("empty task should not dispatch")
	}
	if m.mode != modeInput {
		t.Errorf("empty task should stay on the task input, mode=%d", m.mode)
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
