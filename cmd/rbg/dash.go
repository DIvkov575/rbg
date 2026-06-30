package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/divkov575/rbg/internal/client"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/queue"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
	"github.com/divkov575/rbg/internal/tui"
)

// dash launches the interactive dashboard.
func dash(cfg *config.Config, r run.Runner) int {
	deps := tui.Deps{
		Fetch: func() ([]session.Session, error) {
			return client.FetchSessions(cfg, r)
		},
		Transcript: func(name string) (string, error) {
			return client.FetchTranscript(cfg, r, name)
		},
		Attach: func(name string) error {
			// reuse the existing attach path (resolves id + ssh -t claude).
			if code := attach(cfg, r, name); code != 0 {
				return errAttach
			}
			return nil
		},
		Launch: func(dir, task string) error {
			// name auto-derived by the agent (empty name). A chosen dir is
			// applied via a per-call config copy whose CWD → agent --cwd →
			// LaunchDir, reusing the Task A working-dir path.
			c2 := *cfg
			if dir != "" {
				c2.CWD = dir
			}
			client.Launch(&c2, r, io.Discard, "", task)
			return nil
		},
		Kill: func(name string) error {
			client.Kill(cfg, r, io.Discard, name)
			return nil
		},
		Dirs: func(dir string) (string, string, []tui.DirItem, error) {
			listing, err := client.FetchDirs(cfg, r, dir)
			if err != nil {
				return "", "", nil, err
			}
			items := make([]tui.DirItem, len(listing.Entries))
			for i, e := range listing.Entries {
				items[i] = tui.DirItem{Name: e.Name, Path: e.Path}
			}
			return listing.Dir, listing.Parent, items, nil
		},
		MakeDir: func(dir string) (string, error) {
			return client.MakeDir(cfg, r, dir)
		},
		LoadConfig: func() []tui.ConfigField {
			vals := config.ReadConfFileMap(confPath())
			keys := []string{"RBG_HOST", "RBG_CWD", "RBG_SSH", "RBG_AGENT_PATH", "RBG_MUX", "RBG_CONTROL_PATH", "RBG_CONTROL_PERSIST"}
			fields := make([]tui.ConfigField, 0, len(keys))
			for _, k := range keys {
				fields = append(fields, tui.ConfigField{Key: k, Value: vals[k]})
			}
			return fields
		},
		SaveConfig: func(vals map[string]string) error {
			existing := config.ReadConfFileMap(confPath())
			for k, v := range vals {
				if v == "" {
					delete(existing, k)
				} else {
					existing[k] = v
				}
			}
			return config.WriteConfFile(confPath(), existing)
		},
		LoadQueue: func() []tui.QueueItem {
			q, _ := queue.Load(queuePath())
			out := make([]tui.QueueItem, 0, len(q.Items))
			for _, it := range q.Items {
				out = append(out, tui.QueueItem{Prompt: it.Prompt, Repo: it.Repo})
			}
			return out
		},
		QueueAdd: func(it tui.QueueItem) error {
			q, _ := queue.Load(queuePath())
			q.Add(queue.Item{Prompt: it.Prompt, Repo: it.Repo})
			return q.Save()
		},
		QueueRemove: func(i int) error {
			q, _ := queue.Load(queuePath())
			q.Remove(i)
			return q.Save()
		},
		Dispatch: func(it tui.QueueItem, local bool) error {
			if local {
				return dispatchLocal(it)
			}
			// remote: clone (or reuse) the repo on the desktop, then launch there.
			dir, err := client.CloneRepo(cfg, r, it.Repo)
			if err != nil {
				return err
			}
			c2 := *cfg
			c2.CWD = dir
			client.Launch(&c2, r, io.Discard, "", it.Prompt)
			return nil
		},
	}
	if err := tui.Run(deps, tui.DefaultStdio()); err != nil {
		return 1
	}
	return 0
}

// confPath returns the path to the rbg conf file (~/.rbg.conf).
func confPath() string { return os.ExpandEnv("$HOME/.rbg.conf") }

// queuePath returns the path to the client-only queue store (~/.rbg/queue.json).
func queuePath() string { return os.ExpandEnv("$HOME/.rbg/queue.json") }

// localRepoDir resolves a queue item's repo to a local checkout directory.
// A bare name or git URL maps to ~/workplace/<name>; an absolute/relative path
// is used as-is.
func localRepoDir(repo string) string {
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

// dispatchLocal runs the task with the local claude in the repo's local checkout
// (cloning locally if absent), detached so the dashboard returns immediately.
func dispatchLocal(it tui.QueueItem) error {
	dir := localRepoDir(it.Repo)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := exec.Command("git", "clone", it.Repo, dir).Run(); err != nil {
			return fmt.Errorf("local clone failed: %w", err)
		}
	}
	cmd := exec.Command("claude", "-p", it.Prompt, "--dangerously-skip-permissions")
	cmd.Dir = dir
	if devnull, derr := os.Open(os.DevNull); derr == nil {
		cmd.Stdin = devnull
		defer devnull.Close()
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("local claude failed to start: %w", err)
	}
	go func() { _ = cmd.Wait() }() // detached; reap async
	return nil
}
