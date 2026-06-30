package main

import (
	"io"
	"os"

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
		Dispatch: func(it tui.QueueItem) error {
			// clone (or reuse) the repo on the desktop, then launch claude there.
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
