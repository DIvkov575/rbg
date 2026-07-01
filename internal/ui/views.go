package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// Column widths for the agent table. Name and Repo flex with the terminal
// width; the middle columns are fixed. These are display-cell widths.
const (
	colWWhere  = 7  // "remote"
	colWState  = 8  // "running"
	colWOrigin = 8  // "managed"
	colWSync   = 8  // "[behind]"
	colGap     = 2  // spaces between columns
	nameMin    = 12
	nameMax    = 28
	repoMin    = 10
)

// cursorMarker prefixes the selected row. Tests reference this constant rather
// than hard-coding the glyph, so a marker change doesn't ripple into assertions.
const cursorMarker = "▸ "

// nameWidth picks the Name column width from the terminal width, clamped to
// [nameMin, nameMax]. A zero/unknown width (non-tty) falls back to nameMax so
// tests and pipes get a stable layout.
func nameWidth(m *Model) int {
	if m.W <= 0 {
		return nameMax
	}
	// budget = W - (fixed cols + gaps + marker) ; give Name up to a third.
	third := m.W / 3
	if third < nameMin {
		return nameMin
	}
	if third > nameMax {
		return nameMax
	}
	return third
}

// repoWidth is whatever horizontal space remains after the fixed columns, so
// the Repo column fills the line instead of wrapping. Floored at repoMin.
func repoWidth(m *Model, nameW int) int {
	if m.W <= 0 {
		return 40 // stable width for non-tty / tests
	}
	used := 2 + nameW + colGap + colWWhere + colGap + colWState + colGap +
		colWOrigin + colGap + colWSync + colGap
	rem := m.W - used
	if rem < repoMin {
		return repoMin
	}
	return rem
}

// renderList renders the current view's body (header/hints come from the
// screen's View). It dispatches on m.View to the matching lens renderer.
func renderList(m *Model) string {
	nameW := nameWidth(m)
	repoW := repoWidth(m, nameW)
	var b strings.Builder
	b.WriteString(tableHeader(nameW, repoW))
	switch m.View {
	case ViewCombined:
		b.WriteString(renderCombined(m, nameW, repoW))
	case ViewProject:
		b.WriteString(renderProject(m, nameW, repoW))
	default: // Remote, Local — a flat table of the visible agents
		b.WriteString(renderRows(m, m.Visible(), 0, nameW, repoW))
	}
	return b.String()
}

// tableHeader renders the dimmed column-title row.
func tableHeader(nameW, repoW int) string {
	row := fmt.Sprintf("  %s  %s  %s  %s  %s  %s",
		truncPad("NAME", nameW),
		truncPad("WHERE", colWWhere),
		truncPad("STATE", colWState),
		truncPad("ORIGIN", colWOrigin),
		truncPad("SYNC", colWSync),
		pad("REPO", repoW),
	)
	return dim(row) + "\n"
}

// renderRows renders agents as aligned, coloured rows, marking the row at
// m.Cursor. base is the starting global index of this block so multi-section
// views mark the right row. Column text is truncated/padded to width BEFORE
// colour is applied, so colour codes never break alignment.
func renderRows(m *Model, agents []core.Agent, base, nameW, repoW int) string {
	if len(agents) == 0 {
		return dim("  (none)") + "\n"
	}
	var b strings.Builder
	for i, a := range agents {
		selected := base+i == m.Cursor
		marker := "  "
		if selected {
			marker = fg(colBlue, cursorMarker)
		}
		name := truncPad(a.Name, nameW)
		where := truncPad(string(a.Where), colWWhere)
		state := colorState(a.State, truncPad(string(a.State), colWState))
		origin := truncPad(string(a.Origin), colWOrigin)
		badge := colorSync(a.Sync, truncPad(syncBadge(a.Sync), colWSync))
		repo := pad(a.Repo, repoW)

		row := fmt.Sprintf("%s%s  %s  %s  %s  %s  %s",
			marker, name, where, state, origin, badge, repo)
		if selected {
			// Reverse-video the whole row so the selection reads at a glance.
			row = reverse(row)
		}
		b.WriteString(row)
		b.WriteByte('\n')
	}
	return b.String()
}

// renderCombined sections the inventory by machine (local then remote), marking
// the cursor via renderRows' base offset so Cursor indexes the SAME sequence
// Visible() returns for Combined (local++remote).
func renderCombined(m *Model, nameW, repoW int) string {
	var b strings.Builder
	local := core.OnMachine(m.Agents, core.Local)
	remote := core.OnMachine(m.Agents, core.Remote)
	b.WriteString(fg(colBlue, bold("LOCAL")) + "\n")
	b.WriteString(renderRows(m, local, 0, nameW, repoW))
	b.WriteString(fg(colBlue, bold("REMOTE")) + "\n")
	b.WriteString(renderRows(m, remote, len(local), nameW, repoW))
	return b.String()
}

// renderProject groups agents by repo (core.GroupByRepo) with a per-group sync
// badge, marking the cursor via renderRows' base offset so Cursor indexes the
// GroupByRepo flattening that Visible() returns for Project.
func renderProject(m *Model, nameW, repoW int) string {
	groups := core.GroupByRepo(m.Agents)
	if len(groups) == 0 {
		return dim("  (none)") + "\n"
	}
	var b strings.Builder
	base := 0
	for _, g := range groups {
		repo := g.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		badge := groupSyncBadge(g)
		fmt.Fprintf(&b, "%s  %s\n", fg(colBlue, bold(repo)), colorSyncByString(badge))
		b.WriteString(renderRows(m, g.Agents, base, nameW, repoW))
		base += len(g.Agents)
	}
	return b.String()
}

// groupSyncBadge returns the badge string for a repo group.
func groupSyncBadge(g core.RepoGroup) string {
	for _, a := range g.Agents {
		if b := syncBadge(a.Sync); b != "" {
			return b
		}
	}
	return ""
}

// colorState colours the already-padded state cell by lifecycle.
func colorState(s core.Lifecycle, padded string) string {
	switch s {
	case core.Running:
		return fg(colGreen, padded)
	case core.Held:
		return fg(colYellow, padded)
	case core.Done:
		return dim(padded)
	}
	return padded
}

// colorSync colours the already-padded sync badge cell.
func colorSync(s core.Sync, padded string) string {
	switch s {
	case core.Aligned:
		return fg(colGreen, padded)
	case core.Ahead:
		return fg(colYellow, padded)
	case core.Behind, core.Dirty:
		return fg(colRed, padded)
	}
	return padded
}

// colorSyncByString colours a bare badge string (used for group headers, where
// there's no padding to preserve).
func colorSyncByString(badge string) string {
	switch badge {
	case "[ok]":
		return fg(colGreen, badge)
	case "[ahead]":
		return fg(colYellow, badge)
	case "[behind]", "[dirty]":
		return fg(colRed, badge)
	}
	return badge
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
