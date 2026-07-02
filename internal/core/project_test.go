package core

import "testing"

func TestMergeSuggestionsDedupsByRepoFirstWins(t *testing.T) {
	local := []Suggestion{
		{Label: "app (local)", Repo: "/home/me/workplace/app", Origin: "local"},
		{Label: "web", Repo: "web", Origin: "local"},
	}
	github := []Suggestion{
		{Label: "app (github)", Repo: "/home/me/workplace/app", Origin: "github"}, // dup Repo
		{Label: "newthing", Repo: "me/newthing", Origin: "github"},
	}
	got := MergeSuggestions(local, github)

	// dup Repo keeps the first (local) origin
	var appCount int
	for _, p := range got {
		if p.Repo == "/home/me/workplace/app" {
			appCount++
			if p.Origin != "local" {
				t.Errorf("dup Repo should keep first (local) origin, got %q", p.Origin)
			}
		}
	}
	if appCount != 1 {
		t.Errorf("duplicate Repo not deduped: %d copies", appCount)
	}
	// all unique repos present
	if len(got) != 3 {
		t.Errorf("got %d projects, want 3: %+v", len(got), got)
	}
	// sorted by Label
	for i := 1; i < len(got); i++ {
		if got[i-1].Label > got[i].Label {
			t.Errorf("not sorted by Label: %+v", got)
		}
	}
}

func TestMergeSuggestionsSkipsEmptyRepo(t *testing.T) {
	got := MergeSuggestions([]Suggestion{{Label: "blank", Repo: ""}, {Label: "ok", Repo: "x"}})
	if len(got) != 1 || got[0].Repo != "x" {
		t.Errorf("empty-repo project should be skipped, got %+v", got)
	}
}
