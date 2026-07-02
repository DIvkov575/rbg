package core

import "testing"

func TestReconcileProjectsAutoAdopts(t *testing.T) {
	stored := []Project{{Dir: "/w/app", Name: "app", Repo: "r"}}
	agents := []Agent{
		{Name: "a", ProjectDir: "/w/app"},     // already stored
		{Name: "b", ProjectDir: "/w/adopted"}, // new → auto-adopt
		{Name: "c", Dir: "/w/byDir"},          // no ProjectDir → adopt by Dir
		{Name: "d"},                           // no dir at all → no project
	}
	got := ReconcileProjects(stored, agents)
	byDir := map[string]Project{}
	for _, p := range got {
		byDir[p.Dir] = p
	}
	if byDir["/w/app"].Repo != "r" {
		t.Error("stored project should be preserved with its repo")
	}
	if byDir["/w/adopted"].Name != "adopted" {
		t.Errorf("adopted name = %q, want leaf 'adopted'", byDir["/w/adopted"].Name)
	}
	if _, ok := byDir["/w/byDir"]; !ok {
		t.Error("agent with only Dir should be adopted")
	}
	if len(got) != 3 {
		t.Errorf("want 3 projects, got %d (%v)", len(got), byDir)
	}
}
