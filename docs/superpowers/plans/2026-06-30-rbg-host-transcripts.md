# rbg Host Layer — Transcripts Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `Transcripts` capability of rbg's `host` layer: read an agent's raw conversation transcript (`.jsonl`) from either machine, and mirror a pulled transcript into a local rbg-owned store — so a remote agent's conversation can be read from the laptop (F8). This is the fourth and final host capability.

**Architecture:** Layer 2 of 4 from `docs/HLD-rbg-clean-architecture.md` (§5.6 SyncTranscript; F8). Prior slices built AgentSource+Inventory, Runner, and Repo. This slice adds `Transcripts`, completing the host layer. Reading a transcript = locating `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl` by session-id glob (the cwd-slug dir is unpredictable) and returning its bytes. `LocalTranscripts` globs the local filesystem; `RemoteTranscripts` runs a glob+cat over SSH. All process I/O goes through the `run.Runner` seam; local file reads use a `Home`-rooted path so tests use `t.TempDir()` — no real `~/.claude`, no real SSH.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses `internal/core` (adds `ValidSessionID`), `internal/run` (`Runner`+`Recording`), `internal/sshx` (SSH argv), `internal/config` (`Config`). No new dependencies.

**Scope of this plan (from HLD §2):**
- **F8** transcript access: read a remote agent's conversation from the laptop; mirror it locally on demand.
- **Not in this plan:** rendering `.jsonl` into human-readable text (the shipped `internal/render` already does this; the CLI/dashboard calls it), attaching transcripts to the inventory, and any Store wiring. No transcript *merge* (whole-file, per HLD non-goals).

**Verified facts (grounded 2026-06-30, on this machine):**
- Transcripts live at `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl`; the cwd-slug dir is unpredictable, so rbg locates a transcript by globbing `<home>/.claude/projects/*/<sessionId>.jsonl` (this is exactly what the shipped `agent.findTranscript` does). Confirmed real files exist in this layout, e.g. `~/.claude/projects/-private-tmp/000ab497-....jsonl`.
- `sh -c 'cat ~/.claude/projects/*/<sid>.jsonl'` expands the glob and prints the file — verified exit 0 with real content. When passed as the remote argv `["sh","-c","cat ~/.claude/projects/*/<sid>.jsonl"]`, `sshx.RemoteCommand` single-quotes each token, so the desktop login shell hands `sh -c` the exact command string and the INNER `sh` expands the glob. **Security:** because `<sid>` is interpolated into a shell string, it MUST be validated to `^[A-Za-z0-9-]+$` before use — hence `core.ValidSessionID` (Task 1) guards every transcript op. The existing desktop `agent.validSessionID` uses this same shape.
- `sshx.BuildSSHArgs(c, remote, opts)` wraps a remote argv for ssh over the mux; `r.Run("ssh", args, nil)` executes it — the shipped pattern.
- `run.Recording` returns a canned `Result` by first `BySubstring` match on joined args, else `Default`.

---

## File Structure

One addition to `core`, one new file in `host`:

- Modify: `internal/core/agent.go` — add `ValidSessionID(id string) bool` (the glob/shell-injection guard; session ids are domain identity, and this pairs with `NewSessionID`).
- Modify: `internal/core/agent_test.go` — test `ValidSessionID`.
- Create: `internal/host/transcripts.go` — the `Transcripts` interface, `LocalTranscripts`, `RemoteTranscripts`, and the `SaveMirror` helper.
- Create: `internal/host/transcripts_test.go`

`transcripts.go` owns "get/mirror a transcript," sitting beside source/runner/repo as host's fourth concern.

---

## Task 1: ValidSessionID in core

**Files:**
- Modify: `internal/core/agent.go`
- Test: `internal/core/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/core/agent_test.go`:

```go
func TestValidSessionID(t *testing.T) {
	valid := []string{
		"55a63641-2b5e-413e-bd07-00a74bbc1dfc",
		"abc123",
		"A-B-c-9",
	}
	for _, id := range valid {
		if !ValidSessionID(id) {
			t.Errorf("ValidSessionID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"",                       // empty
		"has space",              // space
		"semi;colon",             // shell metachar
		"glob*star",              // glob metachar
		"dot.dot",                // path char
		"slash/slash",            // path separator
		"tilde~home",             // tilde
		"quote'quote",            // single quote (shell breakout attempt)
		"dollar$var",             // expansion
	}
	for _, id := range invalid {
		if ValidSessionID(id) {
			t.Errorf("ValidSessionID(%q) = true, want false", id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestValidSessionID -v`
Expected: FAIL — `undefined: ValidSessionID`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/agent.go` (after `DeriveSync`; no new imports):

```go
// ValidSessionID reports whether id is a safe session identifier: non-empty and
// matching ^[A-Za-z0-9-]+$. rbg interpolates session ids into glob patterns and
// (for remote transcript reads) into a remote shell command string, so any id
// used there MUST pass this guard first — it admits only characters that are
// inert to globbing and shell parsing, preventing injection.
func ValidSessionID(id string) bool {
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestValidSessionID -v && go test ./internal/core/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/agent.go internal/core/agent_test.go
git commit -m "feat(core): ValidSessionID — glob/shell-injection guard for session ids"
```

---

## Task 2: Transcripts interface, LocalTranscripts, and SaveMirror

**Files:**
- Create: `internal/host/transcripts.go`
- Test: `internal/host/transcripts_test.go`

`LocalTranscripts` reads a transcript from the local `~/.claude/projects/*/` tree (rooted at an injectable `Home` so tests use `t.TempDir()`). `SaveMirror` writes pulled bytes into a local rbg-owned mirror dir so a remote transcript has a deterministic home on the laptop.

- [ ] **Step 1: Write the failing test**

Create `internal/host/transcripts_test.go`:

```go
package host

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTranscript plants a transcript file under home's claude tree and returns
// nothing; it mimics claude's real layout: ~/.claude/projects/<slug>/<sid>.jsonl.
func writeTranscript(t *testing.T, home, slug, sid, content string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalTranscriptsReadFindsBySessionGlob(t *testing.T) {
	home := t.TempDir()
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	writeTranscript(t, home, "-some-unpredictable-cwd-slug", sid, `{"line":1}`+"\n")

	data, err := LocalTranscripts{Home: home}.Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != `{"line":1}`+"\n" {
		t.Errorf("Read = %q, want the transcript content", data)
	}
}

func TestLocalTranscriptsReadMissingIsError(t *testing.T) {
	home := t.TempDir()
	_, err := LocalTranscripts{Home: home}.Read("11111111-2222-3333-4444-555555555555")
	if err == nil {
		t.Errorf("expected error reading a nonexistent transcript")
	}
}

func TestLocalTranscriptsReadRejectsBadSessionID(t *testing.T) {
	home := t.TempDir()
	_, err := LocalTranscripts{Home: home}.Read("../etc/passwd")
	if err == nil {
		t.Errorf("expected error for an invalid session id (guard)")
	}
}

func TestSaveMirrorWritesToRbgDir(t *testing.T) {
	home := t.TempDir()
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	content := []byte(`{"mirrored":true}` + "\n")

	path, err := SaveMirror(home, sid, content)
	if err != nil {
		t.Fatalf("SaveMirror: %v", err)
	}
	want := filepath.Join(home, ".rbg", "transcripts", sid+".jsonl")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading mirrored file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("mirrored content = %q, want %q", got, content)
	}
}

func TestSaveMirrorRejectsBadSessionID(t *testing.T) {
	if _, err := SaveMirror(t.TempDir(), "bad/../id", []byte("x")); err == nil {
		t.Errorf("expected error for invalid session id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run "TestLocalTranscripts|TestSaveMirror" -v`
Expected: FAIL — `undefined: LocalTranscripts`, `undefined: SaveMirror`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/host/transcripts.go`:

```go
package host

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// Transcripts reads an agent's raw .jsonl conversation transcript from one
// machine, located by session id (the on-disk cwd-slug dir is unpredictable).
type Transcripts interface {
	// Read returns the raw transcript bytes for a claude session id.
	Read(session string) ([]byte, error)
}

// transcriptGlob is the session-id glob under a home dir, matching claude's
// layout ~/.claude/projects/<cwd-slug>/<sessionId>.jsonl.
func transcriptGlob(home, session string) string {
	return filepath.Join(home, ".claude", "projects", "*", session+".jsonl")
}

// LocalTranscripts reads transcripts from the laptop's ~/.claude tree. Home is
// the home directory root (injectable so tests use t.TempDir()).
type LocalTranscripts struct {
	Home string
}

// Read globs the local claude tree for the session's transcript and returns it.
func (l LocalTranscripts) Read(session string) ([]byte, error) {
	if !core.ValidSessionID(session) {
		return nil, fmt.Errorf("invalid session id %q", session)
	}
	matches, err := filepath.Glob(transcriptGlob(l.Home, session))
	if err != nil {
		return nil, fmt.Errorf("glob transcript: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no transcript found for session %s", session)
	}
	return os.ReadFile(matches[0])
}

// SaveMirror writes pulled transcript bytes to the laptop's rbg-owned mirror
// (<home>/.rbg/transcripts/<session>.jsonl) so a remote transcript has a stable
// local home, and returns the path. The session id is guarded before use in the
// path.
func SaveMirror(home, session string, data []byte) (string, error) {
	if !core.ValidSessionID(session) {
		return "", fmt.Errorf("invalid session id %q", session)
	}
	dir := filepath.Join(home, ".rbg", "transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

var _ Transcripts = LocalTranscripts{}
```

Note: `config`, `run`, and `sshx` are imported for `RemoteTranscripts` (Task 3). If the build complains they are unused in THIS task, omit them now and re-add in Task 3 — keep the build green (imports for this task: `fmt`, `os`, `path/filepath`, `core`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run "TestLocalTranscripts|TestSaveMirror" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/host/transcripts.go internal/host/transcripts_test.go
git commit -m "feat(host): Transcripts interface + LocalTranscripts + SaveMirror"
```

---

## Task 3: RemoteTranscripts

**Files:**
- Modify: `internal/host/transcripts.go`
- Test: `internal/host/transcripts_test.go`

`RemoteTranscripts` reads a transcript from the desktop by running a glob+cat over SSH. The session id is guarded (Task 1) BEFORE interpolation into the remote shell command — this is the injection defense.

- [ ] **Step 1: Write the failing test**

Append to `internal/host/transcripts_test.go` (add imports `"github.com/divkov575/rbg/internal/config"` and `"github.com/divkov575/rbg/internal/run"` to the file's import block):

```go
func TestRemoteTranscriptsReadCatsOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	r := &run.Recording{Default: run.Result{Stdout: []byte(`{"remote":1}` + "\n"), Code: 0}}

	data, err := RemoteTranscripts{C: cfg, R: r}.Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != `{"remote":1}`+"\n" {
		t.Errorf("Read = %q", data)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	j := joined(r.Calls[0].Args)
	// must invoke a shell to expand the glob, carry the session id and host.
	if !contains(j, "sh") || !contains(j, sid) || !contains(j, "desktop") {
		t.Errorf("ssh args missing sh/sid/host: %v", r.Calls[0].Args)
	}
	if !contains(j, "projects") {
		t.Errorf("ssh args missing the claude projects glob: %v", r.Calls[0].Args)
	}
}

func TestRemoteTranscriptsReadRejectsBadSessionID(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if _, err := (RemoteTranscripts{C: cfg, R: r}).Read("evil; rm -rf ~"); err == nil {
		t.Errorf("expected invalid-session-id error BEFORE any ssh call")
	}
	if len(r.Calls) != 0 {
		t.Errorf("must not run ssh for an invalid session id, got %+v", r.Calls)
	}
}

func TestRemoteTranscriptsReadNonZeroErrors(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("cat: no such file"), Code: 1}}
	if _, err := (RemoteTranscripts{C: cfg, R: r}).Read("55a63641-2b5e-413e-bd07-00a74bbc1dfc"); err == nil {
		t.Errorf("expected error on non-zero cat exit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/host/ -run TestRemoteTranscripts -v`
Expected: FAIL — `undefined: RemoteTranscripts`.

- [ ] **Step 3: Write minimal implementation**

Two edits to `internal/host/transcripts.go`:

(a) Ensure the import block includes `"github.com/divkov575/rbg/internal/config"`, `"github.com/divkov575/rbg/internal/run"`, and `"github.com/divkov575/rbg/internal/sshx"` (add any that were omitted in Task 2).

(b) Append:

```go
// RemoteTranscripts reads transcripts from the desktop over SSH. It runs
// `sh -c 'cat <glob>'` so the DESKTOP shell expands the session-id glob (the
// cwd-slug dir is unknown to the laptop). The session id is validated before it
// is placed into that command string, which is the shell-injection defense.
type RemoteTranscripts struct {
	C *config.Config
	R run.Runner
}

// Read cats the desktop transcript for the session and returns its bytes.
func (s RemoteTranscripts) Read(session string) ([]byte, error) {
	if !core.ValidSessionID(session) {
		return nil, fmt.Errorf("invalid session id %q", session)
	}
	// The glob uses ~ so the desktop shell resolves the remote home. sshx quotes
	// each remote token, so the login shell hands `sh -c` this exact command and
	// the inner sh expands the glob. session is guarded above, so it is inert.
	catCmd := "cat ~/.claude/projects/*/" + session + ".jsonl"
	remote := []string{"sh", "-c", catCmd}
	sshArgs := sshx.BuildSSHArgs(s.C, remote, sshx.Options{ConnectTimeout: true})
	out, code, err := s.R.Run("ssh", sshArgs, nil)
	if err != nil {
		return nil, fmt.Errorf("remote transcript read: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("remote transcript read exited %d: %s", code, out)
	}
	return out, nil
}

var _ Transcripts = RemoteTranscripts{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/host/ -run TestRemoteTranscripts -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/host/transcripts.go internal/host/transcripts_test.go
git commit -m "feat(host): RemoteTranscripts — read a desktop transcript over SSH (guarded)"
```

---

## Task 4: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run host + core suites**

Run: `go test ./internal/host/ ./internal/core/ -v`
Expected: PASS — all Transcripts + Repo + Runner + Source + Inventory + core tests.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/host/ ./internal/core/ && gofmt -l internal/host/ internal/core/`
Expected: vet clean; gofmt lists no files. (If gofmt lists a file, run `gofmt -w` on it and include in the commit below.)

- [ ] **Step 4: Commit any fixups** (skip if none)

```bash
git add internal/host/ internal/core/
git commit -m "test(host): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **F8 (read a remote agent's conversation from the laptop):** `RemoteTranscripts.Read` cats the desktop `.jsonl` over SSH; `LocalTranscripts.Read` reads the laptop's. ✅
- **F8 (copy across on demand):** `SaveMirror` writes pulled bytes to `<home>/.rbg/transcripts/<session>.jsonl` — the CLI composes `remote.Read(sid)` → `SaveMirror(home, sid, data)`. ✅
- **Security (glob + shell-injection):** every transcript op guards the session id with `core.ValidSessionID` before it enters a glob or a remote shell string; the remote-read test asserts NO ssh call is made for a malicious id. ✅
- **Local-is-just-another-machine:** `LocalTranscripts`/`RemoteTranscripts` behind one `Transcripts` interface. ✅
- **Testability (NFR):** local paths rooted at an injectable `Home` (tests use `t.TempDir()`); remote goes through `run.Runner` (`run.Recording`) — no real `~/.claude`, no real SSH. ✅
- **No merge (HLD non-goal):** `SaveMirror` is whole-file overwrite; no merge logic. ✅

**Deferred to later plans (not gaps here):** rendering `.jsonl` to readable text (the shipped `internal/render.Stream` does this; the CLI/dashboard calls it), attaching transcripts to inventory rows, the interactive "SyncTranscript" action wiring, and pushing a local transcript to the desktop (F8 is laptop-read-centric; a symmetric push is trivial to add later if wanted).

**Design note (documented):** pulled remote transcripts land in a dedicated `~/.rbg/transcripts/` mirror rather than being reconstructed under the remote's `<cwd-slug>` locally — the local cwd-slug for a remote path may not exist, and an rbg-owned mirror is unambiguous and clearly ours. This is a whole-file, newest-wins copy per the HLD.

**Type/name consistency:** `Transcripts`, `LocalTranscripts{Home}`, `RemoteTranscripts{C,R}`, `SaveMirror`, `transcriptGlob`, `core.ValidSessionID` — used identically across tasks and matching the verified layouts/signatures. ✅
