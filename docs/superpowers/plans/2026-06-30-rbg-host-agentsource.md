# rbg Host Layer — AgentSource & Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first slice of rbg's `host` capability layer: an `AgentSource` that returns live `claude agents` snapshots from a machine (local via `os/exec`, remote via SSH), and an `Inventory` function that reconciles rbg's stored records with both machines' live agents into one `[]core.Agent`. This is what makes `core.Reconcile` runnable against real machines.

**Architecture:** Layer 2 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.1 Capabilities; delivery step 2). This plan implements ONE of the four capabilities — `AgentSource` (list agents on a host) — plus the `Inventory` composition that wires it to layer-1's `core.Reconcile`. The other three capabilities (`Runner` for spawn/resume, `Repo` for git sync, `Transcripts` for `.jsonl` transfer) are later plans. All I/O goes through the existing `run.Runner` seam, so this layer is unit-testable with `run.Recording` (no real SSH, no real `claude`).

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses existing packages: `internal/core` (the `Live`, `Agent` types and `Reconcile`), `internal/run` (the `Runner` seam + `Recording` test stub), `internal/sshx` (SSH argv builder), `internal/config` (`Config`), `internal/claudecli` (the claude argv contract). No new dependencies.

**Scope of this plan (from HLD §2):**
- Implements the data source behind **F3** (unified inventory) and **F6** (agent-list synchronization): pulling live agents from each machine and reconciling with records.
- **Not in this plan:** running/resuming/killing agents (`Runner` — F1/F2/F4), git sync (`Repo` — F5), transcript transfer (`Transcripts` — F8), the CLI, the dashboard, and persisting adoption. Those are later plans.

**Verified facts (grounded 2026-06-30):**
- `claude agents --json --all` on this machine (**claude v2.1.197**) exits 0 and prints a JSON array; `core.Live` already decodes its element shape (`sessionId, name, cwd, state, startedAt`). Verified in layer 1.
- `os/exec` runs the `claude` **binary** directly — the interactive shell alias `claude=claude --dangerously-skip-permissions` does NOT apply, which is correct: listing agents needs no permission flag.
- `sshx.BuildSSHArgs(c, remote, opts)` (internal/sshx/sshx.go:57) returns the `ssh` argv with the remote argv collapsed into ONE shell-quoted string re-parsed by the desktop login shell. Passing `remote = []string{"claude","agents","--json","--all"}` runs claude on the desktop's login-shell PATH — the same mechanism rbg already uses against the real host.
- `run.Runner.Run(name, args, stdin) ([]byte, int, error)` (internal/run/run.go:28); `run.Recording` matches a canned `Result` by the first `BySubstring` key found in the joined args, else `Default` (internal/run/run.go:52-68).
- `config.Config` has `Host`, `SSHOpts`, `Mux`, `ControlPath`, `ControlPersist`, `AgentPath`, `CWD` (internal/config/config.go:16).

---

## File Structure

New package `internal/host`, plus one small addition to the existing `claudecli` contract package:

- Modify: `internal/claudecli/claude.go` — add `AgentsListArgs()` so the `claude agents --json --all` argv lives with the rest of the claude contract (single source of truth for how claude is invoked).
- Modify: `internal/claudecli/claude_test.go` — test for `AgentsListArgs`.
- Create: `internal/host/source.go` — the `AgentSource` interface, `LocalSource` (exec claude), `RemoteSource` (ssh + claude), and the shared JSON parse.
- Create: `internal/host/source_test.go`
- Create: `internal/host/inventory.go` — `Inventory(records, local, remote)` composing the sources with `core.Reconcile`, degrading gracefully when a machine is unreachable.
- Create: `internal/host/inventory_test.go`

`source.go` owns "get live agents from one machine"; `inventory.go` owns "combine records + both machines into one list." Each file has one responsibility and is independently testable.

---

## Task 1: Add the claude agents-list argv to the contract

**Files:**
- Modify: `internal/claudecli/claude.go`
- Test: `internal/claudecli/claude_test.go`

The `claudecli` package is the single place that knows how `claude` is invoked. Add the agents-list argv here rather than hardcoding it in the host layer.

- [ ] **Step 1: Write the failing test**

Add this test to `internal/claudecli/claude_test.go` (keep the existing two tests; append this function):

```go
func TestAgentsListArgs(t *testing.T) {
	got := AgentsListArgs()
	want := []string{"agents", "--json", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecli/ -run TestAgentsListArgs -v`
Expected: FAIL — `undefined: AgentsListArgs`.

- [ ] **Step 3: Write minimal implementation**

Add this function to `internal/claudecli/claude.go` (after `ResumeHeadlessArgs`):

```go
// AgentsListArgs builds `claude agents --json --all`, the headless listing of
// every background session on a host regardless of spawner (verified against
// claude v2.1.197). --json prints a JSON array and exits without a TTY; --all
// includes completed sessions, so rbg sees finished agents too. The result is
// decoded into []core.Live by the host layer. No permission flag is needed:
// listing runs no tools.
func AgentsListArgs() []string {
	return []string{"agents", "--json", "--all"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/claudecli/ -v`
Expected: PASS (all three tests, including the two pre-existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/claudecli/claude.go internal/claudecli/claude_test.go
git commit -m "feat(claudecli): AgentsListArgs for the agents --json --all contract"
```

---

## Task 2: LocalSource and RemoteSource

**Files:**
- Create: `internal/host/source.go`
- Test: `internal/host/source_test.go`

An `AgentSource` returns the live agents on one machine as `[]core.Live`. `LocalSource` execs `claude` directly; `RemoteSource` runs it over SSH. Both parse the same JSON via a shared helper.

- [ ] **Step 1: Write the failing test**

Create `internal/host/source_test.go`:

```go
package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// A realistic two-element `claude agents --json --all` payload (verified shape).
const agentsPayload = `[
  {"id":"55a63641","cwd":"/home/me/app","kind":"background","startedAt":1782395439347,
   "sessionId":"55a63641-2b5e-413e-bd07-00a74bbc1dfc","name":"analyze","state":"done"},
  {"pid":70515,"id":"48fd50b3","cwd":"/home/me/svc","kind":"background","startedAt":1782840532214,
   "sessionId":"48fd50b3-9f01-4320-93ba-290d1c7c65a3","name":"init","status":"busy","state":"working"}
]`

func TestLocalSourceListParses(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte(agentsPayload), Code: 0}}
	live, err := LocalSource{R: r}.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if live[0].SessionID != "55a63641-2b5e-413e-bd07-00a74bbc1dfc" || live[1].State != "working" {
		t.Errorf("parsed wrong: %+v", live)
	}
}

func TestLocalSourceExecsClaudeAgents(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("[]"), Code: 0}}
	if _, err := (LocalSource{R: r}).List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(r.Calls))
	}
	c := r.Calls[0]
	if c.Name != "claude" {
		t.Errorf("ran %q, want claude", c.Name)
	}
	// argv must be exactly the agents-list contract.
	want := []string{"agents", "--json", "--all"}
	if len(c.Args) != len(want) {
		t.Fatalf("args = %v, want %v", c.Args, want)
	}
	for i := range want {
		if c.Args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, c.Args[i], want[i])
		}
	}
}

func TestLocalSourceNonZeroExitErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("boom"), Code: 1}}
	if _, err := (LocalSource{R: r}).List(); err == nil {
		t.Errorf("expected error on non-zero exit, got nil")
	}
}

func TestLocalSourceBadJSONErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("{not an array"), Code: 0}}
	if _, err := (LocalSource{R: r}).List(); err == nil {
		t.Errorf("expected error on unparseable JSON, got nil")
	}
}

func TestRemoteSourceRunsClaudeOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{Default: run.Result{Stdout: []byte(agentsPayload), Code: 0}}
	live, err := RemoteSource{C: cfg, R: r}.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	// The remote command is the last ssh arg (a single shell-quoted string);
	// it must carry the claude agents-list invocation and target the host.
	args := r.Calls[0].Args
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if !contains(joined, "desktop") {
		t.Errorf("ssh args missing host: %v", args)
	}
	if !contains(joined, "agents") || !contains(joined, "--json") {
		t.Errorf("ssh args missing claude agents invocation: %v", args)
	}
}

// contains is a tiny substring helper (avoids importing strings in the test).
func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return needle == ""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -v`
Expected: FAIL — `undefined: LocalSource`, `undefined: RemoteSource` (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/source.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -v`
Expected: PASS (all source tests).

- [ ] **Step 5: Commit**

```bash
git add internal/host/source.go internal/host/source_test.go
git commit -m "feat(host): AgentSource — local exec and remote SSH claude agents listing"
```

---

## Task 3: Inventory — reconcile records with both machines

**Files:**
- Create: `internal/host/inventory.go`
- Test: `internal/host/inventory_test.go`

`Inventory` is the composition point: it pulls live agents from both machines and reconciles them with rbg's stored records via `core.Reconcile`. It degrades gracefully — if a machine is unreachable, its agents are treated as empty and the failure is reported alongside the (still usable) inventory, matching the HLD's "surface the drift, let the operator choose" mitigation.

- [ ] **Step 1: Write the failing test**

Create `internal/host/inventory_test.go`:

```go
package host

import (
	"errors"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// fakeSource is a canned AgentSource for Inventory tests.
type fakeSource struct {
	live []core.Live
	err  error
}

func (f fakeSource) List() ([]core.Live, error) { return f.live, f.err }

func TestInventoryReconcilesRecordsWithBothMachines(t *testing.T) {
	records := []core.Agent{
		{Name: "held", Repo: "r", Task: "t", State: core.Held, Origin: core.Managed},
	}
	local := fakeSource{live: []core.Live{
		{SessionID: "L1", Name: "loc", Cwd: "/home/me/x", State: "working"},
	}}
	remote := fakeSource{live: []core.Live{
		{SessionID: "R1", Name: "rem", Cwd: "/srv/y", State: "done"},
	}}

	agents, err := Inventory(records, local, remote)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	// held record + local foreign + remote foreign = 3
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(agents), agents)
	}
	byName := map[string]core.Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	if byName["loc"].Where != core.Local || byName["loc"].Origin != core.Foreign {
		t.Errorf("local foreign wrong: %+v", byName["loc"])
	}
	if byName["rem"].Where != core.Remote || byName["rem"].Origin != core.Foreign {
		t.Errorf("remote foreign wrong: %+v", byName["rem"])
	}
	if byName["held"].State != core.Held {
		t.Errorf("held record lost: %+v", byName["held"])
	}
}

func TestInventoryDegradesWhenRemoteFails(t *testing.T) {
	records := []core.Agent{{Name: "keep", State: core.Held, Origin: core.Managed, Task: "t"}}
	local := fakeSource{live: []core.Live{{SessionID: "L1", Name: "loc", Cwd: "/x", State: "idle"}}}
	remote := fakeSource{err: errors.New("host down")}

	agents, err := Inventory(records, local, remote)
	if err == nil {
		t.Errorf("expected a non-nil error signaling remote failure")
	}
	// Still returns the usable inventory from records + local.
	names := map[string]bool{}
	for _, a := range agents {
		names[a.Name] = true
	}
	if !names["keep"] || !names["loc"] {
		t.Errorf("degraded inventory should still contain keep + loc, got %+v", agents)
	}
}

func TestInventoryNoErrorWhenBothSucceed(t *testing.T) {
	agents, err := Inventory(nil, fakeSource{}, fakeSource{})
	if err != nil {
		t.Errorf("both sources ok → err should be nil, got %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("empty everything → empty inventory, got %+v", agents)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestInventory -v`
Expected: FAIL — `undefined: Inventory`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/inventory.go`:

```go
package host

import (
	"errors"

	"github.com/divkov575/rbg/internal/core"
)

// Inventory pulls the live agents from both machines and reconciles them with
// rbg's stored records into one inventory (HLD F3/F6). It degrades gracefully:
// if a source fails (e.g. the desktop is unreachable), that machine's agents are
// treated as empty and the failure is returned alongside the still-usable
// inventory built from records plus whatever source(s) did answer. Callers
// should surface a non-nil error to the operator but may still display agents.
func Inventory(records []core.Agent, local, remote AgentSource) ([]core.Agent, error) {
	var errs []error

	localLive, err := local.List()
	if err != nil {
		errs = append(errs, err)
		localLive = nil
	}
	remoteLive, err := remote.List()
	if err != nil {
		errs = append(errs, err)
		remoteLive = nil
	}

	agents := core.Reconcile(records, localLive, remoteLive)
	return agents, errors.Join(errs...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -v`
Expected: PASS (all host tests: source + inventory).

- [ ] **Step 5: Commit**

```bash
git add internal/host/inventory.go internal/host/inventory_test.go
git commit -m "feat(host): Inventory reconciles records with both machines, degrading on failure"
```

---

## Task 4: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run the host + claudecli test suites**

Run: `go test ./internal/host/ ./internal/claudecli/ -v`
Expected: PASS — all tests from Tasks 1–3.

- [ ] **Step 2: Verify the whole module builds and tests green**

Run: `go build ./... && go test ./...`
Expected: PASS — new package compiles, no existing package regressed.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/host/ ./internal/claudecli/ && gofmt -l internal/host/ internal/claudecli/`
Expected: vet clean (no output); gofmt lists no files.

- [ ] **Step 4: Commit any fixups**

If Steps 1–3 surfaced fixes, commit them:

```bash
git add internal/host/ internal/claudecli/
git commit -m "test(host): whole-package verification fixups"
```

If nothing changed, skip this commit.

---

## Self-Review Notes (traceability to the HLD)

- **F3 (unified inventory):** `Inventory` (Task 3) returns one `[]core.Agent` combining records + both machines. ✅
- **F6 (agent-list sync):** `AgentSource` (Task 2) pulls live truth from each machine; `Inventory` reconciles it with records via `core.Reconcile`. ✅
- **Graceful degradation (HLD §6.2 risk row):** `Inventory` returns a usable inventory plus a joined error when a machine is unreachable, rather than failing entirely. ✅ (tested in `TestInventoryDegradesWhenRemoteFails`).
- **Claude contract centralization:** the `agents --json --all` argv lives in `claudecli` (Task 1), consistent with `LaunchHeadlessArgs`/`ResumeHeadlessArgs`. ✅
- **Testability (NFR):** every path goes through `run.Runner`; all tests use `run.Recording` or a fake source — no real SSH/claude. ✅

**Deferred to later plans (not gaps here):** the `Runner` capability (spawn/resume/kill — F1/F2/F4), the `Repo` capability (clone/pull/sync-status — F5), the `Transcripts` capability (F8), the scriptable CLI that calls `LoadStore`+`Inventory`, and the dashboard.

**Type/name consistency:** `AgentSource`, `LocalSource{R}`, `RemoteSource{C,R}`, `parseLive`, `Inventory`, `claudecli.AgentsListArgs`, `core.Live`, `core.Reconcile`, `core.Agent` — names are used identically across every task and match layer-1's exported API. ✅
