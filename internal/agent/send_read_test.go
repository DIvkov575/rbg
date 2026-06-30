package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// seed writes a session and a transcript file with the given lines.
func seed(t *testing.T, a *Agent, name, sid string, lines string) {
	t.Helper()
	store, _ := session.Load(a.StatePath)
	tp := a.transcriptPath(sid)
	os.MkdirAll(filepath.Dir(tp), 0o755)
	os.WriteFile(tp, []byte(lines), 0o600)
	store.Add(session.Session{Name: name, ClaudeSessionID: sid, TranscriptPath: tp})
	store.Save()
}

func TestSend_SpawnsChildAndReturnsOK(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	a := newAgent(t, r)
	seed(t, a, "alpha", "sid-1", "")
	spawned := false
	a.Spawn = func(name string, args []string, stdoutPath string) (int, error) {
		spawned = true
		return 4321, nil
	}
	var out bytes.Buffer
	if code := a.Send(&out, "alpha", "next step"); code != 0 {
		t.Fatalf("Send code=%d out=%s", code, out.String())
	}
	if !spawned {
		t.Fatal("expected child spawn")
	}
}

func TestSend_UnknownAgent(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	var out bytes.Buffer
	if code := a.Send(&out, "ghost", "x"); code != 1 {
		t.Fatalf("want 1 for unknown, got %d", code)
	}
}

func TestSend_BusyReturns3(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	seed(t, a, "alpha", "sid-1", "")
	// hold the lock to simulate an in-flight send
	lock, ok, _ := session.TryLock(a.lockPath("alpha"))
	if !ok {
		t.Fatal("could not pre-acquire lock")
	}
	defer lock.Unlock()
	var out bytes.Buffer
	if code := a.Send(&out, "alpha", "x"); code != 3 {
		t.Fatalf("want 3 (busy), got %d", code)
	}
}

func TestRead_StreamsRenderedTranscript(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	lines := `{"message":{"role":"user","content":"q"}}` + "\n" +
		`{"message":{"role":"assistant","content":"a"}}` + "\n"
	seed(t, a, "alpha", "sid-1", lines)
	var out bytes.Buffer
	if code := a.Read(&out, "alpha"); code != 0 {
		t.Fatalf("Read code=%d", code)
	}
	got := out.String()
	if got != "user: q\nassistant: a\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRead_UnknownAgent(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	var out bytes.Buffer
	if code := a.Read(&out, "ghost"); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
}

// Regression (real-host Task 15): the real `claude` writes transcripts to a
// project dir derived from its cwd (e.g. ~/.claude/projects/-local-home-divkov/),
// NOT the hardcoded "sim-project". Read must locate the transcript by globbing
// the unique session id, regardless of which project dir claude chose.
func TestRead_FindsTranscriptInRealProjectDir(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	sid := "dc4818dc-b1af-4c0e-8897-d4e223ccf6b6"
	// Write the transcript in a cwd-derived dir that is NOT "sim-project".
	realDir := filepath.Join(a.ClaudeHome, ".claude", "projects", "-local-home-divkov")
	os.MkdirAll(realDir, 0o755)
	lines := `{"message":{"role":"assistant","content":"PONG"}}` + "\n"
	os.WriteFile(filepath.Join(realDir, sid+".jsonl"), []byte(lines), 0o600)
	// Store the session with a WRONG TranscriptPath (the old sim-project bug).
	store, _ := session.Load(a.StatePath)
	store.Add(session.Session{
		Name:            "pong",
		ClaudeSessionID: sid,
		TranscriptPath:  filepath.Join(a.ClaudeHome, ".claude", "projects", "sim-project", sid+".jsonl"),
	})
	store.Save()

	var out bytes.Buffer
	if code := a.Read(&out, "pong"); code != 0 {
		t.Fatalf("Read code=%d", code)
	}
	if !bytes.Contains(out.Bytes(), []byte("assistant: PONG")) {
		t.Fatalf("Read did not find real-dir transcript; got %q", out.String())
	}
}

// Regression (real-host Task 15): real claude maintains its OWN transcript on
// disk; the headless child's stdout must NOT be redirected at the transcript
// path (which the agent can't predict and may not exist). It should go to a log
// the agent owns under its state dir.
func TestSend_SpawnUsesAgentOwnedLogPath(t *testing.T) {
	a := newAgent(t, &run.Recording{Default: run.Result{Code: 0}})
	seed(t, a, "alpha", "sid-1", "")
	var gotPath string
	a.Spawn = func(name string, args []string, stdoutPath string) (int, error) {
		gotPath = stdoutPath
		return 1234, nil
	}
	var out bytes.Buffer
	if code := a.Send(&out, "alpha", "go"); code != 0 {
		t.Fatalf("Send code=%d out=%s", code, out.String())
	}
	stateDir := filepath.Dir(a.StatePath)
	if !strings.HasPrefix(gotPath, stateDir) {
		t.Fatalf("spawn stdout path %q should be under agent state dir %q", gotPath, stateDir)
	}
	if strings.Contains(gotPath, "projects") {
		t.Fatalf("spawn must not target the claude transcript path, got %q", gotPath)
	}
}
