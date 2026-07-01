package localagent

import (
	"path/filepath"
	"testing"
)

func TestAddGetRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "la.json")
	s, _ := Load(p)
	s.Add(Agent{Name: "fixer", Repo: "github.com/me/svc", Task: "fix the bug"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(p)
	a, ok := got.Get("fixer")
	if !ok || a.Repo != "github.com/me/svc" || a.Task != "fix the bug" {
		t.Fatalf("roundtrip: %+v ok=%v", a, ok)
	}
}

func TestBlankAgent(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "la.json"))
	s.Add(Agent{Name: "blank", Repo: "r"}) // no Task = blank
	a, _ := s.Get("blank")
	if a.Task != "" {
		t.Fatalf("blank agent should have empty task, got %q", a.Task)
	}
}

func TestDeleteAndMissing(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "la.json"))
	s.Add(Agent{Name: "a", Repo: "r"})
	s.Delete("a")
	if _, ok := s.Get("a"); ok {
		t.Fatal("deleted agent should be gone")
	}
	s.Delete("ghost") // no panic
}

func TestListOrderRecentFirst(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "la.json"))
	s.Add(Agent{Name: "never", Repo: "r"})
	s.Add(Agent{Name: "old", Repo: "r", LastRun: "2026-06-01T00:00:00Z"})
	s.Add(Agent{Name: "new", Repo: "r", LastRun: "2026-06-30T00:00:00Z"})
	got := s.List()
	order := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"new", "old", "never"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestLoadMissingEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(s.Agents) != 0 {
		t.Fatalf("missing → empty, got %+v err=%v", s.Agents, err)
	}
}
