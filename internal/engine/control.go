package engine

import (
	"fmt"

	"github.com/divkov575/rbg/internal/core"
)

// Run launches an agent's task on its machine, sync-first (HLD F5): it pulls the
// agent's repo (if any) before launching so the task runs against current code,
// aborting the run if the pull fails rather than running against wrong code. On
// success it records the live identity from the launch (Session, and Pid for a
// local child), marks the agent Running with a RunAt stamp, and persists. Run
// acts on a stored (managed) record — stage it with Create or adopt it first.
func (e *Engine) Run(name string) error {
	rec, ok := e.store.Get(name)
	if !ok {
		return fmt.Errorf("run: agent %q is not managed (create or adopt it first)", name)
	}
	m := e.pick(rec.Where)

	if rec.Repo != "" {
		if err := m.Repo.Pull(rec.Dir); err != nil {
			return fmt.Errorf("run: sync failed for %q (resolve it, then retry): %w", name, err)
		}
	}

	res, err := m.newRunner(rec.Dir).Launch(rec.Name, rec.Task)
	if err != nil {
		return fmt.Errorf("run: launch %q: %w", name, err)
	}

	rec.Session = res.Session
	rec.Pid = res.Pid
	rec.State = core.Running
	rec.RunAt = e.now()
	e.store.Add(rec)
	if err := e.store.Save(); err != nil {
		return fmt.Errorf("run: save: %w", err)
	}
	return nil
}
