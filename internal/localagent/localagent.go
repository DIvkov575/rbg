// Package localagent is a client-only registry of "local agents": persistent,
// re-runnable work units each pinned to a repo. Unlike queue items (staged for
// one dispatch), a local agent is created once — possibly blank (no task) — and
// invoked manually later, e.g. after a remote agent has finished and synced
// changes to the repo. Lives at ~/.rbg/local-agents.json.
package localagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Agent is one local agent pinned to a repo.
type Agent struct {
	Name     string `json:"name"`     // unique handle
	Repo     string `json:"repo"`     // git URL or local path it is pinned to
	Task     string `json:"task"`     // optional default task ("" = blank)
	LastRun  string `json:"lastRun"`  // RFC3339 of the last manual run ("" = never)
	LastTask string `json:"lastTask"` // the task used on the last run
}

// Store is the on-disk registry.
type Store struct {
	path   string
	Agents map[string]Agent `json:"agents"`
}

// Load reads the registry; a missing or corrupt file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, Agents: map[string]Agent{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, s) // corrupt → keep empty map
	if s.Agents == nil {
		s.Agents = map[string]Agent{}
	}
	return s, nil
}

// Add inserts/updates an agent keyed by Name.
func (s *Store) Add(a Agent) { s.Agents[a.Name] = a }

// Get returns the agent for name.
func (s *Store) Get(name string) (Agent, bool) { a, ok := s.Agents[name]; return a, ok }

// Delete removes an agent (missing = no-op).
func (s *Store) Delete(name string) { delete(s.Agents, name) }

// List returns agents sorted: most-recently-run first, never-run after, name asc
// as a tie-break — a stable order for display.
func (s *Store) List() []Agent {
	out := make([]Agent, 0, len(s.Agents))
	for _, a := range s.Agents {
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastRun != out[j].LastRun {
			return out[i].LastRun > out[j].LastRun // RFC3339 desc; "" sorts last
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Save writes the store atomically (temp + rename), creating parents.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Agents map[string]Agent `json:"agents"`
	}{s.Agents}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
