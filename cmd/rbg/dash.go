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
		Launch: func(task string) error {
			// name auto-derived by the agent (empty name).
			client.Launch(cfg, r, io.Discard, "", task)
			return nil
		},
		Kill: func(name string) error {
			client.Kill(cfg, r, io.Discard, name)
			return nil
		},
	}
	if err := tui.Run(deps, tui.DefaultStdio()); err != nil {
		return 1
	}
	return 0
}
