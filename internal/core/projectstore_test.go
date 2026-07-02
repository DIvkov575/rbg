package core

import (
	"path/filepath"
	"testing"
)

func TestProjectStoreRoundTripRenameDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	s, err := LoadProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(Project{Dir: "/w/app", Name: "app", Repo: "git@x:me/app.git"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := s2.Get("/w/app"); !ok || p.Repo != "git@x:me/app.git" {
		t.Fatalf("reload lost project: %+v ok=%v", p, ok)
	}
	if !s2.Rename("/w/app", "renamed") {
		t.Fatal("rename should succeed")
	}
	if p, _ := s2.Get("/w/app"); p.Name != "renamed" {
		t.Errorf("name = %q, want renamed", p.Name)
	}
	if s2.Rename("/w/missing", "x") {
		t.Error("rename of missing dir should be false")
	}
	s2.Delete("/w/app")
	if _, ok := s2.Get("/w/app"); ok {
		t.Error("delete failed")
	}
}

func TestLoadProjectStoreMissingIsEmpty(t *testing.T) {
	s, err := LoadProjectStore(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(s.Records()) != 0 {
		t.Fatalf("missing file should be empty store, err=%v n=%d", err, len(s.Records()))
	}
}
