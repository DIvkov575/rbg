package host

import (
	"errors"

	"github.com/divkov575/rbg/internal/core"
)

// Inventory pulls the live agents from both machines and reconciles them with
// rbg's stored records into one inventory (HLD F3/F6). It degrades gracefully:
// if a source fails (e.g. the desktop is unreachable), that machine's agents are
// treated as empty and the failure is returned alongside the still-usable
// inventory built from records plus whatever source(s) did answer. Callers
// should surface a non-nil error to the operator but may still display agents.
func Inventory(records []core.Agent, local, remote AgentSource) ([]core.Agent, error) {
	var errs []error

	localLive, err := local.List()
	if err != nil {
		errs = append(errs, err)
		localLive = nil
	}
	remoteLive, err := remote.List()
	if err != nil {
		errs = append(errs, err)
		remoteLive = nil
	}

	agents := core.Reconcile(records, localLive, remoteLive)
	return agents, errors.Join(errs...)
}
