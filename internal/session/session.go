// Package session manages the desktop agent's session-state file:
// id -> {name, claudeSessionId, transcriptPath, pid, startedAt}.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Session is one tracked agent session.
type Session struct {
	Name            string `json:"name"`
	ClaudeSessionID string `json:"claudeSessionId"`
	TranscriptPath  string `json:"transcriptPath"`
	PID             int    `json:"pid"`
	StartedAt       string `json:"startedAt"`
}

// Store is the in-memory + on-disk session map.
type Store struct {
	path     string
	Sessions map[string]Session
}

// Load reads the store at path; a missing or corrupt file yields an empty store
// (not an error), so first-run and partial-writes are tolerated.
func Load(path string) (*Store, error) {
	s := &Store{path: path, Sessions: map[string]Session{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, &s.Sessions) // corrupt → keep empty map
	if s.Sessions == nil {
		s.Sessions = map[string]Session{}
	}
	return s, nil
}

// Add inserts/updates a session keyed by its Name.
func (s *Store) Add(sess Session) { s.Sessions[sess.Name] = sess }

// Get returns the session for id (==name in v2).
func (s *Store) Get(id string) (Session, bool) {
	sess, ok := s.Sessions[id]
	return sess, ok
}

// Save writes the store atomically (temp + rename), creating parents.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Sessions, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
