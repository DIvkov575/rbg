package engine

import (
	"fmt"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
)

// List returns the reconciled inventory: rbg's stored records merged with the
// live agents on both machines (HLD F3/F6). It degrades gracefully — an
// unreachable machine yields a non-nil error alongside a still-usable list
// (see host.Inventory).
func (e *Engine) List() ([]core.Agent, error) {
	return host.Inventory(e.store.Records(), e.local.Source, e.remote.Source)
}

// Create stages a delegated task as a held record, to be launched later (HLD
// F2). It forces State=Held and Origin=Managed, requires a non-empty name and
// task (there are no blank agents), and rejects a name already in the store.
// An unset Where defaults to Local (a task with no machine chosen runs on the
// laptop, not silently on the desktop); any other value must be a known
// Location. When the agent is repo-backed and no Dir was given, Create derives
// the working directory from the repo on the target machine (core.RepoDir), so
// Run never launches with an empty dir. The returned agent is the persisted
// record.
func (e *Engine) Create(spec core.Agent) (core.Agent, error) {
	if spec.Name == "" {
		return core.Agent{}, fmt.Errorf("create: name is required")
	}
	if spec.Task == "" {
		return core.Agent{}, fmt.Errorf("create: a task is required (no blank agents)")
	}
	switch spec.Where {
	case "":
		spec.Where = core.Local
	case core.Local, core.Remote:
		// explicit and valid
	default:
		return core.Agent{}, fmt.Errorf("create: invalid location %q (want %q or %q)", spec.Where, core.Local, core.Remote)
	}
	if _, exists := e.store.Get(spec.Name); exists {
		return core.Agent{}, fmt.Errorf("create: agent %q already exists", spec.Name)
	}
	if spec.Repo != "" && spec.Dir == "" {
		m := e.pick(spec.Where)
		spec.Dir = core.RepoDir(m.base, m.home, spec.Repo)
	}
	spec.State = core.Held
	spec.Origin = core.Managed
	e.store.Add(spec)
	if err := e.store.Save(); err != nil {
		return core.Agent{}, fmt.Errorf("create: save: %w", err)
	}
	return spec, nil
}

// find resolves a name to an agent. A managed agent is already fully described
// by its stored record (Name, Session, Where, Dir, Pid, Origin) — everything
// Read/Send/Kill need — so we resolve it from the store directly and skip the
// two-machine reconcile (which for the remote means an SSH round-trip per
// command). Only a name absent from the store might be a Foreign agent, which
// exists solely in live reality, so that case falls back to the full inventory.
// A store hit is always Managed, so Adopt (which requires Foreign) still
// correctly rejects an already-managed name.
func (e *Engine) find(name string) (core.Agent, error) {
	if rec, ok := e.store.Get(name); ok {
		return rec, nil
	}
	agents, err := e.List()
	// Note: err may be a degradation error; the inventory is still usable, so we
	// search it and only surface err if the agent isn't found.
	for _, a := range agents {
		if a.Name == name {
			return a, nil
		}
	}
	if err != nil {
		return core.Agent{}, fmt.Errorf("agent %q not found (inventory degraded: %w)", name, err)
	}
	return core.Agent{}, fmt.Errorf("agent %q not found", name)
}

// Read returns an agent's raw transcript bytes (HLD F8), read from whichever
// machine the agent lives on. An agent that has never run (no session) has no
// transcript.
func (e *Engine) Read(name string) ([]byte, error) {
	a, err := e.find(name)
	if err != nil {
		return nil, err
	}
	if a.Session == "" {
		return nil, fmt.Errorf("agent %q has not run yet (no transcript)", name)
	}
	return e.pick(a.Where).Tx.Read(a.Session)
}

// Adopt takes a foreign agent (discovered live on a machine but absent from
// rbg's records) under management and persists it, so rbg tracks it going
// forward (HLD F3). Adopting an agent that is already managed is an error.
func (e *Engine) Adopt(name string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if !a.IsForeign() {
		return fmt.Errorf("agent %q is already managed", name)
	}
	e.store.Add(core.Adopt(a))
	if err := e.store.Save(); err != nil {
		return fmt.Errorf("adopt: save: %w", err)
	}
	return nil
}
