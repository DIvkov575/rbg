// Package agent implements the desktop-side rbg-agent verbs. It owns session
// state, resolves claude sessions, serializes sends with a file lock, and
// streams transcripts. It is exec'd directly by sshd — never via a shell.
package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/render"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
	"github.com/divkov575/rbg/internal/slug"
)

// SpawnFunc starts a detached child process whose stdout is redirected to
// stdoutPath, returning its pid. The real impl sets a new process group so the
// child outlives the SSH session (the tmux-detachment replacement).
type SpawnFunc func(name string, args []string, stdoutPath, dir string) (pid int, err error)

// Agent holds the agent's injectable dependencies.
type Agent struct {
	Runner     run.Runner
	StatePath  string              // ~/.rbg-agent/sessions.json
	ClaudeHome string              // root for transcript paths (~ in prod)
	Now        func() string       // timestamp source (injectable for tests)
	NewID      func() string       // session-id generator (injectable for tests)
	Spawn      SpawnFunc           // detached child spawner (defaults via DefaultSpawn)
	KillProc   func(pid int) error // terminate a process group (defaults to defaultKill)
	LockDir    string              // dir for per-session lockfiles (defaults beside state)
	LaunchDir  string              // cwd to run claude in ("" = agent default)
}

const claudeBin = "claude"

// newID returns a fresh session id, using the injected generator if present.
func (a *Agent) newID() string {
	if a.NewID != nil {
		return a.NewID()
	}
	return randomUUID()
}

// randomUUID generates a v4-ish UUID from crypto/rand (glob- and shell-safe).
func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// findTranscript locates a session's transcript JSONL by its unique id, globbing
// across all ~/.claude/projects/*/ dirs. The real claude names each project dir
// after its working directory, which the agent cannot predict, so a fixed path
// is unreliable; the session id (a UUID) is the stable key. Returns "" if not
// found or if the id is not glob-safe.
func (a *Agent) findTranscript(claudeSessionID string) string {
	if !validSessionID(claudeSessionID) {
		return ""
	}
	pattern := filepath.Join(a.ClaudeHome, ".claude", "projects", "*", claudeSessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// validSessionID guards the glob pattern: claude session ids are UUID-shaped
// ([A-Za-z0-9-]). Reject anything else so the pattern stays a literal id.
func validSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

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
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	resolved := resolveName(store, name, task)

	// Client-generated session id: claude -p --session-id creates a resumable
	// session with NO resident background process, so later sends (-p --resume)
	// append cleanly. (A --bg launch leaves a live agent that locks the session.)
	sid := a.newID()

	spawn := a.Spawn
	if spawn == nil {
		spawn = DefaultSpawn
	}
	args := append([]string{claudeBin}, claudecli.LaunchHeadlessArgs(sid, task)...)
	pid, err := spawn(args[0], args[1:], a.sendLogPath(resolved), a.LaunchDir)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: launch spawn failed: %v\n", err)
		return 1
	}
	store.Add(session.Session{
		Name:            resolved,
		ClaudeSessionID: sid,
		TranscriptPath:  a.transcriptPath(sid),
		PID:             pid,
		StartedAt:       a.Now(),
		Dir:             a.LaunchDir,
	})
	if err := store.Save(); err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"id": resolved, "claudeSessionId": sid})
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
	sortSessions(list)
	json.NewEncoder(out).Encode(list)
	return 0
}

// sendLogPath returns an agent-owned path for a headless send child's stdout.
// The real claude maintains its own transcript on disk (located by Read via
// session-id glob), so the child's stdout is just a log we keep beside our
// state, not the transcript itself.
func (a *Agent) sendLogPath(id string) string {
	return filepath.Join(filepath.Dir(a.StatePath), "logs", id+".log")
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
	pid, err := spawn(args[0], args[1:], a.sendLogPath(name), a.LaunchDir)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: spawn failed: %v\n", err)
		return 1
	}
	sess.PID = pid
	store.Add(sess) // persist the new child's pid for a later kill
	_ = store.Save()
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
	path := a.findTranscript(sess.ClaudeSessionID)
	if path == "" {
		path = sess.TranscriptPath // fall back to the recorded path
	}
	data, err := os.ReadFile(path)
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

// Kill forgets an agent: it terminates the recorded child's process group if
// one is still alive, removes the session from the store, and KEEPS the
// transcript .jsonl on disk. Unknown agent → exit 1.
func (a *Agent) Kill(out io.Writer, name string) int {
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
	if sess.PID > 0 {
		kill := a.KillProc
		if kill == nil {
			kill = defaultKill
		}
		// Best-effort: the child is detached and often already exited.
		_ = kill(sess.PID)
	}
	store.Delete(name)
	if err := store.Save(); err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"ok": "killed", "id": name})
	return 0
}

// defaultKill sends SIGTERM to the process GROUP of pid (negative pid), matching
// the Setsid'd child spawned by DefaultSpawn, so the whole detached job dies.
func defaultKill(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// DefaultSpawn starts a detached child in its own process group with stdout
// appended to stdoutPath. The child survives the parent (and the SSH session).
func DefaultSpawn(name string, args []string, stdoutPath, dir string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = f
	cmd.Stderr = f
	// Detached child has no stdin; claude waits ~3s for stdin otherwise.
	if devnull, derr := os.Open(os.DevNull); derr == nil {
		cmd.Stdin = devnull
		defer devnull.Close()
	}
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

// dirEntry is one subdirectory in a Lsdir listing.
type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// dirListing is the JSON shape emitted by Lsdir: the resolved dir, its parent,
// and the visible subdirectories within it (sorted, dotfiles skipped).
type dirListing struct {
	Dir     string     `json:"dir"`
	Parent  string     `json:"parent"`
	Entries []dirEntry `json:"entries"`
}

// resolveLsdirBase resolves the directory Lsdir should list: an empty dir means
// the agent's LaunchDir if set, else the user's home; a relative dir resolves
// against home; an absolute dir is used as-is.
func (a *Agent) resolveLsdirBase(dir string) (string, error) {
	home, herr := os.UserHomeDir()
	if dir == "" {
		if a.LaunchDir != "" {
			return a.LaunchDir, nil
		}
		if herr != nil {
			return "", herr
		}
		return home, nil
	}
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	if herr != nil {
		return "", herr
	}
	return filepath.Join(home, dir), nil
}

// Lsdir lists the subdirectories of dir as JSON for the dashboard's directory
// browser. Only directories are emitted (files skipped), dotfiles are skipped,
// and entries are sorted by name. The parent dir is reported separately so the
// browser can always navigate up. A read failure prints a JSON error object and
// returns 1.
func (a *Agent) Lsdir(out io.Writer, dir string) int {
	base, err := a.resolveLsdirBase(dir)
	if err != nil {
		json.NewEncoder(out).Encode(map[string]string{"error": err.Error()})
		return 1
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		json.NewEncoder(out).Encode(map[string]string{"error": err.Error()})
		return 1
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		json.NewEncoder(out).Encode(map[string]string{"error": err.Error()})
		return 1
	}
	listing := dirListing{
		Dir:     abs,
		Parent:  filepath.Dir(abs),
		Entries: []dirEntry{},
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		listing.Entries = append(listing.Entries, dirEntry{
			Name: name,
			Path: filepath.Join(abs, name),
		})
	}
	sort.Slice(listing.Entries, func(i, j int) bool {
		return listing.Entries[i].Name < listing.Entries[j].Name
	})
	json.NewEncoder(out).Encode(listing)
	return 0
}

// parseStartedAt parses an RFC3339 timestamp, returning the zero time for
// empty or unparseable input so such sessions sort as oldest.
func parseStartedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// sortSessions orders sessions for a stable, meaningful Ls listing. Agents are
// grouped by working directory; the group containing the most-recently-created
// agent comes first, and within a group agents are newest-first. Ties break on
// Name ascending (within a group) or Dir ascending (between groups) so the
// order is fully deterministic regardless of map iteration order.
func sortSessions(list []session.Session) {
	// Per-Dir newest StartedAt, used as the primary (group) sort key.
	groupNewest := map[string]time.Time{}
	for _, s := range list {
		t := parseStartedAt(s.StartedAt)
		if cur, ok := groupNewest[s.Dir]; !ok || t.After(cur) {
			groupNewest[s.Dir] = t
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		// Primary: group's newest StartedAt, descending.
		ga, gb := groupNewest[a.Dir], groupNewest[b.Dir]
		if !ga.Equal(gb) {
			return ga.After(gb)
		}
		// Group tie-break: Dir ascending.
		if a.Dir != b.Dir {
			return a.Dir < b.Dir
		}
		// Within a group: StartedAt descending (newest first).
		ta, tb := parseStartedAt(a.StartedAt), parseStartedAt(b.StartedAt)
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		// Final tie-break: Name ascending.
		return a.Name < b.Name
	})
}
