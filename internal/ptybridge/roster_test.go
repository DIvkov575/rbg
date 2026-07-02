package ptybridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindWorkerResolvesByShortAndFullID(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "daemon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	roster := `{"workers":{"4871b31a":{"pid":8616,` +
		`"sessionId":"4871b31a-d236-40ac-8a51-a222b8b7b025",` +
		`"ptySock":"/tmp/x.pty.sock","cwd":"/w/scratch"}}}`
	if err := os.WriteFile(RosterPath(home), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}

	// Full uuid resolves.
	w, err := FindWorker(home, "4871b31a-d236-40ac-8a51-a222b8b7b025")
	if err != nil {
		t.Fatalf("full id: %v", err)
	}
	if w.PtySock != "/tmp/x.pty.sock" {
		t.Errorf("ptySock = %q", w.PtySock)
	}
	// Short id resolves the same worker.
	w2, err := FindWorker(home, "4871b31a")
	if err != nil {
		t.Fatalf("short id: %v", err)
	}
	if w2.SessionID != w.SessionID {
		t.Errorf("short id resolved to %q, want %q", w2.SessionID, w.SessionID)
	}
}

func TestFindWorkerUnknownSessionErrors(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "daemon")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(RosterPath(home), []byte(`{"workers":{}}`), 0o600)

	_, err := FindWorker(home, "deadbeef")
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestFindWorkerNoRosterErrors(t *testing.T) {
	_, err := FindWorker(t.TempDir(), "anything")
	if err == nil {
		t.Fatal("expected error when roster is absent")
	}
}
