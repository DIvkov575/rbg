package core

import "sort"

// Project is a suggested target for a new agent — a repo/dir the user is likely
// to want to work in. It unifies three origins (local checkouts, remote
// checkouts, and GitHub repos) behind one pickable value: Repo is exactly what
// `create` would receive (a path or a git identity), and Label is what the
// picker shows.
type Project struct {
	Label  string // human label shown in the picker
	Repo   string // the value passed to create (path or git URL/name)
	Origin string // "local" | "remote" | "github" | "agent" — where it was found
}

// MergeProjects unifies suggestions from several sources into one sorted,
// de-duplicated list. Dedup is by Repo (the actionable value); when the same
// Repo appears from multiple origins the FIRST occurrence wins, so callers pass
// the most specific/actionable source first (e.g. local checkouts before GitHub
// remotes). The result is sorted by Label for stable display.
func MergeProjects(lists ...[]Project) []Project {
	seen := map[string]bool{}
	var out []Project
	for _, list := range lists {
		for _, p := range list {
			if p.Repo == "" || seen[p.Repo] {
				continue
			}
			seen[p.Repo] = true
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
