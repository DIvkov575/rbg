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

// renderCombined sections the inventory by machine (local then remote). The
// cursor indexes the concatenation local++remote, matching Visible() order for
// Combined (which returns all agents in inventory order) — so this renderer
// re-derives sections but marks by global cursor over the SAME order Visible
// uses. To keep cursor semantics simple, Combined marks over m.Agents order.
func renderCombined(m *Model) string {
	var b strings.Builder
	b.WriteString("LOCAL\n")
	b.WriteString(renderRowsFiltered(m, core.Local))
	b.WriteString("REMOTE\n")
	b.WriteString(renderRowsFiltered(m, core.Remote))
	return b.String()
}

// renderRowsFiltered renders one machine's agents, marking the cursor by the
// agent's index within m.Visible() (Combined's Visible == m.Agents).
func renderRowsFiltered(m *Model, where core.Location) string {
	var b strings.Builder
	any := false
	for i, a := range m.Agents {
		if a.Where != where {
			continue
		}
		any = true
		marker := "  "
		if i == m.Cursor {
			marker = "> "
		}
		fmt.Fprintf(&b, "%s%-20s  %-8s  %-8s  %-10s  %s\n",
			marker, a.Name, a.State, a.Origin, syncBadge(a.Sync), a.Repo)
	}
	if !any {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

// renderProject groups agents by repo (core.GroupByRepo) with a per-group sync
// badge (taken from the group's first agent that has a known sync state).
func renderProject(m *Model) string {
	var b strings.Builder
	for _, g := range core.GroupByRepo(m.Agents) {
		repo := g.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		fmt.Fprintf(&b, "%s  %s\n", repo, groupSyncBadge(g))
		for _, a := range g.Agents {
			fmt.Fprintf(&b, "    %-20s  %-7s  %-8s  %-8s\n", a.Name, a.Where, a.State, a.Origin)
		}
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
