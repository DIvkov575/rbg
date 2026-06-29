package agent

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/run"
)

func newAgent(t *testing.T, r run.Runner) *Agent {
	t.Helper()
	dir := t.TempDir()
	return &Agent{
		Runner:     r,
		StatePath:  filepath.Join(dir, "sessions.json"),
		ClaudeHome: dir, // transcripts rooted here
		Now:        func() string { return "2026-06-28T00:00:00Z" },
	}
}

func TestLaunch_RecordsSessionAndPrintsJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"agents": {Stdout: []byte(`[{"name":"alpha","sessionId":"sid-1"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	code := a.Launch(&out, "alpha", "build it")
	if code != 0 {
		t.Fatalf("Launch code=%d", code)
	}
	var resp map[string]string
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if resp["id"] != "alpha" || resp["claudeSessionId"] != "sid-1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestLaunch_UnresolvedIDErrors(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"agents": {Stdout: []byte("[]")}},
		Default:     run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	if code := a.Launch(&out, "alpha", "x"); code == 0 {
		t.Fatal("expected nonzero when id unresolved")
	}
}

func TestLaunch_BGFailureReported(t *testing.T) {
	// agents-list WOULD resolve the id; only the --bg failure should abort.
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"--bg":   {Code: 1},
			"agents": {Stdout: []byte(`[{"name":"alpha","sessionId":"sid-1"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	if code := a.Launch(&out, "alpha", "x"); code != 1 {
		t.Fatalf("expected 1 when --bg fails, got %d", code)
	}
	// must not record a session
	var ls bytes.Buffer
	a.Ls(&ls)
	if !bytes.Contains(ls.Bytes(), []byte("[]")) {
		t.Fatalf("expected no recorded session, ls = %s", ls.String())
	}
}

func TestLs_PrintsRecordedSessions(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	a := newAgent(t, r)
	// seed two via Launch using a runner that resolves ids
	a.Runner = &run.Recording{BySubstring: map[string]run.Result{
		"agents": {Stdout: []byte(`[{"name":"alpha","sessionId":"sid-1"},{"name":"beta","sessionId":"sid-2"}]`)},
	}}
	a.Launch(&bytes.Buffer{}, "alpha", "one")
	a.Launch(&bytes.Buffer{}, "beta", "two")

	var out bytes.Buffer
	if code := a.Ls(&out); code != 0 {
		t.Fatalf("Ls code=%d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if len(list) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(list))
	}
}
