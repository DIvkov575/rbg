package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// fakeSource is a canned AgentSource for Inventory tests.
type fakeSource struct {
	live []core.Live
	err  error
}

func (f fakeSource) List() ([]core.Live, error) { return f.live, f.err }

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

func TestInventoryNoErrorWhenBothSucceed(t *testing.T) {
	agents, err := Inventory(nil, fakeSource{}, fakeSource{})
	if err != nil {
		t.Errorf("both sources ok → err should be nil, got %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("empty everything → empty inventory, got %+v", agents)
	}
}
