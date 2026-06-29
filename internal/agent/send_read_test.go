package agent

import (
	"bytes"
	"os"
	"path/filepath"
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
