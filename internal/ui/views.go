package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// renderList renders the current view's body (the header/hints are added by the
// screen's View). It dispatches on m.View to the matching lens renderer.
func renderList(m *Model) string {
	switch m.View {
	case ViewCombined:
		return renderCombined(m)
	case ViewProject:
		return renderProject(m)
	default: // Remote, Local — a flat table of the visible agents
		return renderRows(m, m.Visible(), 0)
	}
}

// renderRows renders agents as aligned rows, marking the row at m.Cursor
// (cursor is an index into the current view's Visible() list; base is the
// starting global index of this block so multi-section views mark correctly).
func renderRows(m *Model, agents []core.Agent, base int) string {
	var b strings.Builder
	for i, a := range agents {
		marker := "  "
		if base+i == m.Cursor {
			marker = "> "
		}
		badge := syncBadge(a.Sync)
		fmt.Fprintf(&b, "%s%-20s  %-7s  %-8s  %-8s  %-10s  %s\n",
			marker, a.Name, a.Where, a.State, a.Origin, badge, a.Repo)
	}
	if len(agents) == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

// renderCombined sections the inventory by machine (local then remote), marking
// the cursor via renderRows' base offset so Cursor indexes the SAME sequence
// Visible() returns for Combined (local++remote).
func renderCombined(m *Model) string {
	var b strings.Builder
	local := core.OnMachine(m.Agents, core.Local)
	remote := core.OnMachine(m.Agents, core.Remote)
	b.WriteString("LOCAL\n")
	b.WriteString(renderRows(m, local, 0))
	b.WriteString("REMOTE\n")
	b.WriteString(renderRows(m, remote, len(local)))
	return b.String()
}

// renderProject groups agents by repo (core.GroupByRepo) with a per-group sync
// badge, marking the cursor via renderRows' base offset so Cursor indexes the
// GroupByRepo flattening that Visible() returns for Project.
func renderProject(m *Model) string {
	groups := core.GroupByRepo(m.Agents)
	if len(groups) == 0 {
		return "  (none)\n"
	}
	var b strings.Builder
	base := 0
	for _, g := range groups {
		repo := g.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		fmt.Fprintf(&b, "%s  %s\n", repo, groupSyncBadge(g))
		b.WriteString(renderRows(m, g.Agents, base))
		base += len(g.Agents)
	}
	return b.String()
}

// groupSyncBadge returns the badge for a repo group: the first non-unknown sync
// state among its agents (they share a checkout, so it's representative).
func groupSyncBadge(g core.RepoGroup) string {
	for _, a := range g.Agents {
		if b := syncBadge(a.Sync); b != "" {
			return b
		}
	}
	return ""
}

// syncBadge is a short human tag for a Sync state ("" for unknown).
func syncBadge(s core.Sync) string {
	switch s {
	case core.Aligned:
		return "[ok]"
	case core.Ahead:
		return "[ahead]"
	case core.Behind:
		return "[behind]"
	case core.Dirty:
		return "[dirty]"
	}
	return ""
}
