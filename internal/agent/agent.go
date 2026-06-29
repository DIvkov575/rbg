// Package agent implements the desktop-side rbg-agent verbs. It owns session
// state, resolves claude sessions, serializes sends with a file lock, and
// streams transcripts. It is exec'd directly by sshd — never via a shell.
package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/render"
	"github.com/divkov575/rbg/internal/slug"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// SpawnFunc starts a detached child process whose stdout is redirected to
// stdoutPath, returning its pid. The real impl sets a new process group so the
// child outlives the SSH session (the tmux-detachment replacement).
type SpawnFunc func(name string, args []string, stdoutPath string) (pid int, err error)

// Agent holds the agent's injectable dependencies.
type Agent struct {
	Runner     run.Runner
	StatePath  string        // ~/.rbg-agent/sessions.json
	ClaudeHome string        // root for transcript paths (~ in prod)
	Now        func() string // timestamp source (injectable for tests)
	Spawn      SpawnFunc     // detached child spawner (defaults via DefaultSpawn)
	LockDir    string        // dir for per-session lockfiles (defaults beside state)
}

const claudeBin = "claude"

// transcriptPath derives the JSONL path for a claude session id. In v2 the
// agent owns this mapping, so no glob is ever needed.
func (a *Agent) transcriptPath(claudeSessionID string) string {
	return filepath.Join(a.ClaudeHome, ".claude", "projects", "sim-project", claudeSessionID+".jsonl")
}

// resolveName picks the agent's id: the explicit name if given, else a slug of
// the task; then dedups against the existing store by appending -2, -3, …
func resolveName(store *session.Store, explicit, task string) string {
	base := explicit
	if base == "" {
		base = slug.FromTask(task)
	}
	if _, taken := store.Get(base); !taken {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if _, taken := store.Get(cand); !taken {
			return cand
		}
	}
}

// Launch starts a --bg claude agent, resolves its session id, records it, and
// prints {"id","claudeSessionId"} as JSON.
func (a *Agent) Launch(out io.Writer, name, task string) int {
	_, code, _ := a.Runner.Run(claudeBin, claudecli.BGArgs(name, task), nil)
	if code != 0 {
		fmt.Fprintf(out, "rbg-agent: claude --bg failed (exit %d)\n", code)
		return 1
	}
	listing, _, _ := a.Runner.Run(claudeBin, claudecli.AgentsListArgs(), nil)
	agents, _ := claudecli.ParseAgents(listing)
	sid := claudecli.FindSessionID(agents, name)
	if sid == "" {
		fmt.Fprintf(out, "rbg-agent: could not resolve session id for %q\n", name)
		return 1
	}
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	store.Add(session.Session{
		Name:            name,
		ClaudeSessionID: sid,
		TranscriptPath:  a.transcriptPath(sid),
		StartedAt:       a.Now(),
	})
	if err := store.Save(); err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"id": name, "claudeSessionId": sid})
	return 0
}

// Ls prints all recorded sessions as a JSON array.
func (a *Agent) Ls(out io.Writer) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	list := make([]session.Session, 0, len(store.Sessions))
	for _, s := range store.Sessions {
		list = append(list, s)
	}
	json.NewEncoder(out).Encode(list)
	return 0
}

// lockPath returns the lockfile path for a session id.
func (a *Agent) lockPath(id string) string {
	dir := a.LockDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(a.StatePath), "locks")
	}
	return filepath.Join(dir, id+".lock")
}

// Send acquires the per-session lock (busy → exit 3), then spawns a detached
// headless claude resume that appends to the transcript, and returns at once.
func (a *Agent) Send(out io.Writer, name, task string) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	sess, ok := store.Get(name)
	if !ok {
		fmt.Fprintf(out, "rbg-agent: unknown agent %q\n", name)
		return 1
	}
	lock, ok, err := session.TryLock(a.lockPath(name))
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(out, "rbg-agent: session %q busy\n", name)
		return 3
	}
	// NOTE: we intentionally release the lock once the child is launched; the
	// child's own run is short and append-only. Holding for the child's full
	// lifetime would require the lock to travel into the detached process.
	defer lock.Unlock()

	spawn := a.Spawn
	if spawn == nil {
		spawn = DefaultSpawn
	}
	args := append([]string{claudeBin}, claudecli.ResumeHeadlessArgs(sess.ClaudeSessionID, task)...)
	_, err = spawn(args[0], args[1:], sess.TranscriptPath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: spawn failed: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"ok": "sent", "id": name})
	return 0
}

// Read renders the session's transcript to out. (Follow mode is handled by the
// client tailing via a separate streaming path; here we emit the full file.)
func (a *Agent) Read(out io.Writer, name string) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	sess, ok := store.Get(name)
	if !ok {
		fmt.Fprintf(out, "rbg-agent: unknown agent %q\n", name)
		return 1
	}
	data, err := os.ReadFile(sess.TranscriptPath)
	if err != nil {
		// no transcript yet → nothing to render, but not an error
		return 0
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		lines = append(lines, ln)
	}
	render.Stream(lines, out)
	return 0
}

// DefaultSpawn starts a detached child in its own process group with stdout
// appended to stdoutPath. The child survives the parent (and the SSH session).
func DefaultSpawn(name string, args []string, stdoutPath string) (int, error) {
	f, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		f.Close()
		return 0, err
	}
	pid := cmd.Process.Pid
	// Reap asynchronously so we don't block; the child is detached regardless.
	go func() { _ = cmd.Wait(); f.Close() }()
	return pid, nil
}
