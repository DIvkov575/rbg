package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		NewID:      func() string { return "gen-sid-1" },
		Spawn:      func(name string, args []string, stdoutPath string) (int, error) { return 111, nil },
	}
}

func TestLaunch_RecordsSessionAndPrintsJSON(t *testing.T) {
	a := newAgent(t, &run.Recording{Default: run.Result{Code: 0}})
	var out bytes.Buffer
	code := a.Launch(&out, "alpha", "build it")
	if code != 0 {
		t.Fatalf("Launch code=%d out=%s", code, out.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	// id is the agent name; claudeSessionId is the client-generated UUID.
	if resp["id"] != "alpha" || resp["claudeSessionId"] != "gen-sid-1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestLaunch_SpawnFailureReported(t *testing.T) {
	a := newAgent(t, &run.Recording{Default: run.Result{Code: 0}})
	a.Spawn = func(name string, args []string, stdoutPath string) (int, error) {
		return 0, errSpawnTest
	}
	var out bytes.Buffer
	if code := a.Launch(&out, "alpha", "x"); code == 0 {
		t.Fatal("expected nonzero when spawn fails")
	}
	// must not record a session
	var ls bytes.Buffer
	a.Ls(&ls)
	if !bytes.Contains(ls.Bytes(), []byte("[]")) {
		t.Fatalf("expected no recorded session, ls = %s", ls.String())
	}
}

func TestLaunch_RecordsClientGeneratedID(t *testing.T) {
	a := newAgent(t, &run.Recording{Default: run.Result{Code: 0}})
	var out bytes.Buffer
	if code := a.Launch(&out, "alpha", "x"); code != 0 {
		t.Fatalf("Launch code=%d", code)
	}
	var ls bytes.Buffer
	a.Ls(&ls)
	if !bytes.Contains(ls.Bytes(), []byte("gen-sid-1")) {
		t.Fatalf("expected client-generated id recorded, ls = %s", ls.String())
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

func TestLaunch_DerivesNameWhenEmpty(t *testing.T) {
	a := newAgent(t, &run.Recording{Default: run.Result{Code: 0}})
	var out bytes.Buffer
	if code := a.Launch(&out, "", "fix the flaky test"); code != 0 {
		t.Fatalf("Launch code=%d out=%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("fix-flaky-test")) {
		t.Fatalf("expected derived name in output: %s", out.String())
	}
}

var errSpawnTest = fmt.Errorf("spawn boom")
