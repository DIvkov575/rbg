package uitea

import "github.com/divkov575/rbg/internal/core"

// viewMode is which lens the list shows; tab / ctrl-s cycles them: the remote
// machine, the local machine, and the project grouping (across both machines).
type viewMode int

const (
	viewRemote viewMode = iota
	viewLocal
	viewProject
)

func (v viewMode) next() viewMode { return (v + 1) % 3 }

func (v viewMode) String() string {
	switch v {
	case viewRemote:
		return "remote"
	case viewLocal:
		return "local"
	case viewProject:
		return "projects"
	}
	return "?"
}

// machine returns the Location a machine-scoped view targets (remote/local),
// and false for the project view (which spans both).
func (v viewMode) machine() (core.Location, bool) {
	switch v {
	case viewRemote:
		return core.Remote, true
	case viewLocal:
		return core.Local, true
	}
	return "", false
}

// visible returns the agents shown by the current lens, IN DISPLAY ORDER, so
// the cursor indexes the same sequence the view renders. Remote/Local filter by
// machine; Project is the GroupByProject flatten.
func (m Model) visible() []core.Agent {
	switch m.view {
	case viewLocal:
		return core.OnMachine(m.agents, core.Local)
	case viewProject:
		var out []core.Agent
		for _, g := range core.GroupByProject(m.agents) {
			out = append(out, g.Agents...)
		}
		return out
	default: // remote
		return core.OnMachine(m.agents, core.Remote)
	}
}
