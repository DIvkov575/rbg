package ui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func viewModel(view ViewMode) *Model {
	m := NewModel([]core.Agent{
		{Name: "rem-run", Repo: "app", Where: core.Remote, State: core.Running, Origin: core.Managed, Sync: core.Behind},
		{Name: "loc-held", Repo: "app", Where: core.Local, State: core.Held, Origin: core.Managed},
		{Name: "wild", Repo: "lib", Where: core.Remote, State: core.Done, Origin: core.Foreign, Sync: core.Aligned},
	})
	m.View = view
	m.W, m.H = 80, 24
	return m
}

func TestRenderRemoteListsOnlyRemote(t *testing.T) {
	out := renderList(viewModel(ViewRemote))
	if !strings.Contains(out, "rem-run") || !strings.Contains(out, "wild") {
		t.Errorf("remote view should list remote agents:\n%s", out)
	}
	if strings.Contains(out, "loc-held") {
		t.Errorf("remote view should NOT list the local agent:\n%s", out)
	}
}

func TestRenderCombinedSectionsByMachine(t *testing.T) {
	out := renderList(viewModel(ViewCombined))
	if !strings.Contains(out, "LOCAL") || !strings.Contains(out, "REMOTE") {
		t.Errorf("combined view should have machine sections:\n%s", out)
	}
	for _, n := range []string{"rem-run", "loc-held", "wild"} {
		if !strings.Contains(out, n) {
			t.Errorf("combined view missing %q:\n%s", n, out)
		}
	}
}

func TestRenderProjectGroupsByRepoWithSyncBadge(t *testing.T) {
	out := renderList(viewModel(ViewProject))
	// repo names appear as group headers
	if !strings.Contains(out, "app") || !strings.Contains(out, "lib") {
		t.Errorf("project view should group by repo:\n%s", out)
	}
	// a sync badge for a behind repo is shown somewhere
	if !strings.Contains(strings.ToLower(out), "behind") {
		t.Errorf("project view should show a sync badge:\n%s", out)
	}
}

func TestRenderMarksCursorRow(t *testing.T) {
	m := viewModel(ViewRemote)
	m.Cursor = 1
	out := renderList(m)
	// the cursor marker (">") precedes the selected row's name
	lines := strings.Split(out, "\n")
	var marked string
	for _, ln := range lines {
		if strings.Contains(ln, ">") && (strings.Contains(ln, "rem-run") || strings.Contains(ln, "wild")) {
			marked = ln
		}
	}
	if !strings.Contains(marked, "wild") {
		t.Errorf("cursor at 1 should mark the second remote row (wild):\n%s", out)
	}
}

func TestSyncBadge(t *testing.T) {
	cases := map[core.Sync]string{
		core.Aligned:     "ok",
		core.Behind:      "behind",
		core.Ahead:       "ahead",
		core.Dirty:       "dirty",
		core.SyncUnknown: "",
	}
	for s, want := range cases {
		got := syncBadge(s)
		if want == "" {
			if got != "" {
				t.Errorf("syncBadge(%q) = %q, want empty", s, got)
			}
		} else if !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("syncBadge(%q) = %q, want to contain %q", s, got, want)
		}
	}
}

func TestProjectCursorMarksSelectedAgent(t *testing.T) {
	m := viewModel(ViewProject)
	// Selected() must agree with the marked row across the group reordering.
	for i := 0; i < len(m.Visible()); i++ {
		m.Cursor = i
		sel, ok := m.Selected()
		if !ok {
			t.Fatalf("cursor %d: no selection", i)
		}
		out := renderList(m)
		// the marked ("> ") line must contain the selected agent's name.
		var marked string
		for _, ln := range strings.Split(out, "\n") {
			if strings.HasPrefix(ln, "> ") {
				marked = ln
			}
		}
		if !strings.Contains(marked, sel.Name) {
			t.Errorf("project cursor %d: marked line %q does not contain Selected() %q\n%s", i, marked, sel.Name, out)
		}
	}
}

func TestCombinedCursorMarksSelectedAgent(t *testing.T) {
	m := viewModel(ViewCombined)
	for i := 0; i < len(m.Visible()); i++ {
		m.Cursor = i
		sel, _ := m.Selected()
		out := renderList(m)
		var marked string
		for _, ln := range strings.Split(out, "\n") {
			if strings.HasPrefix(ln, "> ") {
				marked = ln
			}
		}
		if !strings.Contains(marked, sel.Name) {
			t.Errorf("combined cursor %d: marked %q lacks Selected() %q\n%s", i, marked, sel.Name, out)
		}
	}
}
