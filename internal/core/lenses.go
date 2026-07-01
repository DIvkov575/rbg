package core

import "sort"

// OnMachine returns the agents whose Where matches, preserving input order.
// This is the by-machine view (local-only / remote-only) as a pure filter.
func OnMachine(agents []Agent, where Location) []Agent {
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		if a.Where == where {
			out = append(out, a)
		}
	}
	return out
}

// RepoGroup is one repository and the agents belonging to it. The by-project
// view (HLD F7) is a slice of these; a later layer attaches each group's Sync
// badge from the git layer.
type RepoGroup struct {
	Repo   string
	Agents []Agent
}

// GroupByRepo groups agents by Repo. Groups are sorted by repo name, except the
// empty-repo bucket (agents not pinned to a repo) which always sorts last.
// Agents within a group are sorted by Name.
func GroupByRepo(agents []Agent) []RepoGroup {
	byRepo := map[string][]Agent{}
	for _, a := range agents {
		byRepo[a.Repo] = append(byRepo[a.Repo], a)
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool {
		if (repos[i] == "") != (repos[j] == "") {
			return repos[j] == "" // empty repo sorts last
		}
		return repos[i] < repos[j]
	})
	out := make([]RepoGroup, 0, len(repos))
	for _, r := range repos {
		members := byRepo[r]
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		out = append(out, RepoGroup{Repo: r, Agents: members})
	}
	return out
}
