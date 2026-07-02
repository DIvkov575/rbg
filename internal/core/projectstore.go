package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ProjectStore is rbg's on-disk registry of first-class projects at
// ~/.rbg/projects.json, keyed by absolute Dir. Same atomic-write, corrupt→empty
// semantics as Store.
type ProjectStore struct {
	path     string
	projects map[string]Project
}

func LoadProjectStore(path string) (*ProjectStore, error) {
	s := &ProjectStore{path: path, projects: map[string]Project{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var wrap struct {
		Projects map[string]Project `json:"projects"`
	}
	_ = json.Unmarshal(data, &wrap)
	if wrap.Projects != nil {
		s.projects = wrap.Projects
	}
	return s, nil
}

func (s *ProjectStore) Add(p Project) { s.projects[p.Dir] = p }

func (s *ProjectStore) Get(dir string) (Project, bool) { p, ok := s.projects[dir]; return p, ok }

// Rename sets the display name for dir; returns false if dir is absent.
func (s *ProjectStore) Rename(dir, name string) bool {
	p, ok := s.projects[dir]
	if !ok {
		return false
	}
	p.Name = name
	s.projects[dir] = p
	return true
}

func (s *ProjectStore) Delete(dir string) { delete(s.projects, dir) }

func (s *ProjectStore) Records() []Project {
	out := make([]Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *ProjectStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Projects map[string]Project `json:"projects"`
	}{s.projects}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
