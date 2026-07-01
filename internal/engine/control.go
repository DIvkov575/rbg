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

// Send delivers a follow-up task to a running agent (HLD F4), dispatched to its
// machine. The identity passed to the runner is machine-specific: the desktop
// rbg-agent resolves by NAME, while a local resume needs the SESSION id
// directly. A busy remote agent surfaces host.ErrBusy unchanged.
func (e *Engine) Send(name, task string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if a.Session == "" {
		return fmt.Errorf("send: agent %q has not run yet", name)
	}
	id := a.Name
	if a.Where == core.Local {
		id = a.Session
	}
	return e.pick(a.Where).newRunner(a.Dir).Send(id, task)
}

// Kill stops an agent (HLD F4). A remote agent is stopped via the desktop
// rbg-agent (by name); a local agent is stopped by signalling its tracked child
// pid (the Runner interface can't kill locally — the pid lives in the record).
// A managed agent is then marked Done and persisted; the transcript is kept.
func (e *Engine) Kill(name string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if a.Where == core.Local {
		if a.Pid <= 0 {
			return fmt.Errorf("kill: no recorded pid for local agent %q", name)
		}
		if err := e.killLocal(a.Pid); err != nil {
			return fmt.Errorf("kill: local agent %q: %w", name, err)
		}
	} else {
		if err := e.pick(core.Remote).newRunner(a.Dir).Kill(a.Name); err != nil {
			return fmt.Errorf("kill: remote agent %q: %w", name, err)
		}
	}
	if rec, ok := e.store.Get(name); ok {
		rec.State = core.Done
		e.store.Add(rec)
		if err := e.store.Save(); err != nil {
			return fmt.Errorf("kill: save: %w", err)
		}
	}
	return nil
}
