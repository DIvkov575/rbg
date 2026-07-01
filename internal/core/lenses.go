package core

import (
	"path/filepath"
	"sort"
	"strings"
)

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

// ProjectKey is rbg's internal notion of "which project an agent belongs to",
// abstracting away the raw repo string and absolute dir. Agents whose work
// happens in the same directory are the same project — so chats in a single dir
// are linked by default. The key is the working dir's LEAF name (e.g.
// /home/me/workplace/rbg → "rbg"), which also links the same project across
// machines (local ~/workplace/rbg and remote ~/desk/workplace/rbg both → "rbg").
// Falls back to the repo's leaf when there's no dir, and "" (the "unlinked"
// bucket) when neither is known.
func ProjectKey(a Agent) string {
	if a.Dir != "" {
		return leaf(a.Dir)
	}
	if a.Repo != "" {
		return leaf(a.Repo)
	}
	return ""
}

// leaf extracts the final path/URL component, trimming a trailing slash and a
// ".git" suffix, so "git@github.com:me/app.git", "/w/app/" and "app" all → "app".
func leaf(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	if s == "" {
		return ""
	}
	// normalise so the same project on two machines with different roots links.
	return filepath.Base(s)
}

// ProjectGroup is one rbg project and the agents linked to it. The by-project
// view is a slice of these.
type ProjectGroup struct {
	Project string // the ProjectKey ("" = the unlinked bucket)
	Agents  []Agent
}

// GroupByProject groups agents by their ProjectKey (working-dir leaf), so chats
// in the same directory are linked into one project. Groups sort by name, with
// the unlinked ("") bucket last; agents within a group sort by Name.
func GroupByProject(agents []Agent) []ProjectGroup {
	byKey := map[string][]Agent{}
	for _, a := range agents {
		k := ProjectKey(a)
		byKey[k] = append(byKey[k], a)
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "") != (keys[j] == "") {
			return keys[j] == "" // unlinked bucket sorts last
		}
		return keys[i] < keys[j]
	})
	out := make([]ProjectGroup, 0, len(keys))
	for _, k := range keys {
		members := byKey[k]
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		out = append(out, ProjectGroup{Project: k, Agents: members})
	}
	return out
}
