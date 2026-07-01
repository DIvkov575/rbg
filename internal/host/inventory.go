package host

import (
	"errors"
	"sync"

	"github.com/divkov575/rbg/internal/core"
)

// Inventory pulls the live agents from both machines and reconciles them with
// rbg's stored records into one inventory (HLD F3/F6). It degrades gracefully:
// if a source fails (e.g. the desktop is unreachable), that machine's agents are
// treated as empty and the failure is returned alongside the still-usable
// inventory built from records plus whatever source(s) did answer. Callers
// should surface a non-nil error to the operator but may still display agents.
func Inventory(records []core.Agent, local, remote AgentSource) ([]core.Agent, error) {
	// The two sources are independent I/O on different machines (a local exec and
	// an SSH round-trip), so probe them concurrently: wall-clock becomes
	// max(local, remote) instead of their sum, which matters when the remote adds
	// network RTT. Each result is captured in its own slot, so there is no shared
	// mutable state between the goroutines.
	var (
		wg                    sync.WaitGroup
		localLive, remoteLive []core.Live
		localErr, remoteErr   error
	)
	wg.Add(2)
	go func() { defer wg.Done(); localLive, localErr = local.List() }()
	go func() { defer wg.Done(); remoteLive, remoteErr = remote.List() }()
	wg.Wait()

	var errs []error
	if localErr != nil {
		errs = append(errs, localErr)
		localLive = nil
	}
	if remoteErr != nil {
		errs = append(errs, remoteErr)
		remoteLive = nil
	}

	agents := core.Reconcile(records, localLive, remoteLive)
	return agents, errors.Join(errs...)
}
