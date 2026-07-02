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

func TestGroupByProjectSortedWithUnlinkedLast(t *testing.T) {
	groups := GroupByProject(sampleInventory(), nil)
	// Keys fall back to repo leaf here (no Dir): "app" (2), "lib" (1), "" last.
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	if groups[0].Project != "app" || len(groups[0].Agents) != 2 {
		t.Errorf("group[0] = %q with %d agents, want app/2", groups[0].Project, len(groups[0].Agents))
	}
	if groups[1].Project != "lib" || len(groups[1].Agents) != 1 {
		t.Errorf("group[1] = %q with %d agents, want lib/1", groups[1].Project, len(groups[1].Agents))
	}
	if groups[2].Project != "" || len(groups[2].Agents) != 1 {
		t.Errorf("group[2] = %q with %d agents, want \"\"/1", groups[2].Project, len(groups[2].Agents))
	}
}

func TestGroupByProjectLinksSameDir(t *testing.T) {
	// The core linking rule: chats whose working dir is the same project (same
	// absolute Dir) are linked into one project group.
	inv := []Agent{
		{Name: "chat-a", Dir: "/home/me/workplace/rbg", Where: Local},
		{Name: "chat-b", Dir: "/home/me/workplace/rbg", Where: Local},
		{Name: "other", Dir: "/home/me/workplace/notes", Where: Local},
	}
	groups := GroupByProject(inv, nil)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (rbg, notes)", len(groups))
	}
	var rbg *ProjectGroup
	for i := range groups {
		if groups[i].Project == "/home/me/workplace/rbg" {
			rbg = &groups[i]
		}
	}
	if rbg == nil {
		t.Fatalf("no 'rbg' project group; got %+v", groups)
	}
	if len(rbg.Agents) != 2 {
		t.Errorf("same-dir chats should link into one project, got %d", len(rbg.Agents))
	}
}

func TestGroupByProjectAgentsSortedByName(t *testing.T) {
	inv := []Agent{
		{Name: "z", Dir: "/w/app"},
		{Name: "a", Dir: "/w/app"},
	}
	groups := GroupByProject(inv, nil)
	if groups[0].Agents[0].Name != "a" || groups[0].Agents[1].Name != "z" {
		t.Errorf("agents not name-sorted within group: %q, %q",
			groups[0].Agents[0].Name, groups[0].Agents[1].Name)
	}
}

func TestProjectKeyPrefersDirOverRepo(t *testing.T) {
	if k := ProjectKey(Agent{Dir: "/home/me/workplace/rbg", Repo: "git@github.com:me/rbg.git"}); k != "/home/me/workplace/rbg" {
		t.Errorf("ProjectKey with dir = %q, want /home/me/workplace/rbg", k)
	}
	if k := ProjectKey(Agent{Repo: "git@github.com:me/app.git"}); k != "app" {
		t.Errorf("ProjectKey repo fallback = %q, want app", k)
	}
	if k := ProjectKey(Agent{}); k != "" {
		t.Errorf("ProjectKey with nothing = %q, want empty", k)
	}
}

func TestGroupByProjectUsesLinkAndKeepsEmpty(t *testing.T) {
	agents := []Agent{
		{Name: "a", ProjectDir: "/w/app", Dir: "/w/app"},
		{Name: "b", Dir: "/w/lib"}, // no ProjectDir → falls back to Dir
	}
	projects := []Project{
		{Dir: "/w/app", Name: "app"},
		{Dir: "/w/empty", Name: "empty"}, // zero agents, must still appear
	}
	groups := GroupByProject(agents, projects)
	byName := map[string]int{}
	for _, g := range groups {
		byName[g.Project] = len(g.Agents)
	}
	if byName["/w/app"] != 1 {
		t.Errorf("app group = %d, want 1", byName["/w/app"])
	}
	if _, ok := byName["/w/empty"]; !ok {
		t.Error("empty project must render with zero agents")
	}
	if byName["/w/lib"] != 1 {
		t.Errorf("unlisted dir /w/lib should still group by fallback, got %d", byName["/w/lib"])
	}
}
