package main

import (
	"io"

	"github.com/divkov575/rbg/internal/client"
	"github.com/divkov575/rbg/internal/config"
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
	}
	if err := tui.Run(deps, tui.DefaultStdio()); err != nil {
		return 1
	}
	return 0
}
