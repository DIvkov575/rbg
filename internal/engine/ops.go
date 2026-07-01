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
// The returned agent is the persisted record.
func (e *Engine) Create(spec core.Agent) (core.Agent, error) {
	if spec.Name == "" {
		return core.Agent{}, fmt.Errorf("create: name is required")
	}
	if spec.Task == "" {
		return core.Agent{}, fmt.Errorf("create: a task is required (no blank agents)")
	}
	if _, exists := e.store.Get(spec.Name); exists {
		return core.Agent{}, fmt.Errorf("create: agent %q already exists", spec.Name)
	}
	spec.State = core.Held
	spec.Origin = core.Managed
	e.store.Add(spec)
	if err := e.store.Save(); err != nil {
		return core.Agent{}, fmt.Errorf("create: save: %w", err)
	}
	return spec, nil
}
