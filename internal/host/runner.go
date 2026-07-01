package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/divkov575/rbg/internal/agent"
	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/slug"
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

// LocalRunner runs agents on the laptop by spawning detached headless claude,
// mirroring the remote path: it generates the session id client-side so the
// record can carry it, and derives a name from the task when none is given.
type LocalRunner struct {
	// Spawn starts a detached child; defaults to agent.DefaultSpawn. Injectable
	// for tests. stdoutPath is where the child's stdout is logged.
	Spawn agent.SpawnFunc
	// Dir is the working directory claude runs in ("" = process default).
	Dir string
	// LogDir is where a launched child's stdout log is written ("" = os.TempDir).
	LogDir string
}

func (l LocalRunner) spawnFn() agent.SpawnFunc {
	if l.Spawn != nil {
		return l.Spawn
	}
	return agent.DefaultSpawn
}

// logPath returns where a session's stdout log goes. claude keeps its own
// transcript on disk; this is only the child's stdout capture.
func (l LocalRunner) logPath(session string) string {
	dir := l.LogDir
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "rbg-"+session+".log")
}

// Launch starts a new local agent: derive a name (slug from task if empty),
// generate a session id, and spawn detached headless claude with it.
func (l LocalRunner) Launch(name, task string) (RunResult, error) {
	if name == "" {
		name = slug.FromTask(task)
	}
	session := core.NewSessionID()
	args := append([]string{"claude"}, claudecli.LaunchHeadlessArgs(session, task)...)
	_, err := l.spawnFn()(args[0], args[1:], l.logPath(session), l.Dir)
	if err != nil {
		return RunResult{}, fmt.Errorf("local launch spawn: %w", err)
	}
	return RunResult{Name: name, Session: session}, nil
}

// Send continues a local claude session (identified by its session id) with a
// follow-up task, by spawning a detached headless resume.
func (l LocalRunner) Send(session, task string) error {
	args := append([]string{"claude"}, claudecli.ResumeHeadlessArgs(session, task)...)
	_, err := l.spawnFn()(args[0], args[1:], l.logPath(session), l.Dir)
	if err != nil {
		return fmt.Errorf("local send spawn: %w", err)
	}
	return nil
}

// Kill stops a local agent. Local process tracking (pid → kill) lives in the
// Store/CLI layer, so this returns a clear not-implemented error rather than
// silently succeeding; the CLI kills the tracked pid directly.
func (l LocalRunner) Kill(name string) error {
	return fmt.Errorf("local kill not handled by LocalRunner; the CLI stops the tracked pid")
}

var _ Runner = LocalRunner{}
