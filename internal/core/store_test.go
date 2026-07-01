package core

import (
	"path/filepath"
	"testing"
)

func TestStoreLoadMissingFileIsEmpty(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("LoadStore on missing file: %v", err)
	}
	if len(s.Records()) != 0 {
		t.Fatalf("missing file should yield empty store, got %d records", len(s.Records()))
	}
}

func TestStoreAddGetDelete(t *testing.T) {
	s, _ := LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	a := Agent{Name: "one", Repo: "r", Task: "t", State: Held, Origin: Managed}
	s.Add(a)

	got, ok := s.Get("one")
	if !ok {
		t.Fatalf("Get(one): not found after Add")
	}
	if got.Task != "t" {
		t.Errorf("Get(one).Task = %q, want %q", got.Task, "t")
	}

	s.Delete("one")
	if _, ok := s.Get("one"); ok {
		t.Errorf("Get(one): still present after Delete")
	}
	s.Delete("missing") // must be a no-op, not a panic
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agents.json") // parent must be created
	s, _ := LoadStore(path)
	s.Add(Agent{Name: "a", Repo: "ra", State: Held, Origin: Managed})
	s.Add(Agent{Name: "b", Repo: "rb", State: Done, Origin: Managed, Session: "sid-b"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Records()) != 2 {
		t.Fatalf("reloaded %d records, want 2", len(reloaded.Records()))
	}
	b, ok := reloaded.Get("b")
	if !ok || b.Session != "sid-b" {
		t.Errorf("reloaded b = %+v, ok=%v; want Session sid-b", b, ok)
	}
}

func TestStoreLoadCorruptIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := writeFileForTest(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err != nil {
		t.Fatalf("corrupt file should not error, got %v", err)
	}
	if len(s.Records()) != 0 {
		t.Errorf("corrupt file should yield empty store, got %d", len(s.Records()))
	}
}
