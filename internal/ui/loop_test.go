package ui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// fakeOps records calls and returns canned data.
type fakeOps struct {
	agents   []core.Agent
	listErr  error
	ran      string
	sent     [2]string
	killed   string
	adopted  string
	created  core.Agent
	readName string
	readData []byte
}

func (f *fakeOps) List() ([]core.Agent, error)             { return f.agents, f.listErr }
func (f *fakeOps) Create(a core.Agent) (core.Agent, error) { f.created = a; return a, nil }
func (f *fakeOps) Run(name string) error                   { f.ran = name; return nil }
func (f *fakeOps) Send(name, task string) error            { f.sent = [2]string{name, task}; return nil }
func (f *fakeOps) Read(name string) ([]byte, error)        { f.readName = name; return f.readData, nil }
func (f *fakeOps) Kill(name string) error                  { f.killed = name; return nil }
func (f *fakeOps) Adopt(name string) error                 { f.adopted = name; return nil }

func TestApplyActionQuitReturnsTrue(t *testing.T) {
	m := NewModel(nil)
	if quit := applyAction(m, &fakeOps{}, Action{Kind: ActQuit}); !quit {
		t.Error("ActQuit should signal quit")
	}
}

func TestApplyActionRunCallsEngineAndRefreshes(t *testing.T) {
	ops := &fakeOps{agents: []core.Agent{{Name: "after", Where: core.Remote}}}
	m := NewModel([]core.Agent{{Name: "before", Where: core.Remote}})
	quit := applyAction(m, ops, Action{Kind: ActRun, Name: "job"})
	if quit {
		t.Error("ActRun should not quit")
	}
	if ops.ran != "job" {
		t.Errorf("Run called with %q, want job", ops.ran)
	}
	if len(m.Agents) != 1 || m.Agents[0].Name != "after" {
		t.Errorf("inventory not refreshed after Run: %+v", m.Agents)
	}
}

func TestApplyActionReadPushesPager(t *testing.T) {
	ops := &fakeOps{readData: []byte("line1\nline2")}
	m := NewModel(nil)
	applyAction(m, ops, Action{Kind: ActRead, Name: "foo"})
	if ops.readName != "foo" {
		t.Errorf("Read called with %q, want foo", ops.readName)
	}
	if _, ok := m.Top().(*pagerScreen); !ok {
		t.Errorf("ActRead should push a pager, top is %T", m.Top())
	}
}

func TestApplyActionReadNormalizesCRLF(t *testing.T) {
	ops := &fakeOps{readData: []byte("alpha\r\nbeta\r\n")}
	m := NewModel(nil)
	applyAction(m, ops, Action{Kind: ActRead, Name: "foo"})
	p, ok := m.Top().(*pagerScreen)
	if !ok {
		t.Fatalf("expected pager on top, got %T", m.Top())
	}
	for i, ln := range p.lines {
		if strings.Contains(ln, "\r") {
			t.Errorf("line %d still has a carriage return: %q", i, ln)
		}
	}
	if len(p.lines) != 2 || p.lines[0] != "alpha" || p.lines[1] != "beta" {
		t.Errorf("lines = %#v, want [alpha beta]", p.lines)
	}
}

func TestApplyActionSendKillAdoptCreate(t *testing.T) {
	ops := &fakeOps{}
	m := NewModel(nil)
	applyAction(m, ops, Action{Kind: ActSend, Name: "n", Task: "t"})
	if ops.sent != [2]string{"n", "t"} {
		t.Errorf("Send got %v", ops.sent)
	}
	applyAction(m, ops, Action{Kind: ActKill, Name: "k"})
	if ops.killed != "k" {
		t.Errorf("Kill got %q", ops.killed)
	}
	applyAction(m, ops, Action{Kind: ActAdopt, Name: "a"})
	if ops.adopted != "a" {
		t.Errorf("Adopt got %q", ops.adopted)
	}
	applyAction(m, ops, Action{Kind: ActCreate, Spec: core.Agent{Task: "do"}})
	if ops.created.Task != "do" {
		t.Errorf("Create got %+v", ops.created)
	}
}

func TestApplyActionSetsErrorStatusOnFailure(t *testing.T) {
	failing := &failOps{}
	m := NewModel(nil)
	applyAction(m, failing, Action{Kind: ActRun, Name: "x"})
	if m.Status == "" {
		t.Error("a failed action should set a status message")
	}
}

// failOps returns an error from every mutating call.
type failOps struct{ fakeOps }

func (f *failOps) Run(name string) error { return errBoom }

var errBoom = errStr("boom")

type errStr string

func (e errStr) Error() string { return string(e) }
