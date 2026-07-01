// Package engine is rbg's composition layer: it wires the persisted core.Store
// to the host capabilities and exposes whole operations (list, create, read,
// adopt, and — in the control half — run, send, kill) that the CLI and
// dashboard consume. The Engine is the single place that knows how to turn a
// user intent into store updates + host I/O, so its front-ends stay thin.
package engine

import (
	"time"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
	"github.com/divkov575/rbg/internal/run"
)

// machine bundles the host capabilities for one machine: listing agents,
// reading transcripts, git sync, and building a Runner. newRunner is a factory
// (not a fixed Runner) because a local runner bakes the agent's working dir.
type machine struct {
	Source    host.AgentSource
	Tx        host.Transcripts
	Repo      host.Repo
	newRunner func(dir string) host.Runner
}

// Engine composes the store with the local and remote machine capability
// bundles. killLocal stops a local child by pid; now stamps run times. Both are
// injectable for tests.
type Engine struct {
	store     *core.Store
	local     machine
	remote    machine
	killLocal func(pid int) error
	now       func() string
}

// New builds an Engine: it loads (or initializes) the store at storePath and
// wires the real host implementations — local ones for the laptop (home roots
// the local transcript tree) and remote ones for the configured desktop, all
// executing subprocesses through r.
func New(cfg *config.Config, r run.Runner, storePath, home string) (*Engine, error) {
	store, err := core.LoadStore(storePath)
	if err != nil {
		return nil, err
	}
	return &Engine{
		store: store,
		local: machine{
			Source:    host.LocalSource{R: r},
			Tx:        host.LocalTranscripts{Home: home},
			Repo:      host.LocalRepo{R: r},
			newRunner: func(dir string) host.Runner { return host.LocalRunner{Dir: dir} },
		},
		remote: machine{
			Source:    host.RemoteSource{C: cfg, R: r},
			Tx:        host.RemoteTranscripts{C: cfg, R: r},
			Repo:      host.RemoteRepo{C: cfg, R: r},
			newRunner: func(dir string) host.Runner { return host.RemoteRunner{C: cfg, R: r} },
		},
		killLocal: host.KillProcessGroup,
		now:       func() string { return time.Now().UTC().Format(time.RFC3339) },
	}, nil
}

// pick returns the capability bundle for a location — the laptop is just
// another machine, so local and remote operations share one code path.
func (e *Engine) pick(w core.Location) machine {
	if w == core.Local {
		return e.local
	}
	return e.remote
}
