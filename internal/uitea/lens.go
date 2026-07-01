package uitea

import "github.com/divkov575/rbg/internal/core"

// viewMode is which lens the list shows; ctrl-s / tab cycles them.
type viewMode int

const (
	viewRemote viewMode = iota
	viewLocal
	viewCombined
	viewProject
)

func (v viewMode) next() viewMode { return (v + 1) % 4 }

func (v viewMode) String() string {
	switch v {
	case viewRemote:
		return "remote"
	case viewLocal:
		return "local"
	case viewCombined:
		return "combined"
	case viewProject:
		return "project"
	}
	return "?"
}

// visible returns the agents shown by the current lens, IN DISPLAY ORDER, so
// the cursor indexes the same sequence the view renders. Local/Remote filter by
// machine; Combined is local-then-remote; Project is the GroupByRepo flatten.
func (m Model) visible() []core.Agent {
	switch m.view {
	case viewLocal:
		return core.OnMachine(m.agents, core.Local)
	case viewRemote:
		return core.OnMachine(m.agents, core.Remote)
	case viewCombined:
		local := core.OnMachine(m.agents, core.Local)
		return append(local, core.OnMachine(m.agents, core.Remote)...)
	case viewProject:
		var out []core.Agent
		for _, g := range core.GroupByRepo(m.agents) {
			out = append(out, g.Agents...)
		}
		return out
	}
	return m.agents
}
