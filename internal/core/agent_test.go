package core

import (
	"encoding/json"
	"testing"
)

func TestAgentJSONRoundTrip(t *testing.T) {
	a := Agent{
		Name:    "fix-auth",
		Repo:    "git@github.com:me/app.git",
		Dir:     "/home/me/workplace/app",
		Task:    "fix the login bug",
		Session: "55a63641-2b5e-413e-bd07-00a74bbc1dfc",
		Where:   Remote,
		State:   Running,
		Origin:  Managed,
		Sync:    Behind,
		RunAt:   "2026-06-30T12:00:00Z",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Agent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != a {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, a)
	}
}

func TestIsHeld(t *testing.T) {
	held := Agent{State: Held}
	if !held.IsHeld() {
		t.Errorf("State=Held: IsHeld()=false, want true")
	}
	run := Agent{State: Running}
	if run.IsHeld() {
		t.Errorf("State=Running: IsHeld()=true, want false")
	}
}

func TestIsForeign(t *testing.T) {
	f := Agent{Origin: Foreign}
	if !f.IsForeign() {
		t.Errorf("Origin=Foreign: IsForeign()=false, want true")
	}
	m := Agent{Origin: Managed}
	if m.IsForeign() {
		t.Errorf("Origin=Managed: IsForeign()=true, want false")
	}
}
