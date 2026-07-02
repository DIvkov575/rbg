package core

import "sort"

// Suggestion is a pickable target when ADDING a project — a repo/dir the user is
// likely to want. It unifies local checkouts, remote checkouts, and GitHub repos
// behind one value: Repo is what add would receive; Label is what the picker shows.
// (Formerly named Project; renamed when Project became a first-class stored type.)
type Suggestion struct {
	Label  string // human label shown in the picker
	Repo   string // the value passed to add (path or git URL/name)
	Origin string // "local" | "remote" | "github" | "agent"
}

// MergeSuggestions unifies suggestions from several sources into one sorted,
// de-duplicated list. Dedup is by Repo; first occurrence wins, so callers pass
// the most specific source first. Sorted by Label for stable display.
func MergeSuggestions(lists ...[]Suggestion) []Suggestion {
	seen := map[string]bool{}
	var out []Suggestion
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

// Project is a first-class unit of work: a local directory that owns agents and
// has at most one auto-detected git repo. Identity is Dir (the ProjectStore map
// key); it exists independently of any agent.
type Project struct {
	Dir    string `json:"dir"`    // absolute LOCAL dir — identity / store key
	Name   string `json:"name"`   // display name; defaults to leaf(Dir), renameable
	Repo   string `json:"repo"`   // origin URL, auto-detected; "" if not a git repo
	Remote string `json:"remote"` // mirrored desktop dir; "" until mirrored
}
