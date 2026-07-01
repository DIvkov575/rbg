# rbg Engine — Observe & Stage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first slice of rbg's `Engine` — the composition layer that wires the persisted `core.Store` to the `host` capabilities and exposes the operations the CLI and dashboard both call. This slice covers the *observe & stage* operations: `List` (the reconciled inventory), `Create` (stage a held task), `Read` (an agent's transcript), and `Adopt` (take a foreign agent under management), plus the `New` factory and per-machine capability bundle.

**Architecture:** Layer 3 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.1: cmd → Engine → core+host). The `core` (pure domain) and `host` (I/O capabilities) layers are complete and merged. The Engine is the single place that composes them into whole operations, so the CLI (`cmd/rbg`) and dashboard become thin consumers of ONE Engine — not re-implementers of the wiring. This slice is still **additive** (a new `internal/engine` package); it does not touch the shipped `cmd/rbg` binary. It depends only on interfaces (`host.AgentSource`, `host.Transcripts`) plus the real `core.Store`, so every operation is unit-testable with fakes + a temp-dir store — no real SSH, no real claude.

**Scope split:** The Engine has two natural halves. This plan is the **observe & stage** half (operations that read the world or write only rbg's local store). The **control** half (`Run` sync-first, `Send`, `Kill` — which drive remote/local processes and need per-agent working-dir and pid handling) is the next plan, "Engine — Run & Control." Splitting keeps each slice reviewable and independently green.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses `internal/core` (`Agent`, `Store`, `Reconcile`, `Adopt`), `internal/host` (`AgentSource`, `Transcripts`, `Inventory`, and the `Local*`/`Remote*` impls), `internal/config` (`Config`), `internal/run` (`Runner`). No new dependencies.

**Scope of this plan (from HLD §2):**
- **F3/F6** unified inventory + agent-list sync: `List` reconciles store records with both machines' live agents (via the existing `host.Inventory`).
- **F2** prepare-then-run-later: `Create` stages a held record (always with a real task — no blanks, per the design).
- **F8** transcript access: `Read` returns an agent's `.jsonl`, dispatched to the machine it lives on.
- **F3** adopt a foreign agent: `Adopt` flips a discovered agent to managed and persists it.
- **Not in this plan:** `Run`/`Send`/`Kill` (the control half — next plan), sync-first Pull→Launch sequencing, local pid-kill, transcript mirroring on read (the CLI composes `Read`+`host.SaveMirror`), the CLI/dashboard wiring, and any change to the shipped binary.

**Verified facts (grounded 2026-07-01):**
- `host.Inventory(records []core.Agent, local, remote host.AgentSource) ([]core.Agent, error)` exists and reconciles records with both machines' live agents, returning a usable inventory plus a joined degradation error (from the AgentSource slice).
- `core.Store` API: `LoadStore(path) (*Store, error)`, `Add(Agent)`, `Get(name) (Agent, bool)`, `Delete(name)`, `Records() []Agent`, `Save() error` (internal/core/store.go).
- `core.Adopt(a Agent) Agent` returns a copy with `Origin=Managed`, else unchanged (internal/core/reconcile.go).
- Host constructors: `host.LocalSource{R}`, `host.RemoteSource{C, R}`, `host.LocalTranscripts{Home}`, `host.RemoteTranscripts{C, R}` — verified field names from the shipped host slices.
- `core.Agent` fields: `Name, Repo, Dir, Task, Session string`; `Where core.Location`; `State core.Lifecycle` (`Held`/`Running`/`Done`); `Origin core.Origin` (`Managed`/`Foreign`); `Sync`; `RunAt`.
- Design decision (from the host final review): `Runner`/`Repo` are deliberately NOT in this slice's machine bundle — they need per-agent working-dir/pid handling that belongs to the control half. This slice's bundle holds only the two stateless-w.r.t.-target capabilities `List`/`Read`/`Adopt` use: `AgentSource` and `Transcripts`.

---

## File Structure

New package `internal/engine`:

- Create: `internal/engine/engine.go` — the `machine` capability bundle, the `Engine` struct, `New` factory, and the `pick(Location)` dispatcher.
- Create: `internal/engine/engine_test.go` — factory/pick tests + shared fakes (`fakeSource`, `fakeTx`).
- Create: `internal/engine/ops.go` — the operations `List`, `Create`, `Read`, `Adopt`.
- Create: `internal/engine/ops_test.go`

`engine.go` owns construction and dispatch; `ops.go` owns the operations. Splitting keeps each file focused and lets the control-half plan add `control.go` beside them without touching this file's wiring.

---

## Task 1: machine bundle, Engine struct, New factory, and pick

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

The `machine` bundle groups the host capabilities for one machine. `Engine` holds a `local` and a `remote` bundle plus the store. `New` wires the real host impls; `pick` selects a bundle by `core.Location` — the "local is just another machine" dispatch the whole design rests on.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/engine_test.go`:

```go
package engine

import (
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
	"github.com/divkov575/rbg/internal/run"
)

// --- shared test fakes (used across engine_test.go and ops_test.go) ---

// fakeSource is a canned host.AgentSource.
type fakeSource struct {
	live []core.Live
	err  error
}

func (f fakeSource) List() ([]core.Live, error) { return f.live, f.err }

// fakeTx is a canned host.Transcripts that records the session it was asked for.
type fakeTx struct {
	data       []byte
	err        error
	gotSession *string // if non-nil, Read stores the requested session here
}

func (f fakeTx) Read(session string) ([]byte, error) {
	if f.gotSession != nil {
		*f.gotSession = session
	}
	return f.data, f.err
}

func TestNewWiresRealHostImpls(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	store := filepath.Join(t.TempDir(), "agents.json")
	e, err := New(cfg, run.Exec{}, store, "/home/me")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// local bundle must be the local impls; remote must be the remote impls.
	if _, ok := e.local.Source.(host.LocalSource); !ok {
		t.Errorf("local.Source = %T, want host.LocalSource", e.local.Source)
	}
	if _, ok := e.local.Tx.(host.LocalTranscripts); !ok {
		t.Errorf("local.Tx = %T, want host.LocalTranscripts", e.local.Tx)
	}
	if _, ok := e.remote.Source.(host.RemoteSource); !ok {
		t.Errorf("remote.Source = %T, want host.RemoteSource", e.remote.Source)
	}
	if _, ok := e.remote.Tx.(host.RemoteTranscripts); !ok {
		t.Errorf("remote.Tx = %T, want host.RemoteTranscripts", e.remote.Tx)
	}
}

func TestPickSelectsByLocation(t *testing.T) {
	// Distinct sentinel data per machine's Tx lets us prove pick returns the
	// right bundle.
	e := &Engine{
		local:  machine{Tx: fakeTx{data: []byte("LOCAL")}},
		remote: machine{Tx: fakeTx{data: []byte("REMOTE")}},
	}
	l, _ := e.pick(core.Local).Tx.Read("x")
	if string(l) != "LOCAL" {
		t.Errorf("pick(Local).Tx read %q, want LOCAL", l)
	}
	r, _ := e.pick(core.Remote).Tx.Read("x")
	if string(r) != "REMOTE" {
		t.Errorf("pick(Remote).Tx read %q, want REMOTE", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -v`
Expected: FAIL — `undefined: New`, `undefined: Engine`, `undefined: machine` (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/engine.go`:

```go
// Package engine is rbg's composition layer: it wires the persisted core.Store
// to the host capabilities and exposes whole operations (list, create, read,
// adopt, and — in the control half — run, send, kill) that the CLI and
// dashboard consume. The Engine is the single place that knows how to turn a
// user intent into store updates + host I/O, so its front-ends stay thin.
package engine

import (
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
	"github.com/divkov575/rbg/internal/run"
)

// machine bundles the host capabilities for one machine. This slice uses the two
// that the observe/stage operations need — listing agents and reading
// transcripts. The control half adds the run/git capabilities.
type machine struct {
	Source host.AgentSource
	Tx     host.Transcripts
}

// Engine composes the store with the local and remote machine capability
// bundles. It is constructed by New and consumed by the CLI/dashboard.
type Engine struct {
	store  *core.Store
	local  machine
	remote machine
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
			Source: host.LocalSource{R: r},
			Tx:     host.LocalTranscripts{Home: home},
		},
		remote: machine{
			Source: host.RemoteSource{C: cfg, R: r},
			Tx:     host.RemoteTranscripts{C: cfg, R: r},
		},
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat(engine): machine bundle, Engine struct, New factory, pick dispatch"
```

---

## Task 2: List — the reconciled inventory

**Files:**
- Create: `internal/engine/ops.go`
- Test: `internal/engine/ops_test.go`

`List` is the primitive the other operations build on: it reconciles the store's records with both machines' live agents into one inventory, via the existing `host.Inventory`.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/ops_test.go`:

```go
package engine

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// newTestEngine builds an Engine with a real temp-dir store and injected fake
// capabilities, for operation tests.
func newTestEngine(t *testing.T, local, remote machine) *Engine {
	t.Helper()
	store, err := core.LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	return &Engine{store: store, local: local, remote: remote}
}

func findAgent(t *testing.T, agents []core.Agent, name string) core.Agent {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("agent %q not in %+v", name, agents)
	return core.Agent{}
}

func TestListReconcilesStoreWithBothMachines(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "L1", Name: "loc", Cwd: "/x", State: "working"},
		}}},
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "R1", Name: "rem", Cwd: "/y", State: "done"},
		}}},
	)
	// Stage a held record directly in the store.
	e.store.Add(core.Agent{Name: "held", Repo: "r", Task: "t", State: core.Held, Origin: core.Managed})

	agents, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(agents), agents)
	}
	if findAgent(t, agents, "loc").Where != core.Local {
		t.Errorf("loc should be Local")
	}
	if findAgent(t, agents, "rem").Where != core.Remote {
		t.Errorf("rem should be Remote")
	}
	if findAgent(t, agents, "held").State != core.Held {
		t.Errorf("held record should survive reconcile")
	}
}

func TestListPropagatesDegradationError(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{err: errors.New("laptop probe failed")}},
		machine{Source: fakeSource{}},
	)
	_, err := e.List()
	if err == nil {
		t.Errorf("expected a degradation error when a source fails")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestList -v`
Expected: FAIL — `undefined: (*Engine).List` (`e.List undefined`).

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/ops.go`:

```go
package engine

import (
	"fmt"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
)

// List returns the reconciled inventory: rbg's stored records merged with the
// live agents on both machines (HLD F3/F6). It degrades gracefully — an
// unreachable machine yields a non-nil error alongside a still-usable list
// (see host.Inventory).
func (e *Engine) List() ([]core.Agent, error) {
	return host.Inventory(e.store.Records(), e.local.Source, e.remote.Source)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestList -v`
Expected: PASS (both List tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ops.go internal/engine/ops_test.go
git commit -m "feat(engine): List — reconciled inventory across store and both machines"
```

---

## Task 3: Create — stage a held task

**Files:**
- Modify: `internal/engine/ops.go`
- Test: `internal/engine/ops_test.go`

`Create` stages a delegated task as a held record (F2), to be launched later by the control half. It forces `State=Held` and `Origin=Managed`, requires a real name and task (no blanks), and rejects a duplicate name.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/ops_test.go`:

```go
func TestCreateStagesHeldRecord(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	got, err := e.Create(core.Agent{
		Name: "later", Repo: "git@github:me/app", Dir: "/home/me/app",
		Task: "refactor the parser", Where: core.Remote,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Engine forces Held + Managed regardless of the input's other fields.
	if got.State != core.Held || got.Origin != core.Managed {
		t.Errorf("created agent = %+v, want Held+Managed", got)
	}
	if got.Task != "refactor the parser" || got.Where != core.Remote {
		t.Errorf("created agent lost fields: %+v", got)
	}
	// It must be persisted: a fresh List (store-only here) shows it.
	rec, ok := e.store.Get("later")
	if !ok || rec.State != core.Held {
		t.Errorf("held record not persisted: %+v ok=%v", rec, ok)
	}
}

func TestCreateRejectsBlankTaskAndName(t *testing.T) {
	e := newTestEngine(t, machine{}, machine{})
	if _, err := e.Create(core.Agent{Name: "x", Task: ""}); err == nil {
		t.Errorf("blank task should error (no blank agents)")
	}
	if _, err := e.Create(core.Agent{Name: "", Task: "t"}); err == nil {
		t.Errorf("blank name should error")
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	e := newTestEngine(t, machine{}, machine{})
	if _, err := e.Create(core.Agent{Name: "dup", Task: "t"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := e.Create(core.Agent{Name: "dup", Task: "u"}); err == nil {
		t.Errorf("duplicate name should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestCreate -v`
Expected: FAIL — `e.Create undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/engine/ops.go`:

```go
// Create stages a delegated task as a held record, to be launched later (HLD
// F2). It forces State=Held and Origin=Managed, requires a non-empty name and
// task (there are no blank agents), and rejects a name already in the store.
// The returned agent is the persisted record.
func (e *Engine) Create(spec core.Agent) (core.Agent, error) {
	if spec.Name == "" {
		return core.Agent{}, fmt.Errorf("create: name is required")
	}
	if spec.Task == "" {
		return core.Agent{}, fmt.Errorf("create: a task is required (no blank agents)")
	}
	if _, exists := e.store.Get(spec.Name); exists {
		return core.Agent{}, fmt.Errorf("create: agent %q already exists", spec.Name)
	}
	spec.State = core.Held
	spec.Origin = core.Managed
	e.store.Add(spec)
	if err := e.store.Save(); err != nil {
		return core.Agent{}, fmt.Errorf("create: save: %w", err)
	}
	return spec, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestCreate -v`
Expected: PASS (all three Create tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ops.go internal/engine/ops_test.go
git commit -m "feat(engine): Create — stage a held task (managed, no blanks, no dupes)"
```

---

## Task 4: Read — an agent's transcript, dispatched by machine

**Files:**
- Modify: `internal/engine/ops.go`
- Test: `internal/engine/ops_test.go`

`Read` returns an agent's raw transcript (F8). It resolves the agent from the reconciled inventory (so it works for foreign agents too, not just stored records), then reads from the machine the agent lives on via `pick`.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/ops_test.go`:

```go
func TestReadDispatchesToAgentsMachine(t *testing.T) {
	var localAsked, remoteAsked string
	e := newTestEngine(t,
		machine{
			Source: fakeSource{live: []core.Live{{SessionID: "L1", Name: "loc", Cwd: "/x", State: "working"}}},
			Tx:     fakeTx{data: []byte("local transcript"), gotSession: &localAsked},
		},
		machine{
			Source: fakeSource{live: []core.Live{{SessionID: "R1", Name: "rem", Cwd: "/y", State: "done"}}},
			Tx:     fakeTx{data: []byte("remote transcript"), gotSession: &remoteAsked},
		},
	)

	data, err := e.Read("rem")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "remote transcript" {
		t.Errorf("Read = %q, want the remote transcript", data)
	}
	if remoteAsked != "R1" {
		t.Errorf("remote Tx asked for session %q, want R1", remoteAsked)
	}
	if localAsked != "" {
		t.Errorf("local Tx should not have been asked, got %q", localAsked)
	}
}

func TestReadUnknownAgentErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if _, err := e.Read("ghost"); err == nil {
		t.Errorf("reading an unknown agent should error")
	}
}

func TestReadAgentWithoutSessionErrors(t *testing.T) {
	// A held record has never run → no session → nothing to read.
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "held", Task: "t", State: core.Held, Origin: core.Managed})
	if _, err := e.Read("held"); err == nil {
		t.Errorf("reading a never-run agent should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestRead -v`
Expected: FAIL — `e.Read undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/engine/ops.go`:

```go
// find returns the named agent from the reconciled inventory, so callers resolve
// against live reality (including foreign agents), not just stored records.
func (e *Engine) find(name string) (core.Agent, error) {
	agents, err := e.List()
	// Note: err may be a degradation error; the inventory is still usable, so we
	// search it and only surface err if the agent isn't found.
	for _, a := range agents {
		if a.Name == name {
			return a, nil
		}
	}
	if err != nil {
		return core.Agent{}, fmt.Errorf("agent %q not found (inventory degraded: %w)", name, err)
	}
	return core.Agent{}, fmt.Errorf("agent %q not found", name)
}

// Read returns an agent's raw transcript bytes (HLD F8), read from whichever
// machine the agent lives on. An agent that has never run (no session) has no
// transcript.
func (e *Engine) Read(name string) ([]byte, error) {
	a, err := e.find(name)
	if err != nil {
		return nil, err
	}
	if a.Session == "" {
		return nil, fmt.Errorf("agent %q has not run yet (no transcript)", name)
	}
	return e.pick(a.Where).Tx.Read(a.Session)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestRead -v`
Expected: PASS (all three Read tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ops.go internal/engine/ops_test.go
git commit -m "feat(engine): Read — an agent's transcript, dispatched to its machine"
```

---

## Task 5: Adopt — take a foreign agent under management

**Files:**
- Modify: `internal/engine/ops.go`
- Test: `internal/engine/ops_test.go`

`Adopt` takes an agent discovered on a machine but not in rbg's records (F3) and persists it as managed, so rbg tracks it going forward. Adopting a non-foreign agent is an error (it's already managed).

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/ops_test.go`:

```go
func TestAdoptPersistsForeignAsManaged(t *testing.T) {
	e := newTestEngine(t,
		machine{Source: fakeSource{}},
		machine{Source: fakeSource{live: []core.Live{
			{SessionID: "R1", Name: "wild", Cwd: "/srv/app", State: "working"},
		}}},
	)
	if err := e.Adopt("wild"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	// Now stored as a managed record carrying the live identity.
	rec, ok := e.store.Get("wild")
	if !ok {
		t.Fatalf("adopted agent not in store")
	}
	if rec.Origin != core.Managed {
		t.Errorf("adopted Origin = %q, want managed", rec.Origin)
	}
	if rec.Session != "R1" || rec.Dir != "/srv/app" {
		t.Errorf("adopted record lost live identity: %+v", rec)
	}
}

func TestAdoptNonForeignErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "mine", Task: "t", State: core.Held, Origin: core.Managed})
	if err := e.Adopt("mine"); err == nil {
		t.Errorf("adopting an already-managed agent should error")
	}
}

func TestAdoptUnknownErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Adopt("ghost"); err == nil {
		t.Errorf("adopting an unknown agent should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestAdopt -v`
Expected: FAIL — `e.Adopt undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/engine/ops.go`:

```go
// Adopt takes a foreign agent (discovered live on a machine but absent from
// rbg's records) under management and persists it, so rbg tracks it going
// forward (HLD F3). Adopting an agent that is already managed is an error.
func (e *Engine) Adopt(name string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	if !a.IsForeign() {
		return fmt.Errorf("agent %q is already managed", name)
	}
	e.store.Add(core.Adopt(a))
	if err := e.store.Save(); err != nil {
		return fmt.Errorf("adopt: save: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestAdopt -v`
Expected: PASS (all three Adopt tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ops.go internal/engine/ops_test.go
git commit -m "feat(engine): Adopt — persist a foreign agent as managed"
```

---

## Task 6: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run the engine suite**

Run: `go test ./internal/engine/ -v`
Expected: PASS — all factory/pick + List/Create/Read/Adopt tests.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS — new package compiles, nothing regressed.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/engine/ && gofmt -l internal/engine/`
Expected: vet clean (no output); gofmt lists no files. If gofmt lists a file, run `gofmt -w internal/engine/` and include it in the commit below.

- [ ] **Step 4: Commit any fixups** (skip if none)

```bash
git add internal/engine/
git commit -m "test(engine): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **F3/F6 (unified inventory + sync):** `List` = `host.Inventory(store.Records, local.Source, remote.Source)`, degradation error propagated. ✅
- **F2 (prepare-then-run-later):** `Create` stages a Held+Managed record with a required task (no blanks) and no duplicate names. ✅ (launching it later is the control half.)
- **F8 (transcript access):** `Read` resolves the agent from the reconciled inventory and reads from its machine via `pick`; never-run agents error. ✅
- **F3 (adopt foreign):** `Adopt` flips Foreign→Managed via `core.Adopt` and persists. ✅
- **Local-is-just-another-machine:** `machine` bundle + `pick(Location)` — one code path for local/remote. ✅
- **Thin front-ends:** all wiring lives in `New`; the CLI/dashboard will call `List/Create/Read/Adopt`, not re-compose host+store. ✅
- **Testability (NFR):** operations depend on `host.AgentSource`/`host.Transcripts` interfaces + a temp-dir `core.Store`; tests inject `fakeSource`/`fakeTx` — no real SSH/claude/filesystem-home. ✅

**Design decisions (documented, deliberate):**
- The `machine` bundle holds only `Source` + `Tx` this slice. `Runner`/`Repo` are added by the control-half plan, which also resolves per-agent working-dir (LocalRunner bakes a `Dir`) and pid-based local kill — those don't belong to observe/stage.
- `find` (and thus `Read`/`Adopt`) resolves against the full reconciled inventory rather than the store alone, so foreign agents are addressable. This costs a live probe of both machines per call; acceptable per "refresh on demand" — the CLI/dashboard may cache a `List` and pass resolved agents directly if it wants to avoid repeated probes (a future refinement, not needed now).
- `Read` returns raw bytes and does NOT mirror; the CLI composes `Read` + `host.SaveMirror` when it wants a local copy (keeps `Read` side-effect-free). Rendering `.jsonl` to text is `internal/render`'s job at the front-end.

**Deferred to later plans (not gaps here):** `Run` (sync-first Pull→Launch, record Session/Pid/State/RunAt), `Send`, `Kill` (control half); the scriptable CLI wiring these onto `rbg` verbs; the dashboard; transcript mirroring/rendering at the front-end.

**Type/name consistency:** `machine{Source,Tx}`, `Engine{store,local,remote}`, `New`, `pick`, `List`, `Create`, `Read`, `Adopt`, `find`, fakes `fakeSource`/`fakeTx` — used identically across tasks and matching the verified `host`/`core` signatures. ✅
