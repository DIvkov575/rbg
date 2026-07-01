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
	"github.com/divkov575/rbg/internal/session"
	"github.com/divkov575/rbg/internal/slug"
	"github.com/divkov575/rbg/internal/sshx"
)

// ErrBusy is returned by Send when the target session is already processing a
// send (the desktop rbg-agent signals this with exit code 3).
var ErrBusy = errors.New("agent busy: a send is already running")

// RunResult is what a Launch produces: the resolved agent name, the claude
// session id it was started with, and (for a local launch) the child pid so the
// caller can later stop it. The caller records Name+Session so Reconcile can
// match the record to the live session. Pid is 0 for a remote launch (the
// desktop rbg-agent tracks the pid on its side).
type RunResult struct {
	Name    string
	Session string
	Pid     int
	// Dir is the working directory the agent was actually launched in, resolved
	// to an absolute path. The caller persists it so a later resume (Send) runs
	// in the same directory even if invoked from elsewhere — without this, a
	// repo-less local agent (record Dir="") would resume in whatever cwd the
	// next command happens to run from. Empty when unknown (remote).
	Dir string
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
	// Dir is the desktop working directory the agent should run claude in. When
	// set it overrides the config's default CWD, so a repo-backed agent launches
	// (and resumes) in the checkout that engine.Run pulled — not the config
	// default. Empty falls back to the config CWD.
	Dir string
}

// ssh runs an rbg-agent verb over SSH and returns stdout + exit code. When Dir
// is set, the rbg-agent runs with that as its --cwd (where claude launches),
// so the launch happens in the same directory engine.Run synced.
func (s RemoteRunner) ssh(verb string, verbArgs []string) ([]byte, int, error) {
	c := s.C
	if s.Dir != "" {
		cp := *s.C
		cp.CWD = s.Dir
		c = &cp
	}
	remote := sshx.AgentArgs(c, verb, verbArgs)
	args := sshx.BuildSSHArgs(c, remote, sshx.Options{ConnectTimeout: true})
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
	// LockDir is where per-session send locks live ("" = os.TempDir). A send
	// takes an exclusive flock on <LockDir>/rbg-send-<session>.lock while it
	// spawns, so two sends racing to the same session at once can't both fire —
	// the loser gets ErrBusy, mirroring the desktop rbg-agent's exit-3 guard.
	// NOTE: like rbg-agent, the lock covers only the spawn, not the detached
	// child's full run (the fd can't travel into the detached process), so it
	// serializes overlapping send COMMANDS, not the resumes' transcript writes.
	LockDir string
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
// generate a session id, and spawn detached headless claude with it. It reports
// the resolved absolute working dir so the caller can persist it and pin later
// resumes to the same directory (l.Dir=="" means claude used the process cwd,
// which we resolve to an absolute path).
func (l LocalRunner) Launch(name, task string) (RunResult, error) {
	if name == "" {
		name = slug.FromTask(task)
	}
	sess := core.NewSessionID() // note: `sess`, not `session` — avoid shadowing the session package
	args := append([]string{"claude"}, claudecli.LaunchHeadlessArgs(sess, task)...)
	pid, err := l.spawnFn()(args[0], args[1:], l.logPath(sess), l.Dir)
	if err != nil {
		return RunResult{}, fmt.Errorf("local launch spawn: %w", err)
	}
	dir := l.Dir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	return RunResult{Name: name, Session: sess, Pid: pid, Dir: dir}, nil
}

// lockPath returns the per-session send-lock path.
func (l LocalRunner) lockPath(sess string) string {
	dir := l.LockDir
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "rbg-send-"+sess+".lock")
}

// Send continues a local claude session (identified by its session id) with a
// follow-up task, by spawning a detached headless resume. It first takes an
// exclusive per-session lock; if another send to the same session is mid-spawn
// the lock is held, so this returns ErrBusy — matching RemoteRunner.Send's
// exit-3 busy semantics. The lock is released once the child is spawned (the fd
// can't travel into the detached process), so it serializes overlapping send
// commands but not the resumes' transcript writes — the same trade-off the
// desktop rbg-agent makes.
func (l LocalRunner) Send(sess, task string) error {
	lock, ok, err := session.TryLock(l.lockPath(sess))
	if err != nil {
		return fmt.Errorf("local send lock: %w", err)
	}
	if !ok {
		return ErrBusy
	}
	defer lock.Unlock()

	args := append([]string{"claude"}, claudecli.ResumeHeadlessArgs(sess, task)...)
	_, err = l.spawnFn()(args[0], args[1:], l.logPath(sess), l.Dir)
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
