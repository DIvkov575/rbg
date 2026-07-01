# rbg Host Layer — Repo Capability (Sync Status) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `Repo` capability of rbg's `host` layer: determine a checkout's git sync status (aligned / ahead / behind / dirty / unknown) and pull it fast-forward — on either machine. This fills the `core.Sync` attribute (F5) and provides the sync-first pull that delegation will use before running a task.

**Architecture:** Layer 2 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.5 Code synchronization). Prior slices built `AgentSource`+`Inventory` (read agents) and `Runner` (run agents). This slice adds `Repo` (read+reconcile git state). The status DERIVATION is a pure function in `core` (a function of observed git facts, per HLD §5.5); the git I/O lives in `host` behind the `run.Runner` seam, so the whole slice is unit-testable with `run.Recording` — no real git, no real SSH. Local and remote share all parsing/derivation logic; they differ only in how a `git` subcommand is executed (directly vs. over SSH).

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses `internal/core` (`Sync` enum), `internal/run` (`Runner`+`Recording`), `internal/sshx` (SSH argv), `internal/config` (`Config`). No new dependencies.

**Scope of this plan (from HLD §2):**
- **F5** code synchronization: surface each context's sync status, and pull before running.
- **Not in this plan:** cloning a repo (the desktop already has `agent.Clone`; the CLI wires it), attaching Sync to `RepoGroup` for display (the dashboard plan), the sync-FIRST *composition* with Runner (the CLI plan sequences Pull→Launch), and any Store persistence.

**Verified facts (grounded 2026-06-30, on this repo):**
- `git status --porcelain` — empty output ⇒ clean; any non-empty line ⇒ dirty. Exit 0.
- `git rev-parse --abbrev-ref @{u}` — prints the upstream ref (e.g. `origin/main`) at exit 0 when an upstream is configured; exits non-zero when there is none. This is the has-upstream probe.
- `git rev-list --left-right --count @{u}...HEAD` — prints two tab-separated integers `<behind>\t<ahead>` (left = commits only in upstream = behind; right = commits only in HEAD = ahead). Verified output `0\t33` on this checkout. Requires an upstream (else non-zero).
- `git -C <dir> <args>` runs git in `<dir>` without a shell `cd`; this is the invocation style already used by `internal/localagent` (`git -C dir pull --ff-only`) and compatible with `run.Exec` (which sets no cwd).
- `sshx.BuildSSHArgs(c, remote, opts)` wraps a remote argv for ssh; the remote argv `["git","-C",dir,...]` runs git on the desktop. Same mechanism the shipped code uses.
- `run.Recording` matches a canned `Result` by the first `BySubstring` key found in the joined args, else `Default` — so a test can return different output per git subcommand by keying on `"status"`, `"rev-parse"`, `"rev-list"`.

---

## File Structure

One addition to `core`, one new file in `host`:

- Modify: `internal/core/agent.go` — add `DeriveSync(hasUpstream bool, behind, ahead int, dirty bool) Sync`, the pure status-derivation (belongs in core: it's a function of observed facts, no I/O).
- Modify: `internal/core/agent_test.go` — table test for `DeriveSync`.
- Create: `internal/host/repo.go` — the `Repo` interface, shared `gitRunner`-based `syncStatus`/`pull` helpers, `LocalRepo`, `RemoteRepo`.
- Create: `internal/host/repo_test.go`

`repo.go` owns "read/reconcile git state on one machine," sitting beside `source.go` (agents) and `runner.go` (run) as host's third concern.

---

## Task 1: DeriveSync in core (pure derivation)

**Files:**
- Modify: `internal/core/agent.go`
- Test: `internal/core/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/core/agent_test.go`:

```go
func TestDeriveSync(t *testing.T) {
	cases := []struct {
		name        string
		hasUpstream bool
		behind      int
		ahead       int
		dirty       bool
		want        Sync
	}{
		{"clean aligned", true, 0, 0, false, Aligned},
		{"behind", true, 3, 0, false, Behind},
		{"ahead", true, 0, 2, false, Ahead},
		{"dirty beats behind", true, 5, 0, true, Dirty},
		{"dirty beats ahead", true, 0, 5, true, Dirty},
		{"behind beats ahead when diverged", true, 1, 1, false, Behind},
		{"no upstream clean is unknown", false, 0, 0, false, SyncUnknown},
		{"no upstream but dirty is dirty", false, 0, 0, true, Dirty},
	}
	for _, c := range cases {
		got := DeriveSync(c.hasUpstream, c.behind, c.ahead, c.dirty)
		if got != c.want {
			t.Errorf("%s: DeriveSync(%v,%d,%d,%v) = %q, want %q",
				c.name, c.hasUpstream, c.behind, c.ahead, c.dirty, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestDeriveSync -v`
Expected: FAIL — `undefined: DeriveSync`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/agent.go` (after `NewSessionID`; no new imports needed):

```go
// DeriveSync computes an agent's repo Sync state from observed git facts. The
// priority is deliberate: uncommitted local changes (dirty) are the most
// actionable warning before running a delegated task, so they win over any
// commit divergence; without an upstream, ahead/behind is unknowable so the
// state is SyncUnknown (unless dirty). When an upstream exists and the tree is
// clean: behind (needs a pull before running) outranks ahead, else Aligned.
func DeriveSync(hasUpstream bool, behind, ahead int, dirty bool) Sync {
	switch {
	case dirty:
		return Dirty
	case !hasUpstream:
		return SyncUnknown
	case behind > 0:
		return Behind
	case ahead > 0:
		return Ahead
	default:
		return Aligned
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestDeriveSync -v && go test ./internal/core/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/agent.go internal/core/agent_test.go
git commit -m "feat(core): DeriveSync — pure repo sync-state derivation from git facts"
```

---

## Task 2: Repo interface, shared helpers, and LocalRepo

**Files:**
- Create: `internal/host/repo.go`
- Test: `internal/host/repo_test.go`

The `Repo` interface has `Status(dir)` and `Pull(dir)`. Local and remote share the command sequence and parsing via a `gitRunner` closure (runs one git subcommand in a dir); they differ only in that closure. This task builds the interface, the shared helpers, and `LocalRepo`.

- [ ] **Step 1: Write the failing test**

Create `internal/host/repo_test.go`:

```go
package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
)

func TestLocalRepoStatusAligned(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},        // clean
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0}, // has upstream
		"rev-list":  {Stdout: []byte("0\t0\n"), Code: 0},   // 0 behind, 0 ahead
	}}
	got, err := LocalRepo{R: r}.Status("/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.Aligned {
		t.Errorf("Status = %q, want aligned", got)
	}
}

func TestLocalRepoStatusBehind(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("4\t0\n"), Code: 0}, // 4 behind
	}}
	got, _ := LocalRepo{R: r}.Status("/repo")
	if got != core.Behind {
		t.Errorf("Status = %q, want behind", got)
	}
}

func TestLocalRepoStatusDirty(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(" M file.go\n"), Code: 0}, // dirty
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("0\t0\n"), Code: 0},
	}}
	got, _ := LocalRepo{R: r}.Status("/repo")
	if got != core.Dirty {
		t.Errorf("Status = %q, want dirty", got)
	}
}

func TestLocalRepoStatusNoUpstream(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("fatal: no upstream\n"), Code: 128}, // no upstream
		// rev-list is never reached when there is no upstream.
	}}
	got, err := LocalRepo{R: r}.Status("/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.SyncUnknown {
		t.Errorf("Status = %q, want unknown", got)
	}
}

func TestLocalRepoStatusUsesGitDashCInDir(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte(""), Code: 0}}
	_, _ = LocalRepo{R: r}.Status("/my/repo")
	if len(r.Calls) == 0 {
		t.Fatal("no git calls made")
	}
	for _, c := range r.Calls {
		if c.Name != "git" {
			t.Errorf("ran %q, want git", c.Name)
		}
		// every git call must be scoped to the dir via -C /my/repo
		j := joined(c.Args)
		if !contains(j, "-C") || !contains(j, "/my/repo") {
			t.Errorf("git call not scoped to dir: %v", c.Args)
		}
	}
}

func TestLocalRepoPull(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (LocalRepo{R: r}).Pull("/repo"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "pull") || !contains(j, "--ff-only") || !contains(j, "/repo") {
		t.Errorf("pull args wrong: %v", r.Calls[0].Args)
	}
}

func TestLocalRepoPullFailsOnNonZero(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("conflict"), Code: 1}}
	if err := (LocalRepo{R: r}).Pull("/repo"); err == nil {
		t.Errorf("expected error on non-zero pull exit")
	}
}
```

Note: `joined` and `contains` already exist in the package's test files — reuse them.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestLocalRepo -v`
Expected: FAIL — `undefined: LocalRepo`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/repo.go`:

```go
package host

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// Repo reconciles and reports the git state of a checkout on one machine.
type Repo interface {
	// Status reports the checkout's Sync state (aligned/ahead/behind/dirty/unknown).
	Status(dir string) (core.Sync, error)
	// Pull fast-forwards the checkout, so a delegated task runs against upstream.
	Pull(dir string) error
}

// gitRunner runs one git subcommand in dir and returns stdout + exit code. The
// dir is applied via `git -C <dir>` (no shell cd), so local and remote share it.
type gitRunner func(dir string, args []string) ([]byte, int, error)

// syncStatus gathers the three git facts (dirty, has-upstream, behind/ahead)
// via g and derives the Sync state. A hard failure of the dirty/upstream probes
// is an error; a non-zero upstream probe simply means "no upstream" (not an
// error), and rev-list is skipped in that case.
func syncStatus(g gitRunner, dir string) (core.Sync, error) {
	// dirty?
	out, code, err := g(dir, []string{"status", "--porcelain"})
	if err != nil {
		return core.SyncUnknown, fmt.Errorf("git status: %w", err)
	}
	if code != 0 {
		return core.SyncUnknown, fmt.Errorf("git status exited %d: %s", code, out)
	}
	dirty := len(strings.TrimSpace(string(out))) > 0

	// has upstream? (non-zero exit = no upstream configured, not a failure)
	_, code, err = g(dir, []string{"rev-parse", "--abbrev-ref", "@{u}"})
	if err != nil {
		return core.SyncUnknown, fmt.Errorf("git rev-parse: %w", err)
	}
	hasUpstream := code == 0

	var behind, ahead int
	if hasUpstream {
		out, code, err = g(dir, []string{"rev-list", "--left-right", "--count", "@{u}...HEAD"})
		if err != nil {
			return core.SyncUnknown, fmt.Errorf("git rev-list: %w", err)
		}
		if code != 0 {
			return core.SyncUnknown, fmt.Errorf("git rev-list exited %d: %s", code, out)
		}
		behind, ahead, err = parseAheadBehind(out)
		if err != nil {
			return core.SyncUnknown, err
		}
	}
	return core.DeriveSync(hasUpstream, behind, ahead, dirty), nil
}

// parseAheadBehind parses `git rev-list --left-right --count` output, two
// tab/space-separated ints: <behind>\t<ahead> (left=upstream-only, right=HEAD-only).
func parseAheadBehind(out []byte) (behind, ahead int, err error) {
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", string(out))
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind %q: %w", fields[0], err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead %q: %w", fields[1], err)
	}
	return behind, ahead, nil
}

// pull fast-forwards the checkout via g.
func pull(g gitRunner, dir string) error {
	out, code, err := g(dir, []string{"pull", "--ff-only"})
	if err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("git pull exited %d: %s", code, out)
	}
	return nil
}

// LocalRepo runs git on the laptop.
type LocalRepo struct {
	R run.Runner
}

// git runs `git -C <dir> <args>` locally.
func (l LocalRepo) git(dir string, args []string) ([]byte, int, error) {
	return l.R.Run("git", append([]string{"-C", dir}, args...), nil)
}

func (l LocalRepo) Status(dir string) (core.Sync, error) { return syncStatus(l.git, dir) }
func (l LocalRepo) Pull(dir string) error                { return pull(l.git, dir) }

var _ Repo = LocalRepo{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestLocalRepo -v`
Expected: PASS (all LocalRepo tests).

- [ ] **Step 5: Commit**

```bash
git add internal/host/repo.go internal/host/repo_test.go
git commit -m "feat(host): Repo interface + LocalRepo — git sync status and ff-only pull"
```

---

## Task 3: RemoteRepo

**Files:**
- Modify: `internal/host/repo.go`
- Test: `internal/host/repo_test.go`

`RemoteRepo` runs the same git commands on the desktop over SSH, reusing all the shared `syncStatus`/`pull` logic — only its `gitRunner` differs.

- [ ] **Step 1: Write the failing test**

Append to `internal/host/repo_test.go`:

```go
func TestRemoteRepoStatusRunsGitOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("0\t1\n"), Code: 0}, // 1 ahead
	}}
	got, err := RemoteRepo{C: cfg, R: r}.Status("/srv/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.Ahead {
		t.Errorf("Status = %q, want ahead", got)
	}
	// every call is ssh, and the git command + dir + host must appear.
	for _, c := range r.Calls {
		if c.Name != "ssh" {
			t.Errorf("ran %q, want ssh", c.Name)
		}
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "desktop") || !contains(j, "git") || !contains(j, "/srv/repo") {
		t.Errorf("ssh git call missing host/git/dir: %v", r.Calls[0].Args)
	}
}

func TestRemoteRepoPull(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (RemoteRepo{C: cfg, R: r}).Pull("/srv/repo"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Calls[0].Name != "ssh" {
		t.Errorf("ran %q, want ssh", r.Calls[0].Name)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "pull") || !contains(j, "--ff-only") || !contains(j, "/srv/repo") {
		t.Errorf("remote pull args wrong: %v", r.Calls[0].Args)
	}
}

func TestRemoteRepoConfigsConnectTimeout(t *testing.T) {
	// A down host must surface as an error via ssh's own non-zero exit, not hang.
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("ssh: connect timeout"), Code: 255}}
	if _, err := (RemoteRepo{C: cfg, R: r}).Status("/srv/repo"); err == nil {
		t.Errorf("expected error when ssh fails (exit 255)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestRemoteRepo -v`
Expected: FAIL — `undefined: RemoteRepo`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/host/repo.go`:

```go
// RemoteRepo runs git on the desktop over SSH.
type RemoteRepo struct {
	C *config.Config
	R run.Runner
}

// git runs `git -C <dir> <args>` on the desktop over SSH.
func (s RemoteRepo) git(dir string, args []string) ([]byte, int, error) {
	remote := append([]string{"git", "-C", dir}, args...)
	sshArgs := sshx.BuildSSHArgs(s.C, remote, sshx.Options{ConnectTimeout: true})
	return s.R.Run("ssh", sshArgs, nil)
}

func (s RemoteRepo) Status(dir string) (core.Sync, error) { return syncStatus(s.git, dir) }
func (s RemoteRepo) Pull(dir string) error                { return pull(s.git, dir) }

var _ Repo = RemoteRepo{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestRemoteRepo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/host/repo.go internal/host/repo_test.go
git commit -m "feat(host): RemoteRepo — git sync status and ff-only pull over SSH"
```

---

## Task 4: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run host + core suites**

Run: `go test ./internal/host/ ./internal/core/ -v`
Expected: PASS — all Repo + Runner + Source + Inventory + core tests.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/host/ ./internal/core/ && gofmt -l internal/host/ internal/core/`
Expected: vet clean; gofmt lists no files.

- [ ] **Step 4: Commit any fixups** (skip if none)

```bash
git add internal/host/ internal/core/
git commit -m "test(host): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **F5 (code sync — surface status):** `LocalRepo.Status` / `RemoteRepo.Status` return `core.Sync`; derivation is the pure `core.DeriveSync`. ✅
- **F5 (code sync — pull before running):** `Pull` (ff-only) on both. The sync-FIRST *sequencing* (Pull then Launch) is the CLI's composition, deferred. ✅
- **Derivation is pure (HLD §5.5):** `DeriveSync` is a total function of (hasUpstream, behind, ahead, dirty); the I/O of gathering those facts is separate, in host. ✅
- **Local-is-just-another-machine:** `LocalRepo`/`RemoteRepo` behind one `Repo` interface; all logic shared via the `gitRunner` closure, differing only in exec. ✅
- **Testability (NFR):** all git I/O behind `run.Runner`; tests use `run.Recording` keyed per subcommand — no real git, no real SSH. ✅

**Priority decision (documented, deliberate):** `DeriveSync` ranks Dirty > (no-upstream ⇒ Unknown) > Behind > Ahead > Aligned. Dirty wins because uncommitted local work is the most actionable warning before a sync-first run; Behind outranks Ahead because it's the state that requires a pull. This is a single-enum summary — a repo that is both behind and dirty reports Dirty; the CLI can re-query specifics if it ever needs finer detail.

**Deferred to later plans (not gaps here):** clone (CLI wires the existing `agent.Clone`), attaching Sync to `RepoGroup`/rendering (dashboard), sync-first Pull→Launch sequencing (CLI), Store persistence, and per-repo status caching/TTL (a caller concern per HLD §6.2).

**Type/name consistency:** `Repo`, `LocalRepo{R}`, `RemoteRepo{C,R}`, `gitRunner`, `syncStatus`, `parseAheadBehind`, `pull`, `core.DeriveSync`, `core.Sync` (`Aligned`/`Ahead`/`Behind`/`Dirty`/`SyncUnknown`) — used identically across tasks and matching the real signatures verified above. ✅
