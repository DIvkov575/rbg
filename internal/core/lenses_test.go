package core

import "testing"

func sampleInventory() []Agent {
	return []Agent{
		{Name: "a", Repo: "app", Where: Local, State: Running},
		{Name: "b", Repo: "app", Where: Remote, State: Done},
		{Name: "c", Repo: "lib", Where: Remote, State: Held},
		{Name: "d", Repo: "", Where: Local, State: Done}, // no repo
	}
}

func TestOnMachine(t *testing.T) {
	inv := sampleInventory()
	local := OnMachine(inv, Local)
	if len(local) != 2 {
		t.Fatalf("Local: got %d, want 2", len(local))
	}
	for _, a := range local {
		if a.Where != Local {
			t.Errorf("OnMachine(Local) returned %q with Where=%q", a.Name, a.Where)
		}
	}
	remote := OnMachine(inv, Remote)
	if len(remote) != 2 {
		t.Fatalf("Remote: got %d, want 2", len(remote))
	}
	if remote[0].Name != "b" || remote[1].Name != "c" {
		t.Errorf("OnMachine did not preserve input order: got %q, %q; want b, c",
			remote[0].Name, remote[1].Name)
	}
}

func TestGroupByRepoSortedWithNoRepoLast(t *testing.T) {
	groups := GroupByRepo(sampleInventory())
	// Expect groups: "app" (2), "lib" (1), then "" bucket (1) last.
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	if groups[0].Repo != "app" || len(groups[0].Agents) != 2 {
		t.Errorf("group[0] = %q with %d agents, want app/2", groups[0].Repo, len(groups[0].Agents))
	}
	if groups[1].Repo != "lib" || len(groups[1].Agents) != 1 {
		t.Errorf("group[1] = %q with %d agents, want lib/1", groups[1].Repo, len(groups[1].Agents))
	}
	if groups[2].Repo != "" || len(groups[2].Agents) != 1 {
		t.Errorf("group[2] = %q with %d agents, want \"\"/1", groups[2].Repo, len(groups[2].Agents))
	}
}

func TestGroupByRepoAgentsSortedByName(t *testing.T) {
	inv := []Agent{
		{Name: "z", Repo: "app"},
		{Name: "a", Repo: "app"},
	}
	groups := GroupByRepo(inv)
	if groups[0].Agents[0].Name != "a" || groups[0].Agents[1].Name != "z" {
		t.Errorf("agents not name-sorted within group: %q, %q",
			groups[0].Agents[0].Name, groups[0].Agents[1].Name)
	}
}
