package uitea

import "github.com/divkov575/rbg/internal/core"

// viewMode is which lens the list shows; tab / ctrl-s cycles them. There are
// only two: the flat combined view (everything, local then remote) and the
// project view (grouped by rbg project). The old local-only / remote-only
// individual views were removed — combined already shows both machines.
type viewMode int

const (
	viewCombined viewMode = iota
	viewProject
)

func (v viewMode) next() viewMode { return (v + 1) % 2 }

func (v viewMode) String() string {
	switch v {
	case viewCombined:
		return "all"
	case viewProject:
		return "projects"
	}
	return "?"
}

// visible returns the agents shown by the current lens, IN DISPLAY ORDER, so
// the cursor indexes the same sequence the view renders. Combined is local-then-
// remote; Project is the GroupByProject flatten.
func (m Model) visible() []core.Agent {
	switch m.view {
	case viewProject:
		var out []core.Agent
		for _, g := range core.GroupByProject(m.agents) {
			out = append(out, g.Agents...)
		}
		return out
	default: // combined
		local := core.OnMachine(m.agents, core.Local)
		return append(local, core.OnMachine(m.agents, core.Remote)...)
	}
}
