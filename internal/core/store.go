package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Store is rbg's on-disk registry of managed-agent records, at ~/.rbg/agents.json.
// It holds only what rbg created/knows about; live status and foreign agents are
// layered on by Reconcile, not persisted here. Keyed by Agent.Name.
type Store struct {
	path   string
	agents map[string]Agent
}

// LoadStore reads the registry at path. A missing or corrupt file yields an
// empty store (not an error), so first run and partial writes are tolerated.
func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, agents: map[string]Agent{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var wrap struct {
		Agents map[string]Agent `json:"agents"`
	}
	_ = json.Unmarshal(data, &wrap) // corrupt → keep empty map
	if wrap.Agents != nil {
		s.agents = wrap.Agents
	}
	return s, nil
}

// Add inserts or updates a record keyed by Name.
func (s *Store) Add(a Agent) { s.agents[a.Name] = a }

// Get returns the record for name.
func (s *Store) Get(name string) (Agent, bool) { a, ok := s.agents[name]; return a, ok }

// Delete removes a record (missing name is a no-op).
func (s *Store) Delete(name string) { delete(s.agents, name) }

// Records returns all records sorted by Name (stable order for callers/tests).
func (s *Store) Records() []Agent {
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Save writes the store atomically (temp file + rename), creating parent dirs.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Agents map[string]Agent `json:"agents"`
	}{s.agents}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// writeFileForTest is a tiny helper so tests can plant a fixture file without
// importing os directly in every test file.
func writeFileForTest(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
