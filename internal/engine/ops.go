package engine

import (
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
