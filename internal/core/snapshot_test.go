package core

import (
	"encoding/json"
	"testing"
)

// Real payload shape verified from `claude agents --json --all` (claude v2.1.197).
const sampleAgentsJSON = `[
  {"id":"55a63641","cwd":"/home/me/app","kind":"background","startedAt":1782395439347,
   "sessionId":"55a63641-2b5e-413e-bd07-00a74bbc1dfc","name":"analyze","state":"done"},
  {"pid":70515,"id":"48fd50b3","cwd":"/home/me/svc","kind":"background","startedAt":1782840532214,
   "sessionId":"48fd50b3-9f01-4320-93ba-290d1c7c65a3","name":"init","status":"busy","state":"working"}
]`

func TestParseLive(t *testing.T) {
	var live []Live
	if err := json.Unmarshal([]byte(sampleAgentsJSON), &live); err != nil {
		t.Fatalf("unmarshal claude agents payload: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if live[0].SessionID != "55a63641-2b5e-413e-bd07-00a74bbc1dfc" {
		t.Errorf("SessionID = %q", live[0].SessionID)
	}
	if live[0].Cwd != "/home/me/app" {
		t.Errorf("Cwd = %q", live[0].Cwd)
	}
	if live[0].Name != "analyze" {
		t.Errorf("Name = %q", live[0].Name)
	}
	if live[0].StartedAt != 1782395439347 {
		t.Errorf("StartedAt = %d", live[0].StartedAt)
	}
	if live[1].State != "working" {
		t.Errorf("State = %q", live[1].State)
	}
}

func TestLifecycleFromState(t *testing.T) {
	cases := map[string]Lifecycle{
		"working": Running,
		"idle":    Running,
		"done":    Done,
		"":        Done, // unknown/empty → treat as finished, never Held
		"weird":   Done,
	}
	for state, want := range cases {
		if got := LifecycleFromState(state); got != want {
			t.Errorf("LifecycleFromState(%q) = %q, want %q", state, got, want)
		}
	}
}
