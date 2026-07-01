package host

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// LocalProjects lists git checkouts under the laptop's <base>/* (e.g.
// ~/workplace/*) as suggestable projects. A directory is a project if it
// contains a .git entry. Errors degrade to an empty list — suggestions are
// best-effort, never fatal to the create flow.
func LocalProjects(r run.Runner, base string) []core.Project {
	// `ls -d <base>/*/.git` prints one line per checkout; trim /.git for the dir.
	out, code, err := r.Run("sh", []string{"-c", "ls -d " + shQuote(base) + "/*/.git 2>/dev/null"}, nil)
	if err != nil || code != 0 {
		return nil
	}
	return parseGitDirs(string(out), "local")
}

// RemoteProjects lists git checkouts under the desktop's <base>/* over SSH.
func RemoteProjects(c *config.Config, r run.Runner, base string) []core.Project {
	remote := []string{"sh", "-c", "ls -d " + shQuote(base) + "/*/.git 2>/dev/null"}
	args := sshx.BuildSSHArgs(c, remote, sshx.Options{ConnectTimeout: true})
	out, code, err := r.Run("ssh", args, nil)
	if err != nil || code != 0 {
		return nil
	}
	return parseGitDirs(string(out), "remote")
}

// GithubProjects lists the user's GitHub repos via `gh repo list --json`. It
// suggests each as "owner/name" (what create/clone accepts). Degrades to nil if
// gh is missing or unauthenticated.
func GithubProjects(r run.Runner) []core.Project {
	out, code, err := r.Run("gh", []string{"repo", "list", "--limit", "100", "--json", "nameWithOwner"}, nil)
	if err != nil || code != 0 {
		return nil
	}
	var repos []struct {
		NameWithOwner string `json:"nameWithOwner"`
	}
	if json.Unmarshal(out, &repos) != nil {
		return nil
	}
	projects := make([]core.Project, 0, len(repos))
	for _, rp := range repos {
		if rp.NameWithOwner == "" {
			continue
		}
		projects = append(projects, core.Project{
			Label:  rp.NameWithOwner + " (github)",
			Repo:   rp.NameWithOwner,
			Origin: "github",
		})
	}
	return projects
}

// ProjectsFromAgents derives suggestions from the repos of agents already in
// the inventory, so a repo you're actively working in is offered even if it's
// not under the conventional checkout root.
func ProjectsFromAgents(agents []core.Agent) []core.Project {
	seen := map[string]bool{}
	var out []core.Project
	for _, a := range agents {
		if a.Repo == "" || seen[a.Repo] {
			continue
		}
		seen[a.Repo] = true
		out = append(out, core.Project{Label: a.Repo + " (in use)", Repo: a.Repo, Origin: "agent"})
	}
	return out
}

// parseGitDirs turns `ls -d .../*/.git` output into projects: each line is a
// <dir>/.git path; we strip /.git and label by the leaf directory name.
func parseGitDirs(out, origin string) []core.Project {
	var projects []core.Project
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dir := strings.TrimSuffix(line, "/.git")
		if dir == line { // no /.git suffix — not a checkout line
			continue
		}
		projects = append(projects, core.Project{
			Label:  filepath.Base(dir) + " (" + origin + ")",
			Repo:   dir,
			Origin: origin,
		})
	}
	return projects
}

// shQuote single-quotes s for safe interpolation into an sh -c command (the base
// dir comes from config, not user input, but quoting keeps a path with spaces
// or metacharacters inert).
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
