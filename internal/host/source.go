// Package host is rbg's capability layer: the I/O behind the pure core domain.
// This file implements AgentSource — reading the live `claude agents` list from
// one machine. LocalSource execs claude directly; RemoteSource runs it over SSH.
// All process execution goes through the run.Runner seam, so the layer is
// testable with run.Recording (no real claude, no real SSH).
package host

import (
	"encoding/json"
	"fmt"

	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// AgentSource lists the live background agents on one machine.
type AgentSource interface {
	List() ([]core.Live, error)
}

// LocalSource lists agents on the laptop by exec-ing the claude binary.
type LocalSource struct {
	R run.Runner
}

// List runs `claude agents --json --all` locally and decodes the result.
func (s LocalSource) List() ([]core.Live, error) {
	out, code, err := s.R.Run("claude", claudecli.AgentsListArgs(), nil)
	if err != nil {
		return nil, fmt.Errorf("local claude agents: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("local claude agents exited %d: %s", code, out)
	}
	return parseLive(out)
}

// RemoteSource lists agents on the desktop by running claude over SSH.
type RemoteSource struct {
	C *config.Config
	R run.Runner
}

// List runs `claude agents --json --all` on the desktop over SSH and decodes it.
// The claude argv is passed as the remote command; sshx quotes it for the
// desktop login shell (which supplies claude's PATH).
func (s RemoteSource) List() ([]core.Live, error) {
	remote := append([]string{"claude"}, claudecli.AgentsListArgs()...)
	sshArgs := sshx.BuildSSHArgs(s.C, remote, sshx.Options{ConnectTimeout: true})
	out, code, err := s.R.Run("ssh", sshArgs, nil)
	if err != nil {
		return nil, fmt.Errorf("remote claude agents: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("remote claude agents exited %d: %s", code, out)
	}
	return parseLive(out)
}

// parseLive decodes a `claude agents --json` array into []core.Live.
func parseLive(out []byte) ([]core.Live, error) {
	var live []core.Live
	if err := json.Unmarshal(out, &live); err != nil {
		return nil, fmt.Errorf("parse claude agents json: %w", err)
	}
	return live, nil
}

var _ AgentSource = LocalSource{}
var _ AgentSource = RemoteSource{}
