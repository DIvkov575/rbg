# rbg Engine — Run & Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete rbg's `Engine` with the *control* operations: `Run` (sync-first launch of a staged/any agent, recording its live identity), `Send` (follow-up to a running agent), and `Kill` (stop an agent). This finishes the composition layer so the CLI and dashboard can drive real work on both machines.

**Architecture:** Layer 3 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.5 sync-first, §5.6 actions). The observe/stage half (`List`/`Create`/`Read`/`Adopt`, the `machine` bundle, `New`, `pick`) is merged. This slice extends the bundle with the run/git capabilities and adds the three control ops. It resolves the integration points earlier reviews flagged: `LocalRunner` bakes a per-agent working dir (so the bundle carries a `newRunner(dir)` factory, not a shared runner); local `Kill` needs the child pid (so `core.Agent` gains a `Pid` field, populated on local `Run`); and `Send`/`Kill` are asymmetric local-vs-remote (the engine resolves this by dispatching on `Where`). Still testable with fakes — no real SSH/claude/processes.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses `internal/core`, `internal/host` (`Runner`/`RunResult`/`ErrBusy`, `Repo`, `LocalRunner`/`RemoteRunner`, `LocalRepo`/`RemoteRepo`), `internal/config`, `internal/run`, `internal/agent` (`DefaultSpawn`, and the process-group kill pattern). One tiny new host primitive (`KillProcessGroup`). No new dependencies.

**Scope of this plan (from HLD §2):**
- **F1/F2** delegate & launch (fire-now, or launch a held record later): `Run`, sync-first.
- **F4** act on a running agent: `Send`, `Kill`.
- **F5** sync-first: `Run` pulls the agent's repo before launching, aborting on a sync failure rather than running against wrong code.
- **Not in this plan:** the scriptable CLI wiring these onto `rbg` verbs (next phase), the dashboard, transcript mirroring, and any change to the shipped `cmd/rbg` binary.

**Verified facts (grounded 2026-07-01):**
- `host.Runner` interface: `Launch(name, task string) (RunResult, error)`, `Send(name, task string) error`, `Kill(name string) error`. `RunResult{Name, Session string; Pid int}`. `host.ErrBusy` is the exit-3 busy sentinel (internal/host/runner.go).
- `host.LocalRunner{Spawn agent.SpawnFunc; Dir string; LogDir string}` — `Dir` is per-agent, baked at construction. `LocalRunner.Launch` returns `RunResult{Name,Session,Pid}` (Pid from the spawned child). `LocalRunner.Send(session, task)` takes the SESSION id. `LocalRunner.Kill` is a deliberate not-implemented stub. `RemoteRunner{C,R}` — `Send(name,...)` takes the NAME; `Kill(name)` works (rbg-agent kills the desktop child).
- `host.Repo` interface: `Status(dir string) (core.Sync, error)`, `Pull(dir string) error`. `LocalRepo{R}`, `RemoteRepo{C,R}`.
- `core.Agent` currently has NO `Pid` field (fields: Name/Repo/Dir/Task/Session/Where/State/Origin/Sync/RunAt). `core.Reconcile` preserves a managed record's fields (overwriting only Where/State, and Dir-if-empty), so a persisted `Pid` survives reconcile.
- The shipped desktop kill (`internal/agent/agent.go:300`) is `syscall.Kill(-pid, syscall.SIGTERM)` — SIGTERM to the process GROUP (matching `DefaultSpawn`, which starts the child in its own group). This is the local-kill primitive to reuse.
- The engine's `machine{Source, Tx}`, `Engine{store, local, remote}`, `New`, `pick`, `List`, `find` (resolves against the reconciled inventory), `Create`, `Read`, `Adopt` are all present and tested (observe/stage slice).

---

## File Structure

- Modify: `internal/core/agent.go` — add a `Pid int` field to `Agent` (with json tag) for local process tracking.
- Modify: `internal/core/agent_test.go` — extend the JSON round-trip test to cover `Pid`.
- Create: `internal/host/kill.go` — `KillProcessGroup(pid int) error` (the SIGTERM-to-group primitive, exported for the engine).
- Create: `internal/host/kill_test.go`
- Modify: `internal/engine/engine.go` — extend `machine` with `Repo` + `newRunner(dir) host.Runner`; wire them in `New`; add an injectable `killLocal` on `Engine`.
- Modify: `internal/engine/engine_test.go` — update the `New` wiring assertions + fakes.
- Create: `internal/engine/control.go` — `Run`, `Send`, `Kill`.
- Create: `internal/engine/control_test.go`

`control.go` sits beside `ops.go`; the bundle/factory extension stays in `engine.go` (the single wiring point).

---

## Task 1: Add Pid to core.Agent

**Files:**
- Modify: `internal/core/agent.go`
- Test: `internal/core/agent_test.go`

Local agents are killed by pid (the desktop tracks its own; the laptop must remember the child it spawned). The record carries it.

- [ ] **Step 1: Write the failing test**

In `internal/core/agent_test.go`, find `TestAgentJSONRoundTrip`. Add a `Pid` field to the `Agent` literal it builds (so the round-trip asserts Pid survives). Change the struct literal in that test from ending with `RunAt:   "2026-06-30T12:00:00Z",` to include a Pid — i.e. add this line inside the literal (after the `RunAt` line, before the closing `}`):

```go
		Pid:     4321,
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestAgentJSONRoundTrip -v`
Expected: FAIL — `unknown field 'Pid' in struct literal of type Agent`.

- [ ] **Step 3: Write minimal implementation**

In `internal/core/agent.go`, add a `Pid` field to the `Agent` struct, right after the `RunAt` field:

```go
	RunAt   string    `json:"runAt"` // RFC3339 of last run ("" = never)
	Pid     int       `json:"pid"`   // local child pid for kill ("" remote: 0; the desktop tracks its own)
```

(The `RunAt` line already exists — add only the `Pid` line beneath it.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestAgentJSONRoundTrip -v && go test ./internal/core/`
Expected: PASS (round-trip now includes Pid; whole package green).

- [ ] **Step 5: Commit**

```bash
git add internal/core/agent.go internal/core/agent_test.go
git commit -m "feat(core): Agent.Pid — track local child pid for kill"
```

---

## Task 2: host.KillProcessGroup primitive

**Files:**
- Create: `internal/host/kill.go`
- Test: `internal/host/kill_test.go`

The engine kills a local agent by sending SIGTERM to the child's process group (the child is spawned in its own group by `agent.DefaultSpawn`). This primitive lives in host (the I/O layer), symmetric with `agent.DefaultSpawn`.

- [ ] **Step 1: Write the failing test**

Create `internal/host/kill_test.go`:

```go
package host

import (
	"os/exec"
	"testing"
	"time"
)

func TestKillProcessGroupStopsAChild(t *testing.T) {
	// Start a real, long-sleeping child in its own process group, then kill it.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = newProcGroupAttr() // sets Setpgid so the child leads its group
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if err := KillProcessGroup(cmd.Process.Pid); err != nil {
		t.Fatalf("KillProcessGroup: %v", err)
	}
	select {
	case <-done: // child exited (killed) — success
	case <-time.After(5 * time.Second):
		t.Fatalf("child was not killed within 5s")
	}
}

func TestKillProcessGroupInvalidPidErrors(t *testing.T) {
	// pid 0 / negative are not valid targets for this helper.
	if err := KillProcessGroup(0); err == nil {
		t.Errorf("expected error for pid 0")
	}
}
```

Note: `newProcGroupAttr` is a tiny test helper you will add in the impl file's platform sense — actually define it in the test via `syscall`. Replace the `cmd.SysProcAttr = newProcGroupAttr()` line with a direct construction so no helper is needed:

```go
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```

and add `"syscall"` to the test imports. (Update the test accordingly before running.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestKillProcessGroup -v`
Expected: FAIL — `undefined: KillProcessGroup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/kill.go`:

```go
package host

import (
	"fmt"
	"syscall"
)

// KillProcessGroup sends SIGTERM to the process GROUP led by pid. rbg's local
// children are spawned in their own process group (agent.DefaultSpawn sets
// Setpgid), so signalling the negative pid terminates the child and any
// grandchildren it started. pid must be positive.
func KillProcessGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	return syscall.Kill(-pid, syscall.SIGTERM)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestKillProcessGroup -v`
Expected: PASS (both tests; the real child is terminated).

- [ ] **Step 5: Commit**

```bash
git add internal/host/kill.go internal/host/kill_test.go
git commit -m "feat(host): KillProcessGroup — SIGTERM a local child's process group"
```

---

## Task 3: Extend the machine bundle (Repo + newRunner) and killLocal

**Files:**
- Modify: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

The control ops need a `Repo` (sync-first) and a `Runner` per machine. Because `LocalRunner` bakes a per-agent `Dir`, the bundle carries a `newRunner(dir) host.Runner` factory rather than a fixed runner. The engine also holds an injectable `killLocal` (defaults to `host.KillProcessGroup`).

- [ ] **Step 1: Write the failing test**

In `internal/engine/engine_test.go`, update `TestNewWiresRealHostImpls` to also assert the new wiring. After the existing four type-assertions, add:

```go
	if _, ok := e.local.Repo.(host.LocalRepo); !ok {
		t.Errorf("local.Repo = %T, want host.LocalRepo", e.local.Repo)
	}
	if _, ok := e.remote.Repo.(host.RemoteRepo); !ok {
		t.Errorf("remote.Repo = %T, want host.RemoteRepo", e.remote.Repo)
	}
	// newRunner builds the right Runner type per machine.
	if _, ok := e.local.newRunner("/some/dir").(host.LocalRunner); !ok {
		t.Errorf("local.newRunner = %T, want host.LocalRunner", e.local.newRunner("/some/dir"))
	}
	if _, ok := e.remote.newRunner("/some/dir").(host.RemoteRunner); !ok {
		t.Errorf("remote.newRunner = %T, want host.RemoteRunner", e.remote.newRunner("/some/dir"))
	}
	// local runner must carry the requested dir.
	if lr := e.local.newRunner("/some/dir").(host.LocalRunner); lr.Dir != "/some/dir" {
		t.Errorf("local.newRunner(dir).Dir = %q, want /some/dir", lr.Dir)
	}
	// killLocal defaults to a non-nil killer.
	if e.killLocal == nil {
		t.Errorf("killLocal should default to a non-nil func")
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestNewWiresRealHostImpls -v`
Expected: FAIL — `e.local.Repo undefined` / `newRunner undefined` / `killLocal undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/engine/engine.go`:

(a) Extend the `machine` struct:

```go
// machine bundles the host capabilities for one machine: listing agents,
// reading transcripts, git sync, and building a Runner. newRunner is a factory
// (not a fixed Runner) because a local runner bakes the agent's working dir.
type machine struct {
	Source    host.AgentSource
	Tx        host.Transcripts
	Repo      host.Repo
	newRunner func(dir string) host.Runner
}
```

(b) Add a `killLocal` field to `Engine`:

```go
// Engine composes the store with the local and remote machine capability
// bundles. killLocal stops a local child by pid (injectable for tests).
type Engine struct {
	store     *core.Store
	local     machine
	remote    machine
	killLocal func(pid int) error
}
```

(c) In `New`, wire the new bundle fields and the killer. Replace the `local`/`remote` machine literals with ones that include `Repo` and `newRunner`, and set `killLocal`:

```go
	cfgCopy := cfg
	rr := r
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
			newRunner: func(dir string) host.Runner { return host.RemoteRunner{C: cfgCopy, R: rr} },
		},
		killLocal: host.KillProcessGroup,
	}, nil
```

(The `cfgCopy`/`rr` locals capture the config and runner for the remote `newRunner` closure; if the linter prefers, capture `cfg`/`r` directly — either is fine as they are not reassigned.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestNewWiresRealHostImpls -v && go test ./internal/engine/`
Expected: PASS (new assertions pass; the observe/stage tests still pass — they construct `machine{Source:...,Tx:...}` literals, and the added fields default to nil, which they don't exercise).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat(engine): extend machine bundle with Repo + newRunner factory; killLocal"
```

---

## Task 4: Run — sync-first launch, record live identity

**Files:**
- Create: `internal/engine/control.go`
- Test: `internal/engine/control_test.go`

`Run` launches an agent's task on its machine, syncing its repo first. It resolves the record from the store (Run acts on a managed record — held or a re-run), pulls the repo if the agent has one (aborting on failure — sync-first), launches via the per-dir runner, then records the returned Session/Pid, sets `State=Running` and `RunAt`, and persists.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/control_test.go`:

```go
package engine

import (
	"errors"
	"testing"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
)

// fakeRepo is a canned host.Repo capturing whether Pull ran.
type fakeRepo struct {
	pulled  *string // if non-nil, Pull stores the dir it was asked to pull
	pullErr error
	status  core.Sync
}

func (f fakeRepo) Status(dir string) (core.Sync, error) { return f.status, nil }
func (f fakeRepo) Pull(dir string) error {
	if f.pulled != nil {
		*f.pulled = dir
	}
	return f.pullErr
}

// fakeRunner is a canned host.Runner.
type fakeRunner struct {
	res       host.RunResult
	launchErr error
	sendErr   error
	killErr   error
	launched  *string // captures the name it was asked to launch
	sent      *[2]string
	killed    *string
}

func (f fakeRunner) Launch(name, task string) (host.RunResult, error) {
	if f.launched != nil {
		*f.launched = name
	}
	return f.res, f.launchErr
}
func (f fakeRunner) Send(id, task string) error {
	if f.sent != nil {
		*f.sent = [2]string{id, task}
	}
	return f.sendErr
}
func (f fakeRunner) Kill(name string) error {
	if f.killed != nil {
		*f.killed = name
	}
	return f.killErr
}

// runnerFactory returns a machine.newRunner that always yields r.
func runnerFactory(r host.Runner) func(string) host.Runner {
	return func(string) host.Runner { return r }
}

func TestRunRemoteSyncsThenLaunchesAndRecords(t *testing.T) {
	var pulledDir, launchedName string
	rem := machine{
		Source:    fakeSource{}, // no live agents; the record drives Run
		Repo:      fakeRepo{pulled: &pulledDir},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "job", Session: "S9", Pid: 0}, launched: &launchedName}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{
		Name: "job", Repo: "git@github:me/app", Dir: "/srv/app",
		Task: "do it", Where: core.Remote, State: core.Held, Origin: core.Managed,
	})

	if err := e.Run("job"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pulledDir != "/srv/app" {
		t.Errorf("sync-first: Pull ran on %q, want /srv/app", pulledDir)
	}
	if launchedName != "job" {
		t.Errorf("launched %q, want job", launchedName)
	}
	rec, _ := e.store.Get("job")
	if rec.State != core.Running {
		t.Errorf("State = %q, want running", rec.State)
	}
	if rec.Session != "S9" {
		t.Errorf("Session = %q, want S9 (recorded from launch)", rec.Session)
	}
	if rec.RunAt == "" {
		t.Errorf("RunAt should be set after Run")
	}
}

func TestRunLocalRecordsPid(t *testing.T) {
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "l", Session: "LS", Pid: 5555}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "l", Dir: "/home/me/app", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("l"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec, _ := e.store.Get("l")
	if rec.Pid != 5555 {
		t.Errorf("Pid = %d, want 5555 (recorded for local kill)", rec.Pid)
	}
}

func TestRunAbortsOnSyncFailure(t *testing.T) {
	var launched string
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pullErr: errors.New("would need merge")},
		newRunner: runnerFactory(fakeRunner{launched: &launched}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "j", Repo: "r", Dir: "/srv/j", Task: "t", Where: core.Remote, State: core.Held, Origin: core.Managed})

	if err := e.Run("j"); err == nil {
		t.Errorf("Run should abort when sync-first Pull fails")
	}
	if launched != "" {
		t.Errorf("must NOT launch when sync failed, but launched %q", launched)
	}
	rec, _ := e.store.Get("j")
	if rec.State != core.Held {
		t.Errorf("State = %q, want held (unchanged on sync failure)", rec.State)
	}
}

func TestRunSkipsSyncWhenNoRepo(t *testing.T) {
	var pulled string
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pulled: &pulled},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "n", Session: "S"}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "n", Repo: "", Dir: "/tmp", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("n"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pulled != "" {
		t.Errorf("no repo → no Pull, but pulled %q", pulled)
	}
}

func TestRunUnknownOrUnmanagedErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Run("ghost"); err == nil {
		t.Errorf("running an unknown agent should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestRun -v`
Expected: FAIL — `e.Run undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/control.go`:

```go
package engine

import (
	"fmt"

	"github.com/divkov575/rbg/internal/core"
)

// Run launches an agent's task on its machine, sync-first (HLD F5): it pulls the
// agent's repo (if any) before launching so the task runs against current code,
// aborting the run if the pull fails rather than running against wrong code. On
// success it records the live identity returned by the launch (Session, and Pid
// for a local child), marks the agent Running with a RunAt stamp, and persists.
// Run acts on a stored (managed) record — stage it with Create first, or adopt
// it. now is injected via the store's clock-free design: we stamp RunAt here.
func (e *Engine) Run(name string) error {
	rec, ok := e.store.Get(name)
	if !ok {
		return fmt.Errorf("run: agent %q is not managed (create or adopt it first)", name)
	}
	m := e.pick(rec.Where)

	// Sync-first: pull the repo before running. Skip when the agent has no repo.
	if rec.Repo != "" {
		if err := m.Repo.Pull(rec.Dir); err != nil {
			return fmt.Errorf("run: sync failed for %q (resolve it, then retry): %w", name, err)
		}
	}

	res, err := m.newRunner(rec.Dir).Launch(rec.Name, rec.Task)
	if err != nil {
		return fmt.Errorf("run: launch %q: %w", name, err)
	}

	rec.Session = res.Session
	rec.Pid = res.Pid
	rec.State = core.Running
	rec.RunAt = e.now()
	e.store.Add(rec)
	if err := e.store.Save(); err != nil {
		return fmt.Errorf("run: save: %w", err)
	}
	return nil
}
```

Also add a `now` helper to the Engine. In `internal/engine/engine.go`, add a `now` field to `Engine` and default it in `New`:

- Add to the `Engine` struct (after `killLocal`):
```go
	now       func() string // RFC3339 timestamp source (injectable for tests)
```
- Add `"time"` to `engine.go` imports, and in `New`'s returned `&Engine{...}` add:
```go
		now: func() string { return time.Now().UTC().Format(time.RFC3339) },
```
- In `internal/engine/ops_test.go`'s `newTestEngine`, the `Engine` is built as a struct literal — its `now` will be nil, so add a default there. Update `newTestEngine` to set `now`:
```go
	return &Engine{store: store, local: local, remote: remote, now: func() string { return "2026-07-01T00:00:00Z" }}
```
(Find the existing `return &Engine{store: store, local: local, remote: remote}` line in ops_test.go and replace it with the line above.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestRun -v && go test ./internal/engine/`
Expected: PASS (all Run tests; whole package green).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/control.go internal/engine/engine.go internal/engine/ops_test.go internal/engine/control_test.go
git commit -m "feat(engine): Run — sync-first launch, record live identity + pid"
```

---

## Task 5: Send — follow-up to a running agent

**Files:**
- Modify: `internal/engine/control.go`
- Test: `internal/engine/control_test.go`

`Send` delivers a follow-up task to a running agent. It dispatches on `Where`: remote sends by NAME (rbg-agent resolves it on the desktop), local sends by SESSION id (the laptop resumes that claude session directly). A busy remote agent surfaces `host.ErrBusy`.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/control_test.go`:

```go
func TestSendRemoteUsesName(t *testing.T) {
	var sent [2]string
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sent: &sent}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Send("job", "next step"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent[0] != "job" || sent[1] != "next step" {
		t.Errorf("remote Send got %v, want [job, next step] (by name)", sent)
	}
}

func TestSendLocalUsesSession(t *testing.T) {
	var sent [2]string
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sent: &sent}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Send("loc", "more"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent[0] != "LS" || sent[1] != "more" {
		t.Errorf("local Send got %v, want [LS, more] (by session)", sent)
	}
}

func TestSendPropagatesBusy(t *testing.T) {
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sendErr: host.ErrBusy}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	err := e.Send("job", "x")
	if err != host.ErrBusy {
		t.Errorf("Send err = %v, want host.ErrBusy", err)
	}
}

func TestSendToNeverRunErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "held", Task: "t", State: core.Held, Origin: core.Managed})
	if err := e.Send("held", "x"); err == nil {
		t.Errorf("sending to a never-run agent should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestSend -v`
Expected: FAIL — `e.Send undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/engine/control.go`:

```go
// Send delivers a follow-up task to a running agent (HLD F4), dispatched to its
// machine. The identity passed to the runner is machine-specific: the desktop
// rbg-agent resolves by NAME, while a local resume needs the SESSION id
// directly. A busy remote agent surfaces host.ErrBusy unchanged.
func (e *Engine) Send(name, task string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if a.Session == "" {
		return fmt.Errorf("send: agent %q has not run yet", name)
	}
	id := a.Name
	if a.Where == core.Local {
		id = a.Session
	}
	return e.pick(a.Where).newRunner(a.Dir).Send(id, task)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestSend -v`
Expected: PASS (all Send tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/control.go internal/engine/control_test.go
git commit -m "feat(engine): Send — follow-up dispatched by machine (name vs session)"
```

---

## Task 6: Kill — stop an agent on either machine

**Files:**
- Modify: `internal/engine/control.go`
- Test: `internal/engine/control_test.go`

`Kill` stops an agent. Remote goes through `RemoteRunner.Kill(name)` (rbg-agent terminates the desktop child). Local kills the tracked pid via `killLocal` (the `Runner` interface can't — `LocalRunner.Kill` is a stub). Either way, after a successful kill of a *managed* agent, the record's `State` is set to `Done` and persisted (the transcript is kept). Killing a foreign agent works but persists nothing new (it isn't in the store).

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/control_test.go`:

```go
func TestKillRemoteCallsRunnerKill(t *testing.T) {
	var killed string
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{killed: &killed}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("job"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killed != "job" {
		t.Errorf("remote Kill got %q, want job", killed)
	}
	rec, _ := e.store.Get("job")
	if rec.State != core.Done {
		t.Errorf("State = %q, want done after kill", rec.State)
	}
}

func TestKillLocalUsesPid(t *testing.T) {
	var killedPid int
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.killLocal = func(pid int) error { killedPid = pid; return nil }
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Pid: 4242, Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("loc"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killedPid != 4242 {
		t.Errorf("local Kill signalled pid %d, want 4242", killedPid)
	}
	rec, _ := e.store.Get("loc")
	if rec.State != core.Done {
		t.Errorf("State = %q, want done after kill", rec.State)
	}
}

func TestKillLocalWithoutPidErrors(t *testing.T) {
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.killLocal = func(pid int) error { t.Fatalf("must not kill with no pid"); return nil }
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Pid: 0, Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("loc"); err == nil {
		t.Errorf("local Kill with no recorded pid should error")
	}
}

func TestKillUnknownErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Kill("ghost"); err == nil {
		t.Errorf("killing an unknown agent should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestKill -v`
Expected: FAIL — `e.Kill undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/engine/control.go`:

```go
// Kill stops an agent (HLD F4). A remote agent is stopped via the desktop
// rbg-agent (by name); a local agent is stopped by signalling its tracked child
// pid (the Runner interface can't kill locally — the pid lives in the record).
// A managed agent is then marked Done and persisted; the transcript is kept.
func (e *Engine) Kill(name string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if a.Where == core.Local {
		if a.Pid <= 0 {
			return fmt.Errorf("kill: no recorded pid for local agent %q", name)
		}
		if err := e.killLocal(a.Pid); err != nil {
			return fmt.Errorf("kill: local agent %q: %w", name, err)
		}
	} else {
		if err := e.pick(core.Remote).newRunner(a.Dir).Kill(a.Name); err != nil {
			return fmt.Errorf("kill: remote agent %q: %w", name, err)
		}
	}
	// Mark a managed record Done (foreign agents aren't in the store).
	if rec, ok := e.store.Get(name); ok {
		rec.State = core.Done
		e.store.Add(rec)
		if err := e.store.Save(); err != nil {
			return fmt.Errorf("kill: save: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestKill -v && go test ./internal/engine/`
Expected: PASS (all Kill tests; whole package green).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/control.go internal/engine/control_test.go
git commit -m "feat(engine): Kill — stop remote via rbg-agent, local via tracked pid"
```

---

## Task 7: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run engine + core + host suites**

Run: `go test ./internal/engine/ ./internal/core/ ./internal/host/ -v`
Expected: PASS — all engine (observe + control), core, and host tests.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/engine/ ./internal/core/ ./internal/host/ && gofmt -l internal/engine/ internal/core/ internal/host/`
Expected: vet clean; gofmt lists no files. If gofmt lists a file, `gofmt -w` it and include in the commit below.

- [ ] **Step 4: Commit any fixups** (skip if none)

```bash
git add internal/engine/ internal/core/ internal/host/
git commit -m "test(engine): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **F1/F2 (delegate & launch, held-run-later):** `Run(name)` launches a managed record's task; a Held record becomes Running. ✅
- **F4 (act on running):** `Send` (follow-up), `Kill` (stop). ✅
- **F5 (sync-first):** `Run` pulls the repo before launch and aborts on pull failure, so a task never runs against un-synced code; skips pull when the agent has no repo. ✅
- **Live-identity capture:** `Run` records `Session` (both) and `Pid` (local) from `RunResult`, sets `Running` + `RunAt`. This is what lets `Reconcile` match the record to the live agent afterward, and lets `Kill` find the local child. ✅
- **Asymmetry resolved at the engine, honestly:** `Send` picks name (remote) vs session (local) by `Where`; `Kill` uses `RemoteRunner.Kill` (remote) vs `killLocal(pid)` (local). The `LocalRunner.Kill` stub is never called. ✅
- **Testability (NFR):** control ops depend on the `host.Repo`/`host.Runner` interfaces + an injectable `killLocal` + an injectable `now`; all control tests use fakes — no real SSH/claude/processes/clock. The one real-process test is `KillProcessGroup`'s (a `sleep` child), which is legitimate and hermetic. ✅

**Design decisions (documented, deliberate):**
- The bundle carries `newRunner(dir) host.Runner` (a factory), not a fixed `Runner`, because `LocalRunner` bakes the agent's working dir. Remote's factory ignores dir. This keeps `Run`/`Send` uniform (`pick(where).newRunner(dir)...`) despite the local/remote difference.
- Local kill goes through `Engine.killLocal(pid)`, NOT the `Runner` interface, because the pid lives in the store record (the engine's domain) and `LocalRunner.Kill` is intentionally a stub. This is the resolution of the asymmetry the observe-slice review flagged.
- `Run` acts only on a stored (managed) record. Fire-now for an *ad-hoc* task is `Create` (stage) + `Run` — one extra call, but it keeps a single launch path and guarantees every launched agent is tracked. A convenience `CreateAndRun` can be added at the CLI layer if wanted (not needed now).
- `Kill` marks the record `Done` rather than deleting it, keeping the agent (and its transcript) visible in the inventory — matches the "forget the live process, keep the transcript" intent. Deletion, if ever wanted, is a separate op.

**Deferred to later plans (not gaps here):** the scriptable CLI (`rbg raw run|send|kill|...`) wiring these onto the binary; the dashboard; `CreateAndRun` convenience; surfacing `Repo.Status` in the inventory/`RepoGroup` (dashboard); the per-call double-probe optimization in `find` (a cached-inventory resolve variant).

**Type/name consistency:** `machine{Source,Tx,Repo,newRunner}`, `Engine{store,local,remote,killLocal,now}`, `Run`, `Send`, `Kill`, `host.KillProcessGroup`, `core.Agent.Pid`, fakes `fakeRepo`/`fakeRunner`/`runnerFactory` — used identically across tasks and matching the verified `host`/`core` signatures. ✅
