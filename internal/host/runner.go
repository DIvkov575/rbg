package host

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// ErrBusy is returned by Send when the target session is already processing a
// send (the desktop rbg-agent signals this with exit code 3).
var ErrBusy = errors.New("agent busy: a send is already running")

// RunResult is what a Launch produces: the resolved agent name and the claude
// session id it was started with. The caller records these so Reconcile can
// later match the record to the live session.
type RunResult struct {
	Name    string
	Session string
}

// Runner launches, continues, and stops agents on one machine. LocalRunner acts
// on the laptop; RemoteRunner drives the desktop rbg-agent over SSH.
type Runner interface {
	Launch(name, task string) (RunResult, error)
	Send(name, task string) error
	Kill(name string) error
}

// remoteExitBusy is the desktop rbg-agent's exit code for a busy session.
const remoteExitBusy = 3

// RemoteRunner runs agents on the desktop via rbg-agent over SSH.
type RemoteRunner struct {
	C *config.Config
	R run.Runner
}

// ssh runs an rbg-agent verb over SSH and returns stdout + exit code.
func (s RemoteRunner) ssh(verb string, verbArgs []string) ([]byte, int, error) {
	remote := sshx.AgentArgs(s.C, verb, verbArgs)
	args := sshx.BuildSSHArgs(s.C, remote, sshx.Options{ConnectTimeout: true})
	return s.R.Run("ssh", args, nil)
}

// Launch starts a new agent on the desktop and returns its name + session id,
// parsed from rbg-agent's {"id","claudeSessionId"} reply. An empty name lets the
// agent derive one from the task.
func (s RemoteRunner) Launch(name, task string) (RunResult, error) {
	verbArgs := []string{"--task", task}
	if name != "" {
		verbArgs = append([]string{"--name", name}, verbArgs...)
	}
	out, code, err := s.ssh("launch", verbArgs)
	if err != nil {
		return RunResult{}, fmt.Errorf("remote launch: %w", err)
	}
	if code != 0 {
		return RunResult{}, fmt.Errorf("remote launch exited %d: %s", code, out)
	}
	var reply struct {
		ID              string `json:"id"`
		ClaudeSessionID string `json:"claudeSessionId"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return RunResult{}, fmt.Errorf("parse launch reply: %w", err)
	}
	return RunResult{Name: reply.ID, Session: reply.ClaudeSessionID}, nil
}

// Send delivers a follow-up task to a running agent. A busy session (exit 3)
// becomes ErrBusy so the caller can distinguish it from a real failure.
func (s RemoteRunner) Send(name, task string) error {
	out, code, err := s.ssh("send", []string{"--id", name, "--task", task})
	if err != nil {
		return fmt.Errorf("remote send: %w", err)
	}
	if code == remoteExitBusy {
		return ErrBusy
	}
	if code != 0 {
		return fmt.Errorf("remote send exited %d: %s", code, out)
	}
	return nil
}

// Kill forgets an agent on the desktop, terminating any live child (transcript
// is kept by rbg-agent).
func (s RemoteRunner) Kill(name string) error {
	out, code, err := s.ssh("kill", []string{"--id", name})
	if err != nil {
		return fmt.Errorf("remote kill: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("remote kill exited %d: %s", code, out)
	}
	return nil
}

var _ Runner = RemoteRunner{}
