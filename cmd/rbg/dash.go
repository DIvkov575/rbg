package main

import (
	"fmt"
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/ui"
)

// dash launches the interactive dashboard over the engine-backed UI. It builds
// the same *engine.Engine the scriptable CLI uses, so the dashboard manages the
// exact same reconciled inventory (create/run/send/read/kill/adopt across the
// four lens views).
func dash(cfg *config.Config, r run.Runner) int {
	e, err := buildEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		return 2
	}
	if err := ui.Run(e, ui.DefaultStdio()); err != nil {
		fmt.Fprintf(os.Stderr, "rbg: dashboard: %v\n", err)
		return 1
	}
	return 0
}
