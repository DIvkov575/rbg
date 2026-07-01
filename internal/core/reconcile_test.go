package core

import (
	"testing"
)

// find returns the agent with the given Name, or fails the test.
func find(t *testing.T, agents []Agent, name string) Agent {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no agent named %q in %+v", name, agents)
	return Agent{}
}

func TestReconcileHeldRecordPassesThrough(t *testing.T) {
	// A held record has no live counterpart; it must survive reconcile unchanged.
	records := []Agent{{Name: "later", Repo: "r", Task: "do it", State: Held, Origin: Managed}}
	got := Reconcile(records, nil, nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	a := find(t, got, "later")
	if a.State != Held || a.Origin != Managed || a.Task != "do it" {
		t.Errorf("held record mangled: %+v", a)
	}
}

func TestReconcileMatchesRecordToLiveBySession(t *testing.T) {
	// A managed record that has run gets its live State/Where from the snapshot,
	// while keeping its Repo/Task/Name from the record.
	records := []Agent{{
		Name: "fix", Repo: "git@github:me/app", Task: "fix bug",
		Session: "sid-1", State: Running, Origin: Managed, Where: Remote,
	}}
	remote := []Live{{SessionID: "sid-1", Name: "fix", Cwd: "/home/me/app", State: "done", StartedAt: 100}}
	got := Reconcile(records, nil, remote)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	a := find(t, got, "fix")
	if a.State != Done { // live says done → record's Running is updated
		t.Errorf("State = %q, want done (from live)", a.State)
	}
	if a.Where != Remote {
		t.Errorf("Where = %q, want remote (live came from remote host)", a.Where)
	}
	if a.Repo != "git@github:me/app" || a.Task != "fix bug" {
		t.Errorf("record identity lost: Repo=%q Task=%q", a.Repo, a.Task)
	}
	if a.Origin != Managed {
		t.Errorf("Origin = %q, want managed", a.Origin)
	}
}

func TestReconcileForeignLocalAndRemote(t *testing.T) {
	// Live agents with no matching record are Foreign, tagged with the Where of
	// the host they were observed on, and given Name/Dir from the live entry.
	local := []Live{{SessionID: "L1", Name: "loc job", Cwd: "/home/me/x", State: "working", StartedAt: 1}}
	remote := []Live{{SessionID: "R1", Name: "rem job", Cwd: "/srv/y", State: "done", StartedAt: 2}}
	got := Reconcile(nil, local, remote)
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2", len(got))
	}
	l := find(t, got, "loc job")
	if l.Origin != Foreign || l.Where != Local || l.State != Running || l.Dir != "/home/me/x" || l.Session != "L1" {
		t.Errorf("local foreign wrong: %+v", l)
	}
	r := find(t, got, "rem job")
	if r.Origin != Foreign || r.Where != Remote || r.State != Done || r.Dir != "/srv/y" || r.Session != "R1" {
		t.Errorf("remote foreign wrong: %+v", r)
	}
}

func TestReconcileRecordWithoutSessionNotMatchedToForeign(t *testing.T) {
	// A held record (empty Session) must NOT accidentally match a live agent that
	// also happens to have no session in a snapshot; empty-session never matches.
	records := []Agent{{Name: "held", Repo: "r", Task: "t", State: Held, Origin: Managed}}
	local := []Live{{SessionID: "", Name: "ghost", Cwd: "/tmp", State: "done", StartedAt: 5}}
	got := Reconcile(records, local, nil)
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2 (held + foreign ghost)", len(got))
	}
	h := find(t, got, "held")
	if h.Origin != Managed || h.State != Held {
		t.Errorf("held record changed: %+v", h)
	}
	g := find(t, got, "ghost")
	if g.Origin != Foreign {
		t.Errorf("ghost should be foreign: %+v", g)
	}
}

func TestReconcileResultIsSortedByName(t *testing.T) {
	records := []Agent{
		{Name: "zebra", State: Held, Origin: Managed, Task: "z"},
		{Name: "alpha", State: Held, Origin: Managed, Task: "a"},
	}
	got := Reconcile(records, nil, nil)
	if got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Errorf("not sorted by name: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestReconcileManagedRecordWithNoLiveMatchPassesThrough(t *testing.T) {
	// A managed record that has run (non-empty Session) but whose session is not
	// in any live snapshot (agent dropped off `claude agents`, or host down) must
	// pass through unchanged — not be lost, reset, or reclassified as foreign.
	records := []Agent{{
		Name: "gone", Repo: "r", Task: "t", Session: "sid-x",
		State: Running, Origin: Managed, Where: Remote,
	}}
	got := Reconcile(records, nil, nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	a := find(t, got, "gone")
	if a.State != Running || a.Where != Remote || a.Origin != Managed || a.Session != "sid-x" {
		t.Errorf("unmatched managed record changed: %+v", a)
	}
}

func TestAdoptFlipsForeignToManaged(t *testing.T) {
	f := Agent{Name: "x", Origin: Foreign, State: Done, Session: "s"}
	got := Adopt(f)
	if got.Origin != Managed {
		t.Errorf("Adopt did not flip Origin: %+v", got)
	}
	if got.Session != "s" || got.Name != "x" {
		t.Errorf("Adopt altered identity: %+v", got)
	}
}
