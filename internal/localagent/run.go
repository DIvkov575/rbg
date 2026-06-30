package localagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Exec abstracts running a command in a dir, so Run is unit-testable without
// spawning git/claude. Returns combined output and an error.
type Exec interface {
	Run(name string, args []string, dir string) error
}

// RepoDir resolves a repo (git URL, scp-style, or local path) to its local
// checkout directory. Absolute/relative/~ paths are used as-is; a URL/slug maps
// to ~/workplace/<name>.
func RepoDir(repo string) string {
	if strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, ".") || strings.HasPrefix(repo, "~") {
		return os.ExpandEnv(strings.Replace(repo, "~", "$HOME", 1))
	}
	name := repo
	if i := strings.LastIndexAny(name, "/:"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".git")
	return os.ExpandEnv(filepath.Join("$HOME", "workplace", name))
}

// SyncPlan is the sequence of git steps RunPlan will take for a repo, exposed
// so callers (and tests) can see exactly what will happen — supports the
// "run later once the remote agent syncs" goal: we pull before running.
type SyncPlan struct {
	Dir     string   // resolved local dir
	Clone   bool     // true if the repo must be cloned (no .git present)
	Steps   [][]string // ordered argv (after the program name "git"/"claude")
}

// PlanRun builds the steps to run agent a with the given task (task may be a's
// default). It clones if absent, otherwise pulls (sync), then runs claude. The
// `now` exists check is injected via dirHasGit for testability.
func PlanRun(a Agent, task string, dirHasGit func(dir string) bool) SyncPlan {
	dir := RepoDir(a.Repo)
	p := SyncPlan{Dir: dir}
	if !dirHasGit(dir) {
		p.Clone = true
		p.Steps = append(p.Steps, []string{"git", "clone", a.Repo, dir})
	} else {
		// sync: pick up changes a remote agent pushed before running locally.
		p.Steps = append(p.Steps, []string{"git", "-C", dir, "pull", "--ff-only"})
	}
	p.Steps = append(p.Steps, []string{"claude", "-p", task, "--dangerously-skip-permissions"})
	return p
}

// dirHasGit is the production check for PlanRun.
func dirHasGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Run executes agent a's plan via ex: sync the pinned repo, then run claude in
// it. task overrides a.Task when non-empty; a blank agent with no task errors.
// The claude step is the LAST step and is the caller's to run detached if
// desired; Run executes all steps synchronously via ex.
func Run(ex Exec, a Agent, task string) (SyncPlan, error) {
	if task == "" {
		task = a.Task
	}
	if strings.TrimSpace(task) == "" {
		return SyncPlan{}, fmt.Errorf("agent %q is blank: provide a task to run it", a.Name)
	}
	plan := PlanRun(a, task, dirHasGit)
	for _, step := range plan.Steps {
		prog, rest := step[0], step[1:]
		// git steps run from cwd (they carry -C); claude runs in the repo dir.
		runDir := ""
		if prog == "claude" {
			runDir = plan.Dir
		}
		if err := ex.Run(prog, rest, runDir); err != nil {
			return plan, fmt.Errorf("%s failed: %w", prog, err)
		}
	}
	return plan, nil
}
