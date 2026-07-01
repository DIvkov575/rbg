# rbg Core Domain Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure, I/O-free `internal/core` domain layer for rbg's clean architecture: one `Agent` type described by orthogonal attributes, a persisted record `Store`, a `Reconcile` function that merges rbg's records with live `claude agents` snapshots from each machine into one inventory, and view `lenses` over that inventory.

**Architecture:** This is layer 1 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.1, delivery step 1). It has **zero I/O** — every function is pure or operates on in-memory/on-disk JSON only, so the whole package is unit-testable without a terminal, SSH, or a live `claude`. Later plans build the `host` capability layer (SSH/git/claude I/O), the scriptable CLI, and the dashboard on top of this. The existing transport packages (`sshx`, `claudecli`, `config`, `client`, `run`) are untouched by this plan.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only (`encoding/json`, `os`, `path/filepath`, `sort`, `strings`, `testing`). No third-party dependencies. Standard `go test` with same-package table tests (repo convention — see `internal/localagent/*_test.go`).

**Scope of this plan (from HLD §2 Functional Requirements):**
- Establishes the data spine for **F3** (unified inventory) and **F6** (agent-list synchronization via `Reconcile`).
- Defines the attribute model enabling **F1/F2** (local/remote, held/fire-now) and **F7** (group-by-repo lens).
- Carries `Sync` as an attribute for **F5** (its *derivation* from git and its *display* are later plans).
- **Not in this plan:** any SSH/git/claude execution, the CLI, the dashboard, transcript transfer (F8), adopt-writes-to-store side effects (the pure `Adopt` transform is here; persisting it is the CLI plan).

**Verified facts (grounded 2026-06-30):**
- `claude agents --json --all` on this machine (**claude v2.1.197**) returns a JSON array; each element has keys `id, cwd, kind, sessionId, name, state, startedAt` and *optionally* `pid` and `status`. `startedAt` is a **Unix-milliseconds integer**. Observed `state` values: `"working"`, `"idle"`, `"done"`.
- Existing stores this layer's `Store` replaces: `internal/queue` (`~/.rbg/queue.json`), `internal/localagent` (`~/.rbg/local-agents.json`), and the desktop-side `internal/session` (`sessions.json`). Their atomic-save pattern (temp file + `os.Rename`, `0o600`, `MkdirAll` parent `0o755`) is the pattern to follow — see `internal/localagent/localagent.go:73-88`.
- Module path is `github.com/divkov575/rbg` (from `go.mod`).

---

## File Structure

All new files live in a new package `internal/core`:

- Create: `internal/core/agent.go` — the `Agent` struct and the four attribute enums (`Location`, `Lifecycle`, `Origin`, `Sync`). Pure data + tiny predicates.
- Create: `internal/core/agent_test.go`
- Create: `internal/core/store.go` — `Store` persisting managed-agent *records* to `~/.rbg/agents.json` (Load/Add/Get/Delete/Records/Save). Replaces queue + localagent + session stores conceptually.
- Create: `internal/core/store_test.go`
- Create: `internal/core/snapshot.go` — `Live` (one `claude agents --json` element) with custom JSON decoding, and `LifecycleFromState` mapping claude's `state` string to our `Lifecycle`.
- Create: `internal/core/snapshot_test.go`
- Create: `internal/core/reconcile.go` — `Reconcile(records, localLive, remoteLive) []Agent`, the F6 merge; plus the pure `Adopt` transform.
- Create: `internal/core/reconcile_test.go`
- Create: `internal/core/lenses.go` — `OnMachine`, `GroupByRepo` (+ `RepoGroup`), `Sort` — the F3/F7 views as pure functions.
- Create: `internal/core/lenses_test.go`

Each file has one responsibility; none imports another rbg package (the layer is a leaf). This keeps every file small and independently reasoned about.

---

## Task 1: Agent type and attribute enums

**Files:**
- Create: `internal/core/agent.go`
- Test: `internal/core/agent_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/core/agent_test.go`:

```go
package core

import (
	"encoding/json"
	"testing"
)

func TestAgentJSONRoundTrip(t *testing.T) {
	a := Agent{
		Name:    "fix-auth",
		Repo:    "git@github.com:me/app.git",
		Dir:     "/home/me/workplace/app",
		Task:    "fix the login bug",
		Session: "55a63641-2b5e-413e-bd07-00a74bbc1dfc",
		Where:   Remote,
		State:   Running,
		Origin:  Managed,
		Sync:    Behind,
		RunAt:   "2026-06-30T12:00:00Z",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Agent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != a {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, a)
	}
}

func TestIsHeld(t *testing.T) {
	held := Agent{State: Held}
	if !held.IsHeld() {
		t.Errorf("State=Held: IsHeld()=false, want true")
	}
	run := Agent{State: Running}
	if run.IsHeld() {
		t.Errorf("State=Running: IsHeld()=true, want false")
	}
}

func TestIsForeign(t *testing.T) {
	f := Agent{Origin: Foreign}
	if !f.IsForeign() {
		t.Errorf("Origin=Foreign: IsForeign()=false, want true")
	}
	m := Agent{Origin: Managed}
	if m.IsForeign() {
		t.Errorf("Origin=Managed: IsForeign()=true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestAgent -v`
Expected: FAIL — `internal/core/agent.go` does not exist (`undefined: Agent`, build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/agent.go`:

```go
// Package core is rbg's pure domain layer: one Agent type described by
// orthogonal attributes (where it runs, how far along it is, whether rbg
// started it, its code-sync state), a persisted record Store, a Reconcile
// that merges records with live `claude agents` snapshots into one inventory,
// and lenses (views) over that inventory. The package performs NO I/O beyond
// reading/writing its own JSON store file, so it is fully unit-testable.
package core

// Location is which machine an agent runs on. The laptop is just one machine,
// so local and remote delegation are the same operation with a different Where.
type Location string

const (
	Local  Location = "local"
	Remote Location = "remote"
)

// Lifecycle is how far along an agent is. Held is "prepared but not yet
// launched" (the old queue/local-agent notion); a held agent always carries a
// real Task — there are no blank placeholders.
type Lifecycle string

const (
	Held    Lifecycle = "held"
	Running Lifecycle = "running"
	Done    Lifecycle = "done"
)

// Origin is whether rbg started the agent. Foreign agents are discovered live
// on a machine but absent from rbg's records; adopting flips them to Managed.
type Origin string

const (
	Managed Origin = "managed"
	Foreign Origin = "foreign"
)

// Sync is the derived git state of an agent's repo/dir. Its value is filled by
// a later layer; SyncUnknown means "not yet determined".
type Sync string

const (
	SyncUnknown Sync = ""
	Aligned     Sync = "aligned"
	Ahead       Sync = "ahead"
	Behind      Sync = "behind"
	Dirty       Sync = "dirty"
)

// Agent is the single unit of delegated work. Local vs remote, held vs running
// vs done, and managed vs foreign are all attributes here, not separate types.
type Agent struct {
	Name    string    `json:"name"`    // stable handle (map key in the Store)
	Repo    string    `json:"repo"`    // git URL / identity; the grouping key
	Dir     string    `json:"dir"`     // working directory on its host
	Task    string    `json:"task"`    // the prompt (held agents still have one)
	Session string    `json:"session"` // claude sessionId once run ("" = never)
	Where   Location  `json:"where"`
	State   Lifecycle `json:"state"`
	Origin  Origin    `json:"origin"`
	Sync    Sync      `json:"sync"`
	RunAt   string    `json:"runAt"` // RFC3339 of last run ("" = never)
}

// IsHeld reports whether the agent is prepared but not yet launched.
func (a Agent) IsHeld() bool { return a.State == Held }

// IsForeign reports whether the agent was started outside rbg.
func (a Agent) IsForeign() bool { return a.Origin == Foreign }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestAgent -v && go test ./internal/core/ -run "TestIsHeld|TestIsForeign" -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/agent.go internal/core/agent_test.go
git commit -m "feat(core): Agent type with orthogonal attribute enums"
```

---

## Task 2: Record Store (persisted managed agents)

**Files:**
- Create: `internal/core/store.go`
- Test: `internal/core/store_test.go`

The Store persists only *records* — the agents rbg created/knows about. Live state (Running/Done, foreign agents) is layered on at reconcile time, not stored. This is the single file replacing the three old stores.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store_test.go`:

```go
package core

import (
	"path/filepath"
	"testing"
)

func TestStoreLoadMissingFileIsEmpty(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("LoadStore on missing file: %v", err)
	}
	if len(s.Records()) != 0 {
		t.Fatalf("missing file should yield empty store, got %d records", len(s.Records()))
	}
}

func TestStoreAddGetDelete(t *testing.T) {
	s, _ := LoadStore(filepath.Join(t.TempDir(), "agents.json"))
	a := Agent{Name: "one", Repo: "r", Task: "t", State: Held, Origin: Managed}
	s.Add(a)

	got, ok := s.Get("one")
	if !ok {
		t.Fatalf("Get(one): not found after Add")
	}
	if got.Task != "t" {
		t.Errorf("Get(one).Task = %q, want %q", got.Task, "t")
	}

	s.Delete("one")
	if _, ok := s.Get("one"); ok {
		t.Errorf("Get(one): still present after Delete")
	}
	s.Delete("missing") // must be a no-op, not a panic
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agents.json") // parent must be created
	s, _ := LoadStore(path)
	s.Add(Agent{Name: "a", Repo: "ra", State: Held, Origin: Managed})
	s.Add(Agent{Name: "b", Repo: "rb", State: Done, Origin: Managed, Session: "sid-b"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Records()) != 2 {
		t.Fatalf("reloaded %d records, want 2", len(reloaded.Records()))
	}
	b, ok := reloaded.Get("b")
	if !ok || b.Session != "sid-b" {
		t.Errorf("reloaded b = %+v, ok=%v; want Session sid-b", b, ok)
	}
}

func TestStoreLoadCorruptIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := writeFileForTest(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err != nil {
		t.Fatalf("corrupt file should not error, got %v", err)
	}
	if len(s.Records()) != 0 {
		t.Errorf("corrupt file should yield empty store, got %d", len(s.Records()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestStore -v`
Expected: FAIL — `undefined: LoadStore` and `undefined: writeFileForTest` (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/store.go`:

```go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Store is rbg's on-disk registry of managed-agent records, at ~/.rbg/agents.json.
// It holds only what rbg created/knows about; live status and foreign agents are
// layered on by Reconcile, not persisted here. Keyed by Agent.Name.
type Store struct {
	path   string
	agents map[string]Agent
}

// LoadStore reads the registry at path. A missing or corrupt file yields an
// empty store (not an error), so first run and partial writes are tolerated.
func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, agents: map[string]Agent{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var wrap struct {
		Agents map[string]Agent `json:"agents"`
	}
	_ = json.Unmarshal(data, &wrap) // corrupt → keep empty map
	if wrap.Agents != nil {
		s.agents = wrap.Agents
	}
	return s, nil
}

// Add inserts or updates a record keyed by Name.
func (s *Store) Add(a Agent) { s.agents[a.Name] = a }

// Get returns the record for name.
func (s *Store) Get(name string) (Agent, bool) { a, ok := s.agents[name]; return a, ok }

// Delete removes a record (missing name is a no-op).
func (s *Store) Delete(name string) { delete(s.agents, name) }

// Records returns all records sorted by Name (stable order for callers/tests).
func (s *Store) Records() []Agent {
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Save writes the store atomically (temp file + rename), creating parent dirs.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Agents map[string]Agent `json:"agents"`
	}{s.agents}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// writeFileForTest is a tiny helper so tests can plant a fixture file without
// importing os directly in every test file.
func writeFileForTest(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestStore -v`
Expected: PASS (all four Store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/store.go internal/core/store_test.go
git commit -m "feat(core): record Store persisting managed agents to agents.json"
```

---

## Task 3: Live snapshot decoding and state mapping

**Files:**
- Create: `internal/core/snapshot.go`
- Test: `internal/core/snapshot_test.go`

`Live` models one element of `claude agents --json --all`. Per the verified shape, `startedAt` is Unix-millis, and `state` is a free string we map onto our `Lifecycle`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/snapshot_test.go`:

```go
package core

import (
	"encoding/json"
	"testing"
)

// Real payload shape verified from `claude agents --json --all` (claude v2.1.197).
const sampleAgentsJSON = `[
  {"id":"55a63641","cwd":"/home/me/app","kind":"background","startedAt":1782395439347,
   "sessionId":"55a63641-2b5e-413e-bd07-00a74bbc1dfc","name":"analyze","state":"done"},
  {"pid":70515,"id":"48fd50b3","cwd":"/home/me/svc","kind":"background","startedAt":1782840532214,
   "sessionId":"48fd50b3-9f01-4320-93ba-290d1c7c65a3","name":"init","status":"busy","state":"working"}
]`

func TestParseLive(t *testing.T) {
	var live []Live
	if err := json.Unmarshal([]byte(sampleAgentsJSON), &live); err != nil {
		t.Fatalf("unmarshal claude agents payload: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if live[0].SessionID != "55a63641-2b5e-413e-bd07-00a74bbc1dfc" {
		t.Errorf("SessionID = %q", live[0].SessionID)
	}
	if live[0].Cwd != "/home/me/app" {
		t.Errorf("Cwd = %q", live[0].Cwd)
	}
	if live[0].Name != "analyze" {
		t.Errorf("Name = %q", live[0].Name)
	}
	if live[0].StartedAt != 1782395439347 {
		t.Errorf("StartedAt = %d", live[0].StartedAt)
	}
	if live[1].State != "working" {
		t.Errorf("State = %q", live[1].State)
	}
}

func TestLifecycleFromState(t *testing.T) {
	cases := map[string]Lifecycle{
		"working": Running,
		"idle":    Running,
		"done":    Done,
		"":        Done, // unknown/empty → treat as finished, never Held
		"weird":   Done,
	}
	for state, want := range cases {
		if got := LifecycleFromState(state); got != want {
			t.Errorf("LifecycleFromState(%q) = %q, want %q", state, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run "TestParseLive|TestLifecycleFromState" -v`
Expected: FAIL — `undefined: Live`, `undefined: LifecycleFromState`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/snapshot.go`:

```go
package core

// Live is one element of `claude agents --json --all` (verified shape, claude
// v2.1.197): keys id, cwd, kind, sessionId, name, state, startedAt, and
// optionally pid/status. startedAt is Unix-milliseconds. We decode only the
// fields reconcile needs.
type Live struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	State     string `json:"state"`
	StartedAt int64  `json:"startedAt"`
}

// LifecycleFromState maps claude's live state string onto our Lifecycle. A live
// agent is never Held (Held means "not yet launched", which by definition never
// appears in `claude agents`): "working"/"idle" are Running, everything else
// (including "done", unknown, and empty) is Done.
func LifecycleFromState(state string) Lifecycle {
	switch state {
	case "working", "idle":
		return Running
	default:
		return Done
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run "TestParseLive|TestLifecycleFromState" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/snapshot.go internal/core/snapshot_test.go
git commit -m "feat(core): Live snapshot decoding + claude-state to Lifecycle mapping"
```

---

## Task 4: Reconcile records with live snapshots (F6)

**Files:**
- Create: `internal/core/reconcile.go`
- Test: `internal/core/reconcile_test.go`

`Reconcile` is the heart of F3/F6: merge rbg's records with what is actually running on each machine into one inventory. Matching is by `Session` id (stable). This task builds it up over several tests; write them all first, then implement to green.

- [ ] **Step 1: Write the failing test**

Create `internal/core/reconcile_test.go`:

```go
package core

import (
	"testing"
)

// find returns the agent with the given Name, or fails the test.
func find(t *testing.T, agents []Agent, name string) Agent {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no agent named %q in %+v", name, agents)
	return Agent{}
}

func TestReconcileHeldRecordPassesThrough(t *testing.T) {
	// A held record has no live counterpart; it must survive reconcile unchanged.
	records := []Agent{{Name: "later", Repo: "r", Task: "do it", State: Held, Origin: Managed}}
	got := Reconcile(records, nil, nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	a := find(t, got, "later")
	if a.State != Held || a.Origin != Managed || a.Task != "do it" {
		t.Errorf("held record mangled: %+v", a)
	}
}

func TestReconcileMatchesRecordToLiveBySession(t *testing.T) {
	// A managed record that has run gets its live State/Where from the snapshot,
	// while keeping its Repo/Task/Name from the record.
	records := []Agent{{
		Name: "fix", Repo: "git@github:me/app", Task: "fix bug",
		Session: "sid-1", State: Running, Origin: Managed, Where: Remote,
	}}
	remote := []Live{{SessionID: "sid-1", Name: "fix", Cwd: "/home/me/app", State: "done", StartedAt: 100}}
	got := Reconcile(records, nil, remote)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	a := find(t, got, "fix")
	if a.State != Done { // live says done → record's Running is updated
		t.Errorf("State = %q, want done (from live)", a.State)
	}
	if a.Where != Remote {
		t.Errorf("Where = %q, want remote (live came from remote host)", a.Where)
	}
	if a.Repo != "git@github:me/app" || a.Task != "fix bug" {
		t.Errorf("record identity lost: Repo=%q Task=%q", a.Repo, a.Task)
	}
	if a.Origin != Managed {
		t.Errorf("Origin = %q, want managed", a.Origin)
	}
}

func TestReconcileForeignLocalAndRemote(t *testing.T) {
	// Live agents with no matching record are Foreign, tagged with the Where of
	// the host they were observed on, and given Name/Dir from the live entry.
	local := []Live{{SessionID: "L1", Name: "loc job", Cwd: "/home/me/x", State: "working", StartedAt: 1}}
	remote := []Live{{SessionID: "R1", Name: "rem job", Cwd: "/srv/y", State: "done", StartedAt: 2}}
	got := Reconcile(nil, local, remote)
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2", len(got))
	}
	l := find(t, got, "loc job")
	if l.Origin != Foreign || l.Where != Local || l.State != Running || l.Dir != "/home/me/x" || l.Session != "L1" {
		t.Errorf("local foreign wrong: %+v", l)
	}
	r := find(t, got, "rem job")
	if r.Origin != Foreign || r.Where != Remote || r.State != Done || r.Dir != "/srv/y" || r.Session != "R1" {
		t.Errorf("remote foreign wrong: %+v", r)
	}
}

func TestReconcileRecordWithoutSessionNotMatchedToForeign(t *testing.T) {
	// A held record (empty Session) must NOT accidentally match a live agent that
	// also happens to have no session in a snapshot; empty-session never matches.
	records := []Agent{{Name: "held", Repo: "r", Task: "t", State: Held, Origin: Managed}}
	local := []Live{{SessionID: "", Name: "ghost", Cwd: "/tmp", State: "done", StartedAt: 5}}
	got := Reconcile(records, local, nil)
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2 (held + foreign ghost)", len(got))
	}
	h := find(t, got, "held")
	if h.Origin != Managed || h.State != Held {
		t.Errorf("held record changed: %+v", h)
	}
	g := find(t, got, "ghost")
	if g.Origin != Foreign {
		t.Errorf("ghost should be foreign: %+v", g)
	}
}

func TestReconcileResultIsSortedByName(t *testing.T) {
	records := []Agent{
		{Name: "zebra", State: Held, Origin: Managed, Task: "z"},
		{Name: "alpha", State: Held, Origin: Managed, Task: "a"},
	}
	got := Reconcile(records, nil, nil)
	if got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Errorf("not sorted by name: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestAdoptFlipsForeignToManaged(t *testing.T) {
	f := Agent{Name: "x", Origin: Foreign, State: Done, Session: "s"}
	got := Adopt(f)
	if got.Origin != Managed {
		t.Errorf("Adopt did not flip Origin: %+v", got)
	}
	if got.Session != "s" || got.Name != "x" {
		t.Errorf("Adopt altered identity: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run "TestReconcile|TestAdopt" -v`
Expected: FAIL — `undefined: Reconcile`, `undefined: Adopt`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/reconcile.go`:

```go
package core

import "sort"

// Reconcile merges rbg's persisted records with the live agents observed on the
// local and remote machines into one inventory (HLD F3/F6). Matching is by
// Session id, which is stable across hosts and refreshes:
//
//   - A record with a Session that appears live keeps its identity (Name, Repo,
//     Task) but takes State from the live snapshot and Where from which host
//     reported it. It stays Managed.
//   - A record whose Session is empty (a Held agent) or does not appear live is
//     passed through unchanged — it is still a real, managed unit of work.
//   - A live agent with no matching record is Foreign: Where from the host it was
//     seen on, Name/Dir/State/Session from the live entry.
//
// The result is sorted by Name for a stable display order.
func Reconcile(records []Agent, localLive, remoteLive []Live) []Agent {
	// Index records by Session so live agents can find their record. Empty
	// sessions are never indexed, so a Held record never matches a live entry.
	bySession := map[string]int{} // session id -> index into out
	var out []Agent

	for _, rec := range records {
		idx := len(out)
		out = append(out, rec)
		if rec.Session != "" {
			bySession[rec.Session] = idx
		}
	}

	merge := func(live []Live, where Location) {
		for _, lv := range live {
			if lv.SessionID != "" {
				if idx, ok := bySession[lv.SessionID]; ok {
					// Managed agent that has run: refresh live-derived fields.
					out[idx].Where = where
					out[idx].State = LifecycleFromState(lv.State)
					if out[idx].Dir == "" {
						out[idx].Dir = lv.Cwd
					}
					continue
				}
			}
			// No record: a foreign agent discovered on this host.
			out = append(out, Agent{
				Name:    lv.Name,
				Dir:     lv.Cwd,
				Session: lv.SessionID,
				Where:   where,
				State:   LifecycleFromState(lv.State),
				Origin:  Foreign,
			})
		}
	}
	merge(localLive, Local)
	merge(remoteLive, Remote)

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Adopt takes a foreign agent under rbg management by flipping its Origin to
// Managed, leaving all other fields untouched. The transform is pure and
// idempotent; persisting the result is the caller's job (a later layer).
func Adopt(a Agent) Agent {
	a.Origin = Managed
	return a
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run "TestReconcile|TestAdopt" -v`
Expected: PASS (all six tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/reconcile.go internal/core/reconcile_test.go
git commit -m "feat(core): Reconcile records with live snapshots into one inventory"
```

---

## Task 5: View lenses — filter by machine, group by repo (F3, F7)

**Files:**
- Create: `internal/core/lenses.go`
- Test: `internal/core/lenses_test.go`

Views are pure functions over the reconciled `[]Agent`, not bespoke screens. This task delivers the two the CLI and dashboard both need: `OnMachine` (by-machine filter) and `GroupByRepo` (the project view).

- [ ] **Step 1: Write the failing test**

Create `internal/core/lenses_test.go`:

```go
package core

import "testing"

func sampleInventory() []Agent {
	return []Agent{
		{Name: "a", Repo: "app", Where: Local, State: Running},
		{Name: "b", Repo: "app", Where: Remote, State: Done},
		{Name: "c", Repo: "lib", Where: Remote, State: Held},
		{Name: "d", Repo: "", Where: Local, State: Done}, // no repo
	}
}

func TestOnMachine(t *testing.T) {
	inv := sampleInventory()
	local := OnMachine(inv, Local)
	if len(local) != 2 {
		t.Fatalf("Local: got %d, want 2", len(local))
	}
	for _, a := range local {
		if a.Where != Local {
			t.Errorf("OnMachine(Local) returned %q with Where=%q", a.Name, a.Where)
		}
	}
	remote := OnMachine(inv, Remote)
	if len(remote) != 2 {
		t.Fatalf("Remote: got %d, want 2", len(remote))
	}
}

func TestGroupByRepoSortedWithNoRepoLast(t *testing.T) {
	groups := GroupByRepo(sampleInventory())
	// Expect groups: "app" (2), "lib" (1), then "" bucket (1) last.
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	if groups[0].Repo != "app" || len(groups[0].Agents) != 2 {
		t.Errorf("group[0] = %q with %d agents, want app/2", groups[0].Repo, len(groups[0].Agents))
	}
	if groups[1].Repo != "lib" || len(groups[1].Agents) != 1 {
		t.Errorf("group[1] = %q with %d agents, want lib/1", groups[1].Repo, len(groups[1].Agents))
	}
	if groups[2].Repo != "" || len(groups[2].Agents) != 1 {
		t.Errorf("group[2] = %q with %d agents, want \"\"/1", groups[2].Repo, len(groups[2].Agents))
	}
}

func TestGroupByRepoAgentsSortedByName(t *testing.T) {
	inv := []Agent{
		{Name: "z", Repo: "app"},
		{Name: "a", Repo: "app"},
	}
	groups := GroupByRepo(inv)
	if groups[0].Agents[0].Name != "a" || groups[0].Agents[1].Name != "z" {
		t.Errorf("agents not name-sorted within group: %q, %q",
			groups[0].Agents[0].Name, groups[0].Agents[1].Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run "TestOnMachine|TestGroupByRepo" -v`
Expected: FAIL — `undefined: OnMachine`, `undefined: GroupByRepo`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/lenses.go`:

```go
package core

import "sort"

// OnMachine returns the agents whose Where matches, preserving input order.
// This is the by-machine view (local-only / remote-only) as a pure filter.
func OnMachine(agents []Agent, where Location) []Agent {
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		if a.Where == where {
			out = append(out, a)
		}
	}
	return out
}

// RepoGroup is one repository and the agents belonging to it. The by-project
// view (HLD F7) is a slice of these; a later layer attaches each group's Sync
// badge from the git layer.
type RepoGroup struct {
	Repo   string
	Agents []Agent
}

// GroupByRepo groups agents by Repo. Groups are sorted by repo name, except the
// empty-repo bucket (agents not pinned to a repo) which always sorts last.
// Agents within a group are sorted by Name.
func GroupByRepo(agents []Agent) []RepoGroup {
	byRepo := map[string][]Agent{}
	for _, a := range agents {
		byRepo[a.Repo] = append(byRepo[a.Repo], a)
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool {
		if (repos[i] == "") != (repos[j] == "") {
			return repos[j] == "" // empty repo sorts last
		}
		return repos[i] < repos[j]
	})
	out := make([]RepoGroup, 0, len(repos))
	for _, r := range repos {
		members := byRepo[r]
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		out = append(out, RepoGroup{Repo: r, Agents: members})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run "TestOnMachine|TestGroupByRepo" -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/lenses.go internal/core/lenses_test.go
git commit -m "feat(core): view lenses — OnMachine filter and GroupByRepo"
```

---

## Task 6: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full core package test suite**

Run: `go test ./internal/core/ -v`
Expected: PASS — all tests from Tasks 1–5, no failures.

- [ ] **Step 2: Verify the whole module still builds and tests green**

Run: `go build ./... && go test ./...`
Expected: PASS — the new package compiles and no existing package regressed (this layer is a leaf; nothing imports it yet, so existing packages are unaffected).

- [ ] **Step 3: Vet the new package**

Run: `go vet ./internal/core/`
Expected: no output (clean).

- [ ] **Step 4: Commit any fixups**

If Steps 1–3 surfaced fixes, commit them:

```bash
git add internal/core/
git commit -m "test(core): whole-package verification fixups"
```

If nothing changed, skip this commit.

---

## Self-Review Notes (traceability to the HLD)

- **F3 (unified inventory):** `Reconcile` (Task 4) produces one `[]Agent` across both machines; `OnMachine`/`GroupByRepo` (Task 5) are the views over it. ✅ data + views.
- **F6 (agent-list sync):** `Reconcile` merges records with live snapshots by Session id, updating State/Where from reality. ✅
- **F1/F2 (local/remote, held/fire-now):** carried as `Where` and `State` (`Held`) attributes on `Agent` (Task 1); held records survive reconcile (Task 4). ✅ model; *execution* is the host/CLI plan.
- **F7 (group by repo):** `GroupByRepo` + `RepoGroup` (Task 5). ✅ grouping; the Sync badge is attached by the git layer later.
- **F5 (code sync):** `Sync` attribute exists on `Agent` (Task 1) and `RepoGroup` is ready to carry it; **derivation and display are explicitly deferred** to the host/dashboard plans. ✅ (placeholder-free: the field is real and used in the round-trip test, not a TODO).
- **Foreign discovery / Adopt (F3):** foreign classification in `Reconcile`, pure `Adopt` transform (Task 4). ✅ transform; *persisting* adoption is the CLI plan.

**Deferred to later plans (not gaps in this one):** all SSH/git/claude I/O (host capability layer), the scriptable CLI verbs that call `LoadStore`/`Reconcile`/`Adopt`, transcript transfer (F8), and the interactive dashboard (F4 actions, ctrl-s view cycling, project-view launch).

**Type consistency check:** `Agent`, `Location`/`Lifecycle`/`Origin`/`Sync` enums, `LoadStore`, `Store.{Add,Get,Delete,Records,Save}`, `Live`, `LifecycleFromState`, `Reconcile`, `Adopt`, `OnMachine`, `GroupByRepo`, `RepoGroup` — names are used identically across every task that references them. ✅
