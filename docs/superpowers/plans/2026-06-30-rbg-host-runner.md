# rbg Host Layer — Runner Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `Runner` capability of rbg's `host` layer: launch a task as a new agent, continue a running agent with a follow-up, and stop an agent — on either machine, behind one interface, always capturing the claude session id so the launched agent can be matched back to its record.

**Architecture:** Layer 2 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.1 Capabilities; §5.6 Actions). The previous slice built `AgentSource`+`Inventory` (read the world). This slice builds `Runner` (change the world): the F1/F2/F4 actions. `LocalRunner` spawns headless `claude` directly; `RemoteRunner` drives the desktop `rbg-agent` over SSH. Both go through the existing `run.Runner` / `SpawnFunc` seams, so the whole capability is unit-testable with no real SSH, no real claude, no real processes. It does NOT touch git sync (that's the `Repo` capability, next plan) — this plan runs the task as-is; sync-first composition happens when the CLI wires Repo+Runner together.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses: `internal/core` (`Agent`, attributes), `internal/run` (`Runner`+`Recording`), `internal/sshx` (SSH argv), `internal/config` (`Config`), `internal/claudecli` (claude argv), `internal/agent` (`DefaultSpawn`, `SpawnFunc`), `internal/slug` (name derivation). No new dependencies.

**Scope of this plan (from HLD §2):**
- **F1** delegate a task (fire-now) — `Launch`.
- **F2** run a held task later — same `Launch` call, invoked on a `State==Held` record; no separate machinery.
- **F4** act on a running agent — `Send` (follow-up) and `Kill`.
- **Not in this plan:** git sync / sync-first (F5, the `Repo` capability); transcript read/transfer (F8, the `Transcripts` capability); persisting records to the Store (the CLI plan wires `Launch`'s returned session id into `core.Store`); the dashboard.

**Verified facts (grounded 2026-06-30):**
- The desktop `rbg-agent launch --task <t> [--name <n>]` generates a client-invisible session id itself and prints `{"id":"<name>","claudeSessionId":"<uuid>"}` on stdout, exit 0 (internal/agent/agent.go:121-158). rbg-agent `send --id <n> --task <t>` returns exit 3 when the session lock is held (busy). `kill --id <n>` terminates any live child and forgets the session, keeping the transcript.
- `sshx.AgentArgs(c, verb, verbArgs)` (internal/sshx/sshx.go:80) builds the remote `rbg-agent <verb> ...` argv; `sshx.BuildSSHArgs(c, remote, opts)` wraps it for ssh. This is exactly what the shipped `client` package uses.
- `claudecli.LaunchHeadlessArgs(sessionID, task)` → `[-p, task, --session-id, sessionID, --dangerously-skip-permissions]`; `ResumeHeadlessArgs(sessionID, task)` → `[-p, task, --resume, sessionID, --dangerously-skip-permissions]` (internal/claudecli/claude.go). Verified against claude v2.1.197/v2.1.187.
- `agent.DefaultSpawn(name, args, stdoutPath, dir) (pid int, err error)` (internal/agent/agent.go:306) starts a DETACHED child in its own process group so it outlives the caller — the local-launch primitive. Its type is `agent.SpawnFunc` (internal/agent/agent.go, the `SpawnFunc` type).
- `slug.FromTask(task) string` derives a `^[a-z0-9-]+$` name, never empty (internal/slug/slug.go:21).
- `run.Recording` records `Calls []Call{Name,Args}` and returns a `Result` by first `BySubstring` match on joined args, else `Default` (internal/run/run.go).
- There is NO client-side UUID generator in a shared package today (only `agent.randomUUID`, unexported). This plan adds one to `core` (session ids are domain identity), used by `LocalRunner`.

---

## File Structure

New files in the existing `internal/host` package, plus one tiny addition to `internal/core`:

- Modify: `internal/core/agent.go` — add `NewSessionID()` (a v4-ish UUID from crypto/rand). Session ids are domain identity (the reconcile key), so the generator belongs in core, not host.
- Modify: `internal/core/agent_test.go` — test `NewSessionID` shape/uniqueness.
- Create: `internal/host/runner.go` — the `Runner` interface, `RunResult`, `LocalRunner`, `RemoteRunner`.
- Create: `internal/host/runner_test.go`

`runner.go` owns "make an agent run / continue / stop on one machine." It sits beside `source.go` (read) and `inventory.go` (compose) as the third host concern. The `Runner` interface is the seam the CLI and a fake both implement.

---

## Task 1: Add NewSessionID to core

**Files:**
- Modify: `internal/core/agent.go`
- Test: `internal/core/agent_test.go`

A launched agent needs a session id that is (a) generated before launch so the record can store it, and (b) the same id claude uses, so `Reconcile` matches record↔live. It is domain identity → lives in `core`.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/agent_test.go` (append; the file already has `package core` and imports `testing` — you will ALSO need `strings`, add it to the import block):

```go
func TestNewSessionIDShapeAndUniqueness(t *testing.T) {
	a := NewSessionID()
	b := NewSessionID()
	if a == b {
		t.Errorf("two ids collided: %q", a)
	}
	// v4-ish UUID: 36 chars, 5 dash-separated groups of 8-4-4-4-12.
	if len(a) != 36 {
		t.Fatalf("len(%q) = %d, want 36", a, len(a))
	}
	groups := strings.Split(a, "-")
	wantLens := []int{8, 4, 4, 4, 12}
	if len(groups) != 5 {
		t.Fatalf("got %d groups in %q, want 5", len(groups), a)
	}
	for i, g := range groups {
		if len(g) != wantLens[i] {
			t.Errorf("group %d = %q (len %d), want len %d", i, g, len(g), wantLens[i])
		}
		for _, r := range g {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Errorf("group %d %q has non-hex rune %q", i, g, r)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestNewSessionID -v`
Expected: FAIL — `undefined: NewSessionID`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/agent.go`: add `"crypto/rand"` and `"fmt"` to the imports (the file currently has none — add an import block), then add:

```go
// NewSessionID returns a fresh v4-ish UUID (crypto/rand), formatted 8-4-4-4-12
// and lowercase-hex — glob- and shell-safe. rbg generates the session id BEFORE
// launching claude (claude -p --session-id <id>), so the launched agent's record
// can carry the id and Reconcile can match the record to the live session later.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestNewSessionID -v && go test ./internal/core/`
Expected: PASS (new test and the whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/core/agent.go internal/core/agent_test.go
git commit -m "feat(core): NewSessionID — client-generated session id for reconcile matching"
```

---

## Task 2: Runner interface, RunResult, and RemoteRunner

**Files:**
- Create: `internal/host/runner.go`
- Test: `internal/host/runner_test.go`

`RemoteRunner` drives the desktop `rbg-agent` over SSH — the proven path. `Launch` parses `rbg-agent`'s `{"id","claudeSessionId"}` reply so the caller learns the session id. `Send`/`Kill` map to the corresponding verbs; `Send` surfaces the busy signal (exit 3).

- [ ] **Step 1: Write the failing test**

Create `internal/host/runner_test.go`:

```go
package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func joined(args []string) string {
	s := ""
	for _, a := range args {
		s += a + " "
	}
	return s
}

func TestRemoteRunnerLaunchParsesSession(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"launch": {Stdout: []byte(`{"id":"fix-bug","claudeSessionId":"sid-42"}`), Code: 0},
	}}
	res, err := RemoteRunner{C: cfg, R: r}.Launch("fix-bug", "fix the bug")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.Name != "fix-bug" || res.Session != "sid-42" {
		t.Errorf("RunResult = %+v, want {fix-bug sid-42}", res)
	}
	// It must go over ssh, invoking the rbg-agent launch verb with the task.
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "launch") || !contains(j, "fix the bug") || !contains(j, "desktop") {
		t.Errorf("ssh args missing launch/task/host: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerLaunchNonZeroErrors(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("boom"), Code: 1}}
	if _, err := (RemoteRunner{C: cfg, R: r}).Launch("n", "t"); err == nil {
		t.Errorf("expected error on non-zero launch exit")
	}
}

func TestRemoteRunnerSendBusyIsError(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 3}} // rbg-agent busy signal
	err := RemoteRunner{C: cfg, R: r}.Send("fix-bug", "more")
	if err == nil {
		t.Fatalf("expected busy error on exit 3")
	}
	if err != ErrBusy {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestRemoteRunnerSendOK(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Send("fix-bug", "more"); err != nil {
		t.Errorf("Send ok: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "send") || !contains(j, "more") {
		t.Errorf("ssh args missing send/task: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerKill(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte(`{"ok":"killed","id":"fix-bug"}`), Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Kill("fix-bug"); err != nil {
		t.Errorf("Kill: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "kill") {
		t.Errorf("ssh args missing kill: %v", r.Calls[0].Args)
	}
}
```

Note: `contains` is already defined in `source_test.go` (same package) — reuse it; do not redefine.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestRemoteRunner -v`
Expected: FAIL — `undefined: RemoteRunner`, `undefined: ErrBusy`, `undefined: RunResult`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/runner.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestRemoteRunner -v`
Expected: PASS (all five RemoteRunner tests).

- [ ] **Step 5: Commit**

```bash
git add internal/host/runner.go internal/host/runner_test.go
git commit -m "feat(host): RemoteRunner — launch/send/kill agents via rbg-agent over SSH"
```

---

## Task 3: LocalRunner

**Files:**
- Modify: `internal/host/runner.go`
- Test: `internal/host/runner_test.go`

`LocalRunner` runs agents on the laptop, mirroring the remote path: it generates the session id client-side (`core.NewSessionID`), derives a name from the task if none given (`slug.FromTask`), and spawns detached headless claude. `Send` spawns a headless resume; `Kill` is a no-op stub returning a clear error (local process tracking is the Store/CLI layer's job, out of scope here — but the method must exist to satisfy the interface).

- [ ] **Step 1: Write the failing test**

Add to `internal/host/runner_test.go`:

```go
// recordingSpawn captures spawn calls in place of agent.DefaultSpawn.
type recordingSpawn struct {
	calls []spawnCall
	pid   int
	err   error
}
type spawnCall struct {
	name string
	args []string
	dir  string
}

func (rs *recordingSpawn) spawn(name string, args []string, stdoutPath, dir string) (int, error) {
	rs.calls = append(rs.calls, spawnCall{name: name, args: args, dir: dir})
	return rs.pid, rs.err
}

func TestLocalRunnerLaunchGeneratesSessionAndSpawns(t *testing.T) {
	sp := &recordingSpawn{pid: 4321}
	lr := LocalRunner{Spawn: sp.spawn, Dir: "/home/me/app"}
	res, err := lr.Launch("", "fix the bug")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// Name derived from task via slug; session id generated (36-char uuid).
	if res.Name == "" {
		t.Errorf("empty name; want slug-derived")
	}
	if len(res.Session) != 36 {
		t.Errorf("Session = %q, want a 36-char uuid", res.Session)
	}
	if len(sp.calls) != 1 {
		t.Fatalf("made %d spawn calls, want 1", len(sp.calls))
	}
	c := sp.calls[0]
	if c.name != "claude" {
		t.Errorf("spawned %q, want claude", c.name)
	}
	if c.dir != "/home/me/app" {
		t.Errorf("spawn dir = %q, want /home/me/app", c.dir)
	}
	// argv must carry the task, --session-id with the returned id, and -p.
	j := joined(c.args)
	if !contains(j, "fix the bug") || !contains(j, "--session-id") || !contains(j, res.Session) {
		t.Errorf("spawn args missing task/session-id: %v", c.args)
	}
}

func TestLocalRunnerLaunchHonorsExplicitName(t *testing.T) {
	sp := &recordingSpawn{pid: 1}
	res, err := (LocalRunner{Spawn: sp.spawn}).Launch("my-name", "do it")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.Name != "my-name" {
		t.Errorf("Name = %q, want my-name", res.Name)
	}
}

func TestLocalRunnerLaunchSpawnErrorPropagates(t *testing.T) {
	sp := &recordingSpawn{err: errFakeSpawn}
	if _, err := (LocalRunner{Spawn: sp.spawn}).Launch("n", "t"); err == nil {
		t.Errorf("expected spawn error to propagate")
	}
}

func TestLocalRunnerSendSpawnsResume(t *testing.T) {
	sp := &recordingSpawn{pid: 2}
	// Local Send needs the session id to resume; it is passed as the name arg's
	// companion — LocalRunner.Send(session, task) resumes that claude session.
	err := (LocalRunner{Spawn: sp.spawn}).Send("sid-9", "next step")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(sp.calls) != 1 {
		t.Fatalf("made %d spawn calls, want 1", len(sp.calls))
	}
	j := joined(sp.calls[0].args)
	if !contains(j, "--resume") || !contains(j, "sid-9") || !contains(j, "next step") {
		t.Errorf("resume args wrong: %v", sp.calls[0].args)
	}
}

var errFakeSpawn = errorString("spawn failed")

type errorString string

func (e errorString) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestLocalRunner -v`
Expected: FAIL — `undefined: LocalRunner`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/host/runner.go`: extend the import block with `"github.com/divkov575/rbg/internal/agent"`, `"github.com/divkov575/rbg/internal/claudecli"`, `"github.com/divkov575/rbg/internal/core"`, `"github.com/divkov575/rbg/internal/slug"`. Then append:

```go
// LocalRunner runs agents on the laptop by spawning detached headless claude,
// mirroring the remote path: it generates the session id client-side so the
// record can carry it, and derives a name from the task when none is given.
type LocalRunner struct {
	// Spawn starts a detached child; defaults to agent.DefaultSpawn. Injectable
	// for tests. stdoutPath is where the child's stdout is logged.
	Spawn agent.SpawnFunc
	// Dir is the working directory claude runs in ("" = process default).
	Dir string
	// LogDir is where a launched child's stdout log is written ("" = os.TempDir
	// via the default; tests inject Spawn so the path is not exercised).
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
```

Also add `"os"` and `"path/filepath"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestLocalRunner -v`
Expected: PASS (all four LocalRunner tests).

- [ ] **Step 5: Commit**

```bash
git add internal/host/runner.go internal/host/runner_test.go
git commit -m "feat(host): LocalRunner — launch/send detached headless claude locally"
```

---

## Task 4: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run host + core suites**

Run: `go test ./internal/host/ ./internal/core/ -v`
Expected: PASS — all Runner + Source + Inventory + core tests.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS — new code compiles, nothing regressed.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/host/ ./internal/core/ && gofmt -l internal/host/ internal/core/`
Expected: vet clean; gofmt lists no files.

- [ ] **Step 4: Commit any fixups**

If Steps 1–3 surfaced fixes, commit them; otherwise skip:

```bash
git add internal/host/ internal/core/
git commit -m "test(host): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **F1 (delegate a task, fire-now):** `LocalRunner.Launch` / `RemoteRunner.Launch`, one `Runner` interface, dispatched by which impl the caller picks (= the agent's `Where`). ✅
- **F2 (run a held task later):** no separate machinery — the CLI calls the same `Launch` on a `State==Held` record when the operator triggers it. This plan provides the `Launch`; the "held" state is a `core` attribute already. ✅ (by design; nothing extra needed here.)
- **F4 (act on a running agent):** `Send` (follow-up) and `Kill`. Remote `Send` surfaces `ErrBusy` (exit 3); remote `Kill` maps to the verb. ✅
- **Session-id capture (reconcile linkage):** both `Launch` impls return `RunResult{Name, Session}`; remote parses rbg-agent's reply, local generates via `core.NewSessionID`. This is what lets the CLI store the id so `core.Reconcile` matches record↔live. ✅
- **Testability (NFR):** everything behind `run.Runner` (remote) and `agent.SpawnFunc` (local); all tests use `run.Recording` / a recording spawn — no real SSH, claude, or processes. ✅
- **Local-is-just-another-machine:** `LocalRunner` and `RemoteRunner` behind one `Runner` interface. ✅

**Known asymmetry (documented, intentional):** `LocalRunner.Kill` returns a not-implemented error because killing a local agent needs the tracked pid, which lives in the Store/CLI layer (the desktop tracks pids in its own session store; the laptop's Store will do the same). The CLI stops a local agent by killing the recorded pid directly. This is called out so a reviewer does not treat it as a gap. `LocalRunner.Send` takes the **session id** (not a name) because locally there is no name→session resolver in this layer; the CLI resolves name→session from the Store before calling. Remote `Send` takes the **name** because rbg-agent resolves it on the desktop.

**Deferred to later plans (not gaps here):** git sync-first (F5, `Repo` capability); transcript read/transfer (F8, `Transcripts`); persisting `RunResult` into `core.Store` and name→session resolution (the CLI plan); the dashboard.

**Type/name consistency:** `Runner`, `RunResult{Name,Session}`, `ErrBusy`, `RemoteRunner{C,R}`, `LocalRunner{Spawn,Dir,LogDir}`, `core.NewSessionID`, `slug.FromTask`, `agent.SpawnFunc`, `agent.DefaultSpawn`, `claudecli.LaunchHeadlessArgs`/`ResumeHeadlessArgs` — used identically across tasks and matching the real signatures verified above. ✅
