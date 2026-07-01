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

func TestReadDispatchesToAgentsMachine(t *testing.T) {
	var localAsked, remoteAsked string
	e := newTestEngine(t,
		machine{
			Source: fakeSource{live: []core.Live{{SessionID: "L1", Name: "loc", Cwd: "/x", State: "working"}}},
			Tx:     fakeTx{data: []byte("local transcript"), gotSession: &localAsked},
		},
		machine{
			Source: fakeSource{live: []core.Live{{SessionID: "R1", Name: "rem", Cwd: "/y", State: "done"}}},
			Tx:     fakeTx{data: []byte("remote transcript"), gotSession: &remoteAsked},
		},
	)

	data, err := e.Read("rem")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "remote transcript" {
		t.Errorf("Read = %q, want the remote transcript", data)
	}
	if remoteAsked != "R1" {
		t.Errorf("remote Tx asked for session %q, want R1", remoteAsked)
	}
	if localAsked != "" {
		t.Errorf("local Tx should not have been asked, got %q", localAsked)
	}
}

func TestReadUnknownAgentErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if _, err := e.Read("ghost"); err == nil {
		t.Errorf("reading an unknown agent should error")
	}
}

func TestReadAgentWithoutSessionErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "held", Task: "t", State: core.Held, Origin: core.Managed})
	if _, err := e.Read("held"); err == nil {
		t.Errorf("reading a never-run agent should error")
	}
}

func TestAdoptPersistsForeignAsManaged(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{}},
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "R1", Name: "wild", Cwd: "/srv/app", State: "working"},
		}}},
	)
	if err := e.Adopt("wild"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	rec, ok := e.store.Get("wild")
	if !ok {
		t.Fatalf("adopted agent not in store")
	}
	if rec.Origin != core.Managed {
		t.Errorf("adopted Origin = %q, want managed", rec.Origin)
	}
	if rec.Session != "R1" || rec.Dir != "/srv/app" {
		t.Errorf("adopted record lost live identity: %+v", rec)
	}
}

func TestAdoptNonForeignErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "mine", Task: "t", State: core.Held, Origin: core.Managed})
	if err := e.Adopt("mine"); err == nil {
		t.Errorf("adopting an already-managed agent should error")
	}
}

func TestAdoptUnknownErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Adopt("ghost"); err == nil {
		t.Errorf("adopting an unknown agent should error")
	}
}

func TestReadSucceedsWhenOtherMachineDegraded(t *testing.T) {
	// The remote probe fails, but the requested agent lives locally. find/Read
	// must resolve it from the still-usable inventory and NOT surface the
	// remote's degradation error, since the agent was found.
	e := newTestEngine(t,
		machine{
			Source: fakeSource{live: []core.Live{{SessionID: "L1", Name: "loc", Cwd: "/x", State: "working"}}},
			Tx:     fakeTx{data: []byte("local transcript")},
		},
		machine{Source: fakeSource{err: errors.New("desktop unreachable")}},
	)
	data, err := e.Read("loc")
	if err != nil {
		t.Fatalf("Read should succeed for a found local agent despite remote degradation: %v", err)
	}
	if string(data) != "local transcript" {
		t.Errorf("Read = %q, want the local transcript", data)
	}
}
