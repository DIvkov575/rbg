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

// ProjectKey is which project an agent belongs to: its explicit ProjectDir link
// if set, else its own Dir, else its Repo leaf, else "" (the unlinked bucket).
func ProjectKey(a Agent) string {
	if a.ProjectDir != "" {
		return a.ProjectDir
	}
	if a.Dir != "" {
		return a.Dir
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

// GroupByProject groups agents under their ProjectKey. It takes the known
// projects so a project with zero agents still renders. Group keys are project
// Dirs (or fallback keys); groups sort by key with the unlinked "" bucket last;
// agents within a group sort by Name.
func GroupByProject(agents []Agent, projects []Project) []ProjectGroup {
	byKey := map[string][]Agent{}
	for _, a := range agents {
		byKey[ProjectKey(a)] = append(byKey[ProjectKey(a)], a)
	}
	// ensure every known project appears, even with no agents.
	for _, p := range projects {
		if _, ok := byKey[p.Dir]; !ok {
			byKey[p.Dir] = nil
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "") != (keys[j] == "") {
			return keys[j] == ""
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
