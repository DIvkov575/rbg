package host

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/divkov575/rbg/internal/core"
)

// fakeSource is a canned AgentSource for Inventory tests.
type fakeSource struct {
	live []core.Live
	err  error
}

func (f fakeSource) List() ([]core.Live, error) { return f.live, f.err }

// gateSource blocks List() until release is closed, and records when it started,
// so a test can prove both sources run concurrently rather than in series.
type gateSource struct {
	live    []core.Live
	release <-chan struct{}
	started chan struct{}
	once    sync.Once
}

func (g *gateSource) List() ([]core.Live, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	return g.live, nil
}

func TestInventoryReconcilesRecordsWithBothMachines(t *testing.T) {
	records := []core.Agent{
		{Name: "held", Repo: "r", Task: "t", State: core.Held, Origin: core.Managed},
	}
	local := fakeSource{live: []core.Live{
		{SessionID: "L1", Name: "loc", Cwd: "/home/me/x", State: "working"},
	}}
	remote := fakeSource{live: []core.Live{
		{SessionID: "R1", Name: "rem", Cwd: "/srv/y", State: "done"},
	}}

	agents, err := Inventory(records, local, remote)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	// held record + local foreign + remote foreign = 3
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(agents), agents)
	}
	byName := map[string]core.Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	if byName["loc"].Where != core.Local || byName["loc"].Origin != core.Foreign {
		t.Errorf("local foreign wrong: %+v", byName["loc"])
	}
	if byName["rem"].Where != core.Remote || byName["rem"].Origin != core.Foreign {
		t.Errorf("remote foreign wrong: %+v", byName["rem"])
	}
	if byName["held"].State != core.Held {
		t.Errorf("held record lost: %+v", byName["held"])
	}
}

func TestInventoryDegradesWhenRemoteFails(t *testing.T) {
	records := []core.Agent{{Name: "keep", State: core.Held, Origin: core.Managed, Task: "t"}}
	local := fakeSource{live: []core.Live{{SessionID: "L1", Name: "loc", Cwd: "/x", State: "idle"}}}
	remote := fakeSource{err: errors.New("host down")}

	agents, err := Inventory(records, local, remote)
	if err == nil {
		t.Errorf("expected a non-nil error signaling remote failure")
	}
	// Still returns the usable inventory from records + local.
	names := map[string]bool{}
	for _, a := range agents {
		names[a.Name] = true
	}
	if !names["keep"] || !names["loc"] {
		t.Errorf("degraded inventory should still contain keep + loc, got %+v", agents)
	}
}

func TestInventoryBothFailStillReturnsRecords(t *testing.T) {
	// Total failure: both machines unreachable. The records-only inventory must
	// still come back (usable), and the error must aggregate BOTH failures.
	records := []core.Agent{{Name: "keep", State: core.Held, Origin: core.Managed, Task: "t"}}
	local := fakeSource{err: errors.New("local down")}
	remote := fakeSource{err: errors.New("remote down")}

	agents, err := Inventory(records, local, remote)
	if err == nil {
		t.Fatalf("expected a non-nil error when both sources fail")
	}
	// errors.Join renders one error per line — both failures must be present.
	msg := err.Error()
	if !strings.Contains(msg, "local down") || !strings.Contains(msg, "remote down") {
		t.Errorf("joined error missing a source failure: %q", msg)
	}
	if len(agents) != 1 || agents[0].Name != "keep" {
		t.Errorf("records-only inventory should survive total failure, got %+v", agents)
	}
}

func TestInventoryProbesSourcesConcurrently(t *testing.T) {
	// Both sources must be in-flight at the same time. Each gate signals when its
	// List() begins; if the two ran serially, the second would never start until
	// the first returned, and we release only after seeing BOTH start.
	release := make(chan struct{})
	local := &gateSource{live: []core.Live{{SessionID: "L", Name: "l", State: "idle"}}, release: release, started: make(chan struct{})}
	remote := &gateSource{live: []core.Live{{SessionID: "R", Name: "r", State: "idle"}}, release: release, started: make(chan struct{})}

	done := make(chan []core.Agent, 1)
	go func() {
		agents, _ := Inventory(nil, local, remote)
		done <- agents
	}()

	// Both List() calls must begin before either is allowed to return.
	for _, s := range []*gateSource{local, remote} {
		select {
		case <-s.started:
		case <-time.After(2 * time.Second):
			t.Fatal("a source never started — Inventory is not probing concurrently")
		}
	}
	close(release)

	select {
	case agents := <-done:
		if len(agents) != 2 {
			t.Errorf("got %d agents, want 2", len(agents))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Inventory did not complete after release")
	}
}

func TestInventoryNoErrorWhenBothSucceed(t *testing.T) {
	agents, err := Inventory(nil, fakeSource{}, fakeSource{})
	if err != nil {
		t.Errorf("both sources ok → err should be nil, got %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("empty everything → empty inventory, got %+v", agents)
	}
}
