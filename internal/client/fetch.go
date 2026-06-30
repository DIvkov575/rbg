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

// DirEntry is one subdirectory returned by FetchDirs.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirListing is the parsed result of the agent's `lsdir` verb: the resolved
// directory, its parent (for navigating up), and its visible subdirectories.
type DirListing struct {
	Dir     string     `json:"dir"`
	Parent  string     `json:"parent"`
	Entries []DirEntry `json:"entries"`
}

// FetchDirs lists the subdirectories of dir on the desktop via the agent's
// `lsdir` verb. An empty dir lets the agent pick its default (LaunchDir or
// home). A nonzero exit (including ssh 255) is reported as an error.
func FetchDirs(c *config.Config, r run.Runner, dir string) (DirListing, error) {
	var verbArgs []string
	if dir != "" {
		verbArgs = []string{"--dir", dir}
	}
	body, code := runAgent(c, r, "lsdir", verbArgs)
	if code != 0 {
		return DirListing{}, fmt.Errorf("lsdir failed (exit %d): %s", code, body)
	}
	var listing DirListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return DirListing{}, fmt.Errorf("parse lsdir output: %w", err)
	}
	return listing, nil
}

// MakeDir creates dir on the desktop via the agent's `mkdir` verb and returns
// the created absolute path. A nonzero exit (including ssh 255) is reported as
// an error.
func MakeDir(c *config.Config, r run.Runner, dir string) (string, error) {
	body, code := runAgent(c, r, "mkdir", []string{"--dir", dir})
	if code != 0 {
		return "", fmt.Errorf("mkdir failed (exit %d): %s", code, body)
	}
	var obj struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", fmt.Errorf("parse mkdir output: %w", err)
	}
	return obj.Dir, nil
}

// CloneRepo asks the desktop agent to clone-or-reuse repo and returns the local
// clone directory. Mirrors FetchDirs.
func CloneRepo(c *config.Config, r run.Runner, repo string) (string, error) {
	body, code := runAgent(c, r, "clone", []string{"--repo", repo})
	if code != 0 {
		return "", fmt.Errorf("clone failed (exit %d): %s", code, body)
	}
	var resp struct {
		Dir   string `json:"dir"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse clone output: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("clone: %s", resp.Error)
	}
	return resp.Dir, nil
}
