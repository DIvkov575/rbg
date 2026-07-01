package engine

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// newTestEngine builds an Engine with a real temp-dir store and injected fake
// capabilities, for operation tests.
func newTestEngine(t *testing.T, local, remote machine) *Engine {
	t.Helper()
	store, err := core.LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	return &Engine{store: store, local: local, remote: remote}
}

func findAgent(t *testing.T, agents []core.Agent, name string) core.Agent {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("agent %q not in %+v", name, agents)
	return core.Agent{}
}

func TestListReconcilesStoreWithBothMachines(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "L1", Name: "loc", Cwd: "/x", State: "working"},
		}}},
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "R1", Name: "rem", Cwd: "/y", State: "done"},
		}}},
	)
	e.store.Add(core.Agent{Name: "held", Repo: "r", Task: "t", State: core.Held, Origin: core.Managed})

	agents, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(agents), agents)
	}
	if findAgent(t, agents, "loc").Where != core.Local {
		t.Errorf("loc should be Local")
	}
	if findAgent(t, agents, "rem").Where != core.Remote {
		t.Errorf("rem should be Remote")
	}
	if findAgent(t, agents, "held").State != core.Held {
		t.Errorf("held record should survive reconcile")
	}
}

func TestListPropagatesDegradationError(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{err: errors.New("laptop probe failed")}},
		machine{Source: fakeSource{}},
	)
	_, err := e.List()
	if err == nil {
		t.Errorf("expected a degradation error when a source fails")
	}
}

func TestCreateStagesHeldRecord(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	got, err := e.Create(core.Agent{
		Name: "later", Repo: "git@github:me/app", Dir: "/home/me/app",
		Task: "refactor the parser", Where: core.Remote,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.State != core.Held || got.Origin != core.Managed {
		t.Errorf("created agent = %+v, want Held+Managed", got)
	}
	if got.Task != "refactor the parser" || got.Where != core.Remote {
		t.Errorf("created agent lost fields: %+v", got)
	}
	rec, ok := e.store.Get("later")
	if !ok || rec.State != core.Held {
		t.Errorf("held record not persisted: %+v ok=%v", rec, ok)
	}
}

func TestCreateRejectsBlankTaskAndName(t *testing.T) {
	e := newTestEngine(t, machine{}, machine{})
	if _, err := e.Create(core.Agent{Name: "x", Task: ""}); err == nil {
		t.Errorf("blank task should error (no blank agents)")
	}
	if _, err := e.Create(core.Agent{Name: "", Task: "t"}); err == nil {
		t.Errorf("blank name should error")
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	e := newTestEngine(t, machine{}, machine{})
	if _, err := e.Create(core.Agent{Name: "dup", Task: "t"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := e.Create(core.Agent{Name: "dup", Task: "u"}); err == nil {
		t.Errorf("duplicate name should error")
	}
}
