package uitea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/divkov575/rbg/internal/core"
)

// Palette — muted, close to Claude Code's agents view: a violet accent, green
// for active work, dim gray for finished, red/amber for attention.
var (
	cAccent = lipgloss.Color("62")  // violet (Claude's accent family)
	cActive = lipgloss.Color("42")  // green — running
	cHold   = lipgloss.Color("178") // amber — held
	cDone   = lipgloss.Color("245") // gray — done
	cWarn   = lipgloss.Color("167") // red — dirty/behind
	cDim    = lipgloss.Color("240")
	cText   = lipgloss.Color("252")

	stTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(cAccent).Padding(0, 1)
	stTab      = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1)
	stTabOn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(cAccent).Padding(0, 1)
	stHeader   = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	stRow      = lipgloss.NewStyle().Foreground(cText)
	stSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("238")).Bold(true)
	stSection  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stStatus   = lipgloss.NewStyle().Foreground(cHold).Italic(true)
	stHints    = lipgloss.NewStyle().Foreground(cDim)
	stBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
)

// View renders the whole dashboard for the current mode.
func (m Model) View() string {
	switch m.mode {
	case modeInput:
		return m.viewInput()
	case modePager:
		return m.viewPager()
	case modePicker:
		return m.viewPicker()
	default:
		return m.viewList()
	}
}

// dot returns a colored status dot for an agent's lifecycle, echoing the
// bullet Claude Code shows next to each agent.
func dot(a core.Agent) string {
	switch a.State {
	case core.Running:
		return lipgloss.NewStyle().Foreground(cActive).Render("●")
	case core.Held:
		return lipgloss.NewStyle().Foreground(cHold).Render("◐")
	default: // done
		return lipgloss.NewStyle().Foreground(cDone).Render("○")
	}
}

func syncTag(s core.Sync) string {
	switch s {
	case core.Aligned:
		return lipgloss.NewStyle().Foreground(cActive).Render("synced")
	case core.Ahead:
		return lipgloss.NewStyle().Foreground(cHold).Render("ahead")
	case core.Behind:
		return lipgloss.NewStyle().Foreground(cWarn).Render("behind")
	case core.Dirty:
		return lipgloss.NewStyle().Foreground(cWarn).Render("dirty")
	}
	return lipgloss.NewStyle().Foreground(cDim).Render("—")
}

// tabsBar renders the four lens tabs with the active one highlighted.
func (m Model) tabsBar() string {
	names := []viewMode{viewRemote, viewLocal, viewCombined, viewProject}
	var parts []string
	for _, v := range names {
		if v == m.view {
			parts = append(parts, stTabOn.Render(v.String()))
		} else {
			parts = append(parts, stTab.Render(v.String()))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// contentWidth is the usable inner width (fallback for non-tty/tests).
func (m Model) contentWidth() int {
	if m.w <= 0 {
		return 90
	}
	return m.w
}

// viewList renders the agents table for the current lens.
func (m Model) viewList() string {
	var b strings.Builder

	title := stTitle.Render(fmt.Sprintf("rbg · %d agents", len(m.agents)))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", m.tabsBar()))
	b.WriteString("\n\n")

	nameW := clamp(m.contentWidth()/3, 14, 30)
	b.WriteString(m.tableHeader(nameW))
	b.WriteByte('\n')

	switch m.view {
	case viewCombined:
		b.WriteString(m.sectionedRows(nameW))
	case viewProject:
		b.WriteString(m.projectRows(nameW))
	default:
		b.WriteString(m.rows(m.visible(), 0, nameW))
	}

	if m.status != "" {
		b.WriteString("\n" + stStatus.Render(m.status) + "\n")
	}
	b.WriteString("\n" + stHints.Render(
		"↑/↓ move · tab lens · enter read · g run · s send · x kill · A adopt · n new · r refresh · q quit"))
	return b.String()
}

func (m Model) tableHeader(nameW int) string {
	return stHeader.Render(fmt.Sprintf("  %-*s  %-7s  %-8s  %-8s  %-7s  %s",
		nameW, "NAME", "WHERE", "STATE", "ORIGIN", "SYNC", "REPO"))
}

// rows renders agents as aligned rows; base is the starting global index so the
// cursor marks the right row across multi-section views.
func (m Model) rows(agents []core.Agent, base, nameW int) string {
	if len(agents) == 0 {
		return stHints.Render("  (none)") + "\n"
	}
	var b strings.Builder
	for i, a := range agents {
		selected := base+i == m.cursor
		line := fmt.Sprintf("%s %s  %-*s  %-7s  %-8s  %-8s  %-7s  %s",
			cursorGlyph(selected), dot(a),
			nameW, trunc(a.Name, nameW),
			trunc(string(a.Where), 7),
			trunc(string(a.State), 8),
			trunc(string(a.Origin), 8),
			plainSync(a.Sync),
			a.Repo)
		if selected {
			b.WriteString(stSelected.Render(line))
		} else {
			b.WriteString(stRow.Render(line))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (m Model) sectionedRows(nameW int) string {
	var b strings.Builder
	local := core.OnMachine(m.agents, core.Local)
	remote := core.OnMachine(m.agents, core.Remote)
	b.WriteString(stSection.Render("LOCAL") + "\n")
	b.WriteString(m.rows(local, 0, nameW))
	b.WriteString(stSection.Render("REMOTE") + "\n")
	b.WriteString(m.rows(remote, len(local), nameW))
	return b.String()
}

func (m Model) projectRows(nameW int) string {
	groups := core.GroupByRepo(m.agents)
	if len(groups) == 0 {
		return stHints.Render("  (none)") + "\n"
	}
	var b strings.Builder
	base := 0
	for _, g := range groups {
		repo := g.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		b.WriteString(stSection.Render(repo) + "\n")
		b.WriteString(m.rows(g.Agents, base, nameW))
		base += len(g.Agents)
	}
	return b.String()
}

// viewInput renders the create/send overlay in a bordered box.
func (m Model) viewInput() string {
	body := fmt.Sprintf("%s\n\n> %s", stSection.Render(m.input.prompt()), m.input.buf)
	box := stBox.Width(clamp(m.contentWidth()-4, 30, 80)).Render(body)
	return box + "\n" + stHints.Render("type · enter next/submit · esc cancel")
}

// viewPicker renders the project chooser: a filter line and a scrollable list
// of matching projects, the selected one highlighted, origin-colored.
func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("Pick a project for the new agent") + "\n")
	b.WriteString(stHints.Render("filter: "+m.picker.filter+"▏") + "\n\n")

	matches := m.picker.matches()
	// Window the list to the terminal height so long lists scroll around the cursor.
	win := m.h - 6
	if win < 3 {
		win = 3
	}
	start := 0
	if m.picker.cursor >= win {
		start = m.picker.cursor - win + 1
	}
	end := start + win
	if end > len(matches) {
		end = len(matches)
	}
	for i := start; i < end; i++ {
		p := matches[i]
		line := "  " + p.Label
		if i == m.picker.cursor {
			line = stSelected.Render("▸ " + p.Label)
		} else {
			line = stRow.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(matches) == 0 {
		b.WriteString(stHints.Render("  (no matches)") + "\n")
	}
	b.WriteString("\n" + stHints.Render("type to filter · ↑/↓ move · enter choose · esc cancel"))
	return b.String()
}

// viewPager renders the transcript.
func (m Model) viewPager() string {
	var b strings.Builder
	b.WriteString(stTitle.Render(m.pager.title) + "\n\n")
	win := m.pager.window(m.h)
	end := m.pager.offset + win
	if end > len(m.pager.lines) {
		end = len(m.pager.lines)
	}
	for i := m.pager.offset; i < end; i++ {
		b.WriteString(m.pager.lines[i] + "\n")
	}
	b.WriteString("\n" + stHints.Render("j/k scroll · esc/q back"))
	return b.String()
}

// --- small helpers ---

func cursorGlyph(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Foreground(cAccent).Render("▸")
	}
	return " "
}

// plainSync is the uncolored sync word for column alignment (the dot carries the
// colour signal; keeping the sync cell plain keeps widths exact under lipgloss).
func plainSync(s core.Sync) string {
	switch s {
	case core.Aligned:
		return "synced"
	case core.Ahead:
		return "ahead"
	case core.Behind:
		return "behind"
	case core.Dirty:
		return "dirty"
	}
	return "—"
}

func trunc(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

var _ = syncTag // reserved for a future detail view
