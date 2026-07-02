package ptybridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Worker is one live background agent as recorded in the daemon roster. Only the
// fields we need to attach are modeled.
type Worker struct {
	Pid       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	PtySock   string `json:"ptySock"`
	CWD       string `json:"cwd"`
}

// roster is the on-disk ~/.claude/daemon/roster.json shape.
type roster struct {
	Workers map[string]Worker `json:"workers"`
}

// RosterPath returns the daemon roster path under home.
func RosterPath(home string) string {
	return filepath.Join(home, ".claude", "daemon", "roster.json")
}

// FindWorker loads the roster under home and returns the live worker whose
// sessionId equals or is prefixed by id (the roster keys and the CLI both use
// the short 8-char form, so a prefix match resolves either). It returns an error
// the caller can surface verbatim when no live worker matches — that is the
// signal the session is not currently running as a bg agent.
func FindWorker(home, id string) (Worker, error) {
	data, err := os.ReadFile(RosterPath(home))
	if err != nil {
		return Worker{}, fmt.Errorf("no bg-agent daemon roster (%v)", err)
	}
	var r roster
	if err := json.Unmarshal(data, &r); err != nil {
		return Worker{}, fmt.Errorf("roster unreadable: %v", err)
	}
	for _, w := range r.Workers {
		if matches(w.SessionID, id) {
			if w.PtySock == "" {
				return Worker{}, fmt.Errorf("worker %s has no pty socket", id)
			}
			return w, nil
		}
	}
	return Worker{}, fmt.Errorf("session %s is not a live background agent", id)
}

// matches reports whether session and id name the same session, tolerating the
// short (8-char) vs full uuid forms in either position.
func matches(session, id string) bool {
	if session == id {
		return true
	}
	if len(id) >= 8 && len(session) >= len(id) && session[:len(id)] == id {
		return true
	}
	if len(session) >= 8 && len(id) >= len(session) && id[:len(session)] == session {
		return true
	}
	return false
}
