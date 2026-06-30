package session

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sessions) != 0 {
		t.Fatalf("expected empty, got %+v", s.Sessions)
	}
}

func TestAddSaveLoadRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "sessions.json") // parent absent
	s, _ := Load(p)
	s.Add(Session{Name: "alpha", ClaudeSessionID: "sid-1", TranscriptPath: "/t/x.jsonl"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(p)
	a, ok := got.Get("alpha")
	if !ok || a.ClaudeSessionID != "sid-1" || a.TranscriptPath != "/t/x.jsonl" {
		t.Fatalf("roundtrip failed: %+v ok=%v", a, ok)
	}
}

func TestGetMissing(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "s.json"))
	if _, ok := s.Get("ghost"); ok {
		t.Fatal("expected ghost absent")
	}
}

func TestLoadCorruptReturnsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	_ = writeFile(p, "{not json")
	s, err := Load(p)
	if err != nil {
		t.Fatalf("corrupt should not error, got %v", err)
	}
	if len(s.Sessions) != 0 {
		t.Fatal("corrupt should load empty")
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sessions.json")
	s, _ := Load(p)
	s.Add(Session{Name: "alpha", ClaudeSessionID: "sid-1"})
	s.Add(Session{Name: "beta", ClaudeSessionID: "sid-2"})
	s.Delete("alpha")
	if _, ok := s.Get("alpha"); ok {
		t.Fatal("alpha should be deleted")
	}
	if _, ok := s.Get("beta"); !ok {
		t.Fatal("beta should remain")
	}
}

func TestDeleteMissingIsNoop(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "s.json"))
	s.Delete("ghost") // must not panic
}
