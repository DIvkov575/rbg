package client

import (
	"encoding/json"
	"fmt"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// FetchSessions returns the desktop's recorded sessions as structured data.
func FetchSessions(c *config.Config, r run.Runner) ([]session.Session, error) {
	body, code := runAgent(c, r, "ls", nil)
	if code != 0 {
		return nil, fmt.Errorf("ls failed (exit %d): %s", code, body)
	}
	var sessions []session.Session
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, fmt.Errorf("parse ls output: %w", err)
	}
	return sessions, nil
}

// FetchTranscript returns the named agent's transcript as rendered text. The
// agent's `read` already renders, so we pass its output through verbatim.
func FetchTranscript(c *config.Config, r run.Runner, name string) (string, error) {
	body, code := runAgent(c, r, "read", []string{"--id", name})
	if code != 0 {
		return "", fmt.Errorf("read failed (exit %d): %s", code, body)
	}
	return string(body), nil
}
