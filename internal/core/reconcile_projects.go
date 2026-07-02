package core

import "sort"

// ReconcileProjects returns the full project list to display: every stored
// project, plus a synthesized project for any agent whose ProjectKey names a dir
// not already stored (auto-adopt), so dirs-with-agents always appear. Synthesized
// projects get Name=leaf(dir) and empty Repo (origin detection is I/O, done by
// the engine). Deduped by Dir, sorted by Name.
func ReconcileProjects(stored []Project, agents []Agent) []Project {
	byDir := map[string]Project{}
	for _, p := range stored {
		byDir[p.Dir] = p
	}
	for _, a := range agents {
		k := ProjectKey(a)
		if k == "" {
			continue
		}
		if _, ok := byDir[k]; !ok {
			byDir[k] = Project{Dir: k, Name: leaf(k)}
		}
	}
	out := make([]Project, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
