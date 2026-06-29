# rbg Auto-Names + Dashboard TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent names optional (auto-derived from the task on the desktop agent) and add a stdlib-only interactive dashboard that lists agents, shows the selected transcript, and attaches — so the user never has to type or memorize names.

**Architecture:** A pure `internal/slug` helper feeds agent-side name derivation+dedup. The client gains structured data fetchers (`FetchSessions`/`FetchTranscript`) that the existing stdout verbs and the new TUI both consume. The TUI is split into a pure, fully-tested state machine (`internal/tui/model.go`) and a thin build-tagged raw-terminal loop (`internal/tui/term*.go`). No external dependencies (the Go module proxy is unreachable; Bubble Tea is not an option).

**Tech Stack:** Go 1.26 stdlib only — `encoding/json`, `syscall` (termios ioctls), `os`, `bufio`, `strings`, `testing`. Raw-mode files are build-tagged per OS.

---

## Design Reference (read before starting)

**Spec:** `docs/superpowers/specs/2026-06-29-rbg-tui-and-autonames-design.md`.

**Existing signatures this plan builds on (do not change their behavior):**
- `client.Ls(c *config.Config, r run.Runner, out io.Writer) int`
- `client.Read(c *config.Config, r run.Runner, out io.Writer, name string) int`
- `client.runAgent(c, r, verb, verbArgs) ([]byte, int)` (unexported; in client pkg)
- `session.Session{Name, ClaudeSessionID, TranscriptPath, PID, StartedAt}` (JSON tags: name, claudeSessionId, transcriptPath, pid, startedAt)
- `render.Line(s string) (string, bool)` and `render.Stream([]string, io.Writer)`
- agent `Launch(out io.Writer, name, task string) int`; `Agent` has `StatePath`, store via `session.Load`
- `session.Store{Sessions map[string]Session}`, `.Get(id)`, `.Add(Session)`, `.Save()`

**Module path:** `github.com/divkov575/rbg`. Run everything from repo root
`/Users/divkov/workplace/remote-ccbg`. Build/test: `go test ./...`, `go build ./...`,
`gofmt -w .`, `go vet ./...`.

**Key decisions locked by the spec:**
- Name dedup happens on the AGENT (it owns the name set), not the client.
- TUI logic is a PURE function `Update(Model, Key) (Model, Action)` + `View(Model) string`; the terminal layer is thin and not unit-tested (integration only).
- No background polling: fetch on open, on select, on `r`.
- View + attach only; launch/send remain CLI verbs.

---

## Task 1: `internal/slug` — task → slug

**Files:**
- Create: `internal/slug/slug.go`
- Create: `internal/slug/slug_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/slug/slug_test.go`:

```go
package slug

import "testing"

func TestFromTask(t *testing.T) {
	cases := map[string]string{
		"fix the flaky payments test": "fix-flaky-payments-test",
		"Refactor the Auth Module":     "refactor-auth-module",
		"investigate a bug in setup()": "investigate-bug-setup",
		"the a an to of":               "agent", // all stopwords → fallback
		"":                             "agent",
		"!!!  @@@":                     "agent", // no alnum → fallback
		"a very long task description that keeps going and going forever":
			"very-long-task-description", // capped at 4 words
	}
	for in, want := range cases {
		if got := FromTask(in); got != want {
			t.Errorf("FromTask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromTaskMaxLen(t *testing.T) {
	got := FromTask("supercalifragilistic expialidocious")
	if len(got) > 40 {
		t.Errorf("slug too long (%d): %q", len(got), got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slug/`
Expected: FAIL — `undefined: FromTask`.

- [ ] **Step 3: Write the implementation**

Create `internal/slug/slug.go`:

```go
// Package slug derives short, filesystem- and shell-safe agent names from a
// task string. Output matches ^[a-z0-9-]+$ and is never empty (falls back to
// "agent"), so it is always a valid rbg agent id.
package slug

import "strings"

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "and": true, "is": true, "it": true,
}

const (
	maxWords = 4
	maxLen   = 40
)

// FromTask converts a free-text task into a slug: lowercase, alnum runs only,
// stopwords dropped, joined by '-', capped at maxWords / maxLen. Empty results
// fall back to "agent".
func FromTask(task string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		cur.Reset()
		if !stopwords[w] {
			words = append(words, w)
		}
	}
	for _, r := range strings.ToLower(task) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	if len(words) > maxWords {
		words = words[:maxWords]
	}
	out := strings.Join(words, "-")
	if len(out) > maxLen {
		out = out[:maxLen]
		out = strings.TrimRight(out, "-")
	}
	if out == "" {
		return "agent"
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/slug/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/slug/
git commit -m "feat(tui): slug package for task-derived agent names"
```

---

## Task 2: Agent-side name derivation + dedup

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/name_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/name_test.go`:

```go
package agent

import (
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/session"
)

func storeWith(t *testing.T, names ...string) (*session.Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sessions.json")
	s, _ := session.Load(p)
	for _, n := range names {
		s.Add(session.Session{Name: n})
	}
	return s, p
}

func TestResolveName_UsesExplicitName(t *testing.T) {
	s, _ := storeWith(t)
	if got := resolveName(s, "explicit", "some task"); got != "explicit" {
		t.Errorf("got %q, want explicit", got)
	}
}

func TestResolveName_DerivesFromTaskWhenEmpty(t *testing.T) {
	s, _ := storeWith(t)
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test" {
		t.Errorf("got %q, want fix-flaky-test", got)
	}
}

func TestResolveName_DedupsAgainstStore(t *testing.T) {
	s, _ := storeWith(t, "fix-flaky-test")
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test-2" {
		t.Errorf("got %q, want fix-flaky-test-2", got)
	}
	s.Add(session.Session{Name: "fix-flaky-test-2"})
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test-3" {
		t.Errorf("got %q, want fix-flaky-test-3", got)
	}
}

func TestResolveName_DedupsExplicitNameToo(t *testing.T) {
	s, _ := storeWith(t, "explicit")
	if got := resolveName(s, "explicit", "task"); got != "explicit-2" {
		t.Errorf("got %q, want explicit-2", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestResolveName`
Expected: FAIL — `undefined: resolveName`.

- [ ] **Step 3: Write the implementation**

Add to `internal/agent/agent.go` — first add `"github.com/divkov575/rbg/internal/slug"` to the import block (alphabetically, after `render`). Then add this function (place it just above `func (a *Agent) Launch`):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestResolveName`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/name_test.go
git commit -m "feat(tui): agent-side name derivation and dedup"
```

---

## Task 3: Wire resolveName into Launch (name becomes optional end-to-end)

**Files:**
- Modify: `internal/agent/agent.go` (the `Launch` method)
- Modify: `internal/agent/agent_test.go` (add a derived-name case)
- Modify: `cmd/rbg-agent/main.go` (launch no longer requires --name)
- Modify: `cmd/rbg/main.go` (`launch` accepts 1 or 2 positional args)
- Modify: `internal/client/client.go` (`Launch` omits --name when empty)

- [ ] **Step 1: Write the failing test (agent derives + records the slug)**

Add to `internal/agent/agent_test.go`:

```go
func TestLaunch_DerivesNameWhenEmpty(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"agents": {Stdout: []byte(`[{"name":"fix-flaky-test","sessionId":"sid-9"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	if code := a.Launch(&out, "", "fix the flaky test"); code != 0 {
		t.Fatalf("Launch code=%d out=%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("fix-flaky-test")) {
		t.Fatalf("expected derived name in output: %s", out.String())
	}
}
```

Note: the fake claude in tests echoes the `-n <name>` it is given, so the
`agents` listing must contain whatever name `Launch` passes to `BGArgs`. The
Recording stub above is static; this test asserts the DERIVED name reached the
output, which requires `Launch` to (a) load the store, (b) call `resolveName`
with empty explicit name BEFORE building `BGArgs`, and (c) pass the resolved
name to claude. The stub returns a fixed listing keyed by name `fix-flaky-test`,
so resolveName must produce exactly that for the task "fix the flaky test".

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestLaunch_DerivesNameWhenEmpty`
Expected: FAIL — Launch currently passes the empty name straight to claude, so the listing lookup for "" fails and it returns code 1.

- [ ] **Step 3: Modify `Launch` to resolve the name first**

Replace the body of `func (a *Agent) Launch(out io.Writer, name, task string) int` in `internal/agent/agent.go` with:

```go
func (a *Agent) Launch(out io.Writer, name, task string) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	resolved := resolveName(store, name, task)

	_, code, _ := a.Runner.Run(claudeBin, claudecli.BGArgs(resolved, task), nil)
	if code != 0 {
		fmt.Fprintf(out, "rbg-agent: claude --bg failed (exit %d)\n", code)
		return 1
	}
	listing, _, _ := a.Runner.Run(claudeBin, claudecli.AgentsListArgs(), nil)
	agents, _ := claudecli.ParseAgents(listing)
	sid := claudecli.FindSessionID(agents, resolved)
	if sid == "" {
		fmt.Fprintf(out, "rbg-agent: could not resolve session id for %q\n", resolved)
		return 1
	}
	store.Add(session.Session{
		Name:            resolved,
		ClaudeSessionID: sid,
		TranscriptPath:  a.transcriptPath(sid),
		StartedAt:       a.Now(),
	})
	if err := store.Save(); err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"id": resolved, "claudeSessionId": sid})
	return 0
}
```

- [ ] **Step 4: Run agent tests to verify pass**

Run: `go test ./internal/agent/`
Expected: `ok` — including the existing `TestLaunch_*` (they pass explicit names, which `resolveName` returns unchanged when not in the store) and the new derived-name test.

- [ ] **Step 5: Make `--name` optional in the agent entrypoint**

In `cmd/rbg-agent/main.go`, find the `launch` case in `parseArgs` (it currently
errors if `--name` is empty). Replace that case so only `--task` is required:

```go
	case "launch":
		inv.Name = flagValue(rest, "--name") // optional now
		inv.Task = flagValue(rest, "--task")
		if inv.Task == "" {
			return nil, errors.New("launch requires --task")
		}
```

- [ ] **Step 6: Make the client omit --name when empty**

In `internal/client/client.go`, replace `func Launch` with:

```go
// Launch starts a bg agent on the desktop and prints the agent's reply. If name
// is empty the agent derives one from the task.
func Launch(c *config.Config, r run.Runner, out io.Writer, name, task string) int {
	args := []string{"--task", task}
	if name != "" {
		args = append([]string{"--name", name}, args...)
	}
	body, code := runAgent(c, r, "launch", args)
	out.Write(body)
	return code
}
```

- [ ] **Step 7: Accept 1-or-2 positional args for `launch` in the client CLI**

In `cmd/rbg/main.go` `parse`, the `launch` and `send` cases currently share a
`len(rest) < 2` check. Split them so `launch` accepts one or two positionals:

```go
	case "launch":
		switch len(rest) {
		case 1:
			in.task = rest[0] // name auto-derived by the agent
		case 2:
			in.name, in.task = rest[0], rest[1]
		default:
			return nil, fmt.Errorf("launch requires \"<task>\" or <name> \"<task>\"")
		}
	case "send":
		if len(rest) < 2 {
			return nil, fmt.Errorf("send requires <name> <task>")
		}
		in.name, in.task = rest[0], rest[1]
```

- [ ] **Step 8: Add a CLI parse test for the 1-arg launch**

Add to `cmd/rbg/main_test.go`:

```go
func TestParse_LaunchTaskOnly(t *testing.T) {
	in, err := parse([]string{"launch", "do the thing"})
	if err != nil {
		t.Fatal(err)
	}
	if in.verb != "launch" || in.name != "" || in.task != "do the thing" {
		t.Fatalf("inv = %+v", in)
	}
}
```

- [ ] **Step 9: Verify everything builds and passes**

Run: `go build ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 10: Commit**

```bash
git add internal/agent/ internal/client/client.go cmd/rbg-agent/main.go cmd/rbg/main.go cmd/rbg/main_test.go
git commit -m "feat(tui): optional name on launch, end to end"
```

---

## Task 4: Client structured fetchers (shared by verbs + TUI)

**Files:**
- Create: `internal/client/fetch.go`
- Create: `internal/client/fetch_test.go`
- Modify: `internal/client/client.go` (Ls/Read delegate to fetchers)

- [ ] **Step 1: Write the failing test**

Create `internal/client/fetch_test.go`:

```go
package client

import (
	"testing"

	"github.com/divkov575/rbg/internal/run"
)

func TestFetchSessions_ParsesLsJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"ls": {Stdout: []byte(`[{"name":"alpha","claudeSessionId":"sid-1","transcriptPath":"/t/a"},{"name":"beta","claudeSessionId":"sid-2"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	sessions, err := FetchSessions(cfg(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].Name != "alpha" || sessions[1].ClaudeSessionID != "sid-2" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestFetchSessions_NonZeroExitErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 1, Stdout: []byte("boom")}}
	if _, err := FetchSessions(cfg(), r); err == nil {
		t.Fatal("expected error on nonzero exit")
	}
}

func TestFetchTranscript_ReturnsRenderedText(t *testing.T) {
	transcript := `{"message":{"role":"user","content":"q"}}` + "\n" +
		`{"message":{"role":"assistant","content":"a"}}` + "\n"
	r := &run.Recording{
		BySubstring: map[string]run.Result{"read": {Stdout: []byte(transcript)}},
		Default:     run.Result{Code: 0},
	}
	text, err := FetchTranscript(cfg(), r, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if text != "user: q\nassistant: a\n" {
		t.Fatalf("text = %q", text)
	}
}
```

Note: `cfg()` is the helper already defined in `internal/client/client_test.go`
(same package), returning `&config.Config{Host:"desk", AgentPath:"rbg-agent"}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run Fetch`
Expected: FAIL — `undefined: FetchSessions`, `undefined: FetchTranscript`.

- [ ] **Step 3: Write the fetchers**

Create `internal/client/fetch.go`:

```go
package client

import (
	"encoding/json"
	"fmt"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/render"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// FetchSessions returns the desktop's recorded sessions as structured data.
func FetchSessions(c *config.Config, r run.Runner) ([]session.Session, error) {
	body, code := runAgent(c, r, "ls", nil)
	if code != 0 {
		return nil, fmt.Errorf("ls failed (exit %d): %s", code, body)
	}
	var sessions []session.Session
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, fmt.Errorf("parse ls output: %w", err)
	}
	return sessions, nil
}

// FetchTranscript returns the named agent's transcript as rendered text. The
// agent's `read` already renders, so we pass its output through verbatim.
func FetchTranscript(c *config.Config, r run.Runner, name string) (string, error) {
	body, code := runAgent(c, r, "read", []string{"--id", name})
	if code != 0 {
		return "", fmt.Errorf("read failed (exit %d): %s", code, body)
	}
	return string(body), nil
}

var _ = render.Line // render stays the agent's responsibility; kept for import parity
```

Wait — `render` is not actually used in fetch.go; remove that last line and the
`render` import. Final `fetch.go` import block is:

```go
import (
	"encoding/json"
	"fmt"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)
```

and delete the `var _ = render.Line` line entirely.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run Fetch`
Expected: `ok`.

- [ ] **Step 5: Refactor Ls/Read to delegate (DRY)**

In `internal/client/client.go`, replace `func Ls` and `func Read` with versions
that reuse the fetchers:

```go
// Ls prints the desktop's session list as JSON (one object per line is not
// required; this preserves prior behavior by re-emitting the array).
func Ls(c *config.Config, r run.Runner, out io.Writer) int {
	sessions, err := FetchSessions(c, r)
	if err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.Encode(sessions)
	return 0
}

// Read prints the named agent's transcript (already rendered by the agent).
func Read(c *config.Config, r run.Runner, out io.Writer, name string) int {
	text, err := FetchTranscript(c, r, name)
	if err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprint(out, text)
	return 0
}
```

Add `"encoding/json"` to `client.go`'s import block (after `fmt`).

- [ ] **Step 6: Verify the existing client tests still pass**

Run: `go test ./internal/client/`
Expected: `ok`. Note: `TestLs_RendersAgentJSON` and `TestRead_RendersStreamedTranscript`
assert substrings ("alpha", "a") which both new implementations still satisfy.

- [ ] **Step 7: Commit**

```bash
git add internal/client/
git commit -m "feat(tui): structured client fetchers shared by verbs and TUI"
```

---

## Task 5: TUI pure model — state + Update + View

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/model_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/session"
)

func sample() Model {
	return New([]session.Session{
		{Name: "alpha", ClaudeSessionID: "sid-1"},
		{Name: "beta", ClaudeSessionID: "sid-2"},
		{Name: "gamma", ClaudeSessionID: "sid-3"},
	})
}

func TestDownUpMovesSelection(t *testing.T) {
	m := sample()
	if m.Selected != 0 {
		t.Fatalf("start selected = %d", m.Selected)
	}
	m, _ = Update(m, KeyDown)
	m, _ = Update(m, KeyDown)
	if m.Selected != 2 {
		t.Fatalf("after 2 downs selected = %d", m.Selected)
	}
	m, _ = Update(m, KeyDown) // clamp at bottom
	if m.Selected != 2 {
		t.Fatalf("should clamp at last, got %d", m.Selected)
	}
	m, _ = Update(m, KeyUp)
	if m.Selected != 1 {
		t.Fatalf("after up selected = %d", m.Selected)
	}
}

func TestViewLoadsTranscriptIntoPane(t *testing.T) {
	m := sample()
	m, act := Update(m, KeyView)
	if act != ActionLoadTranscript {
		t.Fatalf("KeyView action = %v, want ActionLoadTranscript", act)
	}
	// the loop fulfills the action by calling SetTranscript:
	m = m.SetTranscript("user: hi\nassistant: yo\n")
	if !strings.Contains(View(m), "assistant: yo") {
		t.Fatalf("transcript not rendered in view:\n%s", View(m))
	}
}

func TestAttackKeyYieldsAttachAction(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyDown) // select beta
	_, act := Update(m, KeyAttach)
	if act != ActionAttach {
		t.Fatalf("KeyAttach action = %v, want ActionAttach", act)
	}
}

func TestRefreshAndQuitActions(t *testing.T) {
	m := sample()
	if _, act := Update(m, KeyRefresh); act != ActionRefresh {
		t.Fatalf("refresh action = %v", act)
	}
	if _, act := Update(m, KeyQuit); act != ActionQuit {
		t.Fatalf("quit action = %v", act)
	}
}

func TestSelectedName(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyDown)
	if m.SelectedName() != "beta" {
		t.Fatalf("SelectedName = %q", m.SelectedName())
	}
}

func TestEmptyModelIsSafe(t *testing.T) {
	m := New(nil)
	m, act := Update(m, KeyView) // nothing to load
	if act != ActionNone {
		t.Fatalf("empty view action = %v, want ActionNone", act)
	}
	if m.SelectedName() != "" {
		t.Fatalf("empty SelectedName = %q", m.SelectedName())
	}
	_ = View(m) // must not panic
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/`
Expected: FAIL — `undefined: New`, `undefined: Update`, etc.

- [ ] **Step 3: Write the pure model**

Create `internal/tui/model.go`:

```go
// Package tui holds the rbg dashboard. model.go is the PURE state machine —
// no terminal, no I/O — so it is fully unit-testable. term*.go drives it.
package tui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/session"
)

// Key is an abstract key event fed to Update (decoded from raw bytes by the
// terminal layer).
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyView    // ⏎ or v: load selected transcript
	KeyAttach  // a
	KeyRefresh // r
	KeyQuit    // q
)

// Action is what the terminal loop must do after an Update (the model itself
// performs no I/O).
type Action int

const (
	ActionNone Action = iota
	ActionLoadTranscript
	ActionAttach
	ActionRefresh
	ActionQuit
)

// Model is the dashboard state.
type Model struct {
	Sessions   []session.Session
	Selected   int
	Transcript string // rendered text of the currently-shown transcript
}

// New builds a model from a session list.
func New(sessions []session.Session) Model {
	return Model{Sessions: sessions}
}

// SelectedName returns the highlighted agent's name, or "" if none.
func (m Model) SelectedName() string {
	if len(m.Sessions) == 0 || m.Selected < 0 || m.Selected >= len(m.Sessions) {
		return ""
	}
	return m.Sessions[m.Selected].Name
}

// SetSessions replaces the list (after a refresh), clamping Selected.
func (m Model) SetSessions(s []session.Session) Model {
	m.Sessions = s
	if m.Selected >= len(s) {
		m.Selected = len(s) - 1
	}
	if m.Selected < 0 {
		m.Selected = 0
	}
	return m
}

// SetTranscript stores rendered transcript text for the right pane.
func (m Model) SetTranscript(text string) Model {
	m.Transcript = text
	return m
}

// Update applies a key, returning the new model and an Action for the loop.
func Update(m Model, k Key) (Model, Action) {
	switch k {
	case KeyUp:
		if m.Selected > 0 {
			m.Selected--
		}
	case KeyDown:
		if m.Selected < len(m.Sessions)-1 {
			m.Selected++
		}
	case KeyView:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionLoadTranscript
	case KeyAttach:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionAttach
	case KeyRefresh:
		return m, ActionRefresh
	case KeyQuit:
		return m, ActionQuit
	}
	return m, ActionNone
}

// View renders the two-pane dashboard to a string.
func View(m Model) string {
	var b strings.Builder
	b.WriteString("agents:\n")
	for i, s := range m.Sessions {
		cursor := "  "
		if i == m.Selected {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, s.Name))
	}
	b.WriteString("\n[↑/↓ move  ⏎/v view  a attach  r refresh  q quit]\n")
	b.WriteString("\ntranscript:\n")
	if m.Transcript == "" {
		b.WriteString("(press ⏎ to load)\n")
	} else {
		b.WriteString(m.Transcript)
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): pure dashboard model (Update/View, fully tested)"
```

---

## Task 6: Raw-terminal layer + key decoding

**Files:**
- Create: `internal/tui/term.go` (OS-independent: key decoding, run loop skeleton)
- Create: `internal/tui/term_darwin.go` (build-tagged raw mode)
- Create: `internal/tui/term_linux.go` (build-tagged raw mode)
- Create: `internal/tui/decode_test.go`

- [ ] **Step 1: Write the failing test (key decoding is pure & testable)**

Create `internal/tui/decode_test.go`:

```go
package tui

import "testing"

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   []byte
		want Key
	}{
		{[]byte("\x1b[A"), KeyUp},
		{[]byte("\x1b[B"), KeyDown},
		{[]byte("k"), KeyUp},
		{[]byte("j"), KeyDown},
		{[]byte("\r"), KeyView},
		{[]byte("v"), KeyView},
		{[]byte("a"), KeyAttach},
		{[]byte("r"), KeyRefresh},
		{[]byte("q"), KeyQuit},
		{[]byte("\x03"), KeyQuit}, // Ctrl-C
		{[]byte("x"), KeyNone},
	}
	for _, c := range cases {
		if got := decodeKey(c.in); got != c.want {
			t.Errorf("decodeKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestDecodeKey`
Expected: FAIL — `undefined: decodeKey`.

- [ ] **Step 3: Write the OS-independent term.go**

Create `internal/tui/term.go`:

```go
package tui

import (
	"fmt"
	"io"
	"os"
)

// decodeKey maps a raw input chunk to an abstract Key. Arrow keys arrive as the
// 3-byte escape sequences ESC [ A/B; we also accept vi-style and letter keys.
func decodeKey(b []byte) Key {
	if len(b) == 0 {
		return KeyNone
	}
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp
		case 'B':
			return KeyDown
		}
		return KeyNone
	}
	switch b[0] {
	case 'k':
		return KeyUp
	case 'j':
		return KeyDown
	case '\r', '\n', 'v':
		return KeyView
	case 'a':
		return KeyAttach
	case 'r':
		return KeyRefresh
	case 'q', 0x03: // q or Ctrl-C
		return KeyQuit
	}
	return KeyNone
}

const clearScreen = "\x1b[2J\x1b[H" // clear + cursor home

// draw renders the model to w, clearing first.
func draw(w io.Writer, m Model) {
	fmt.Fprint(w, clearScreen)
	fmt.Fprint(w, View(m))
}

// readKey reads one key event from r (a raw-mode fd). Returns KeyNone on EOF.
func readKey(r io.Reader) Key {
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n == 0 {
		return KeyQuit // treat read failure/EOF as quit
	}
	return decodeKey(buf[:n])
}

// Stdio bundles the loop's I/O endpoints (injectable for clarity/testing).
type Stdio struct {
	In  io.Reader
	Out io.Writer
}

// DefaultStdio uses the process terminal.
func DefaultStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout} }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestDecodeKey`
Expected: `ok`.

- [ ] **Step 5: Write the build-tagged raw-mode files**

Create `internal/tui/term_darwin.go`:

```go
//go:build darwin

package tui

import (
	"syscall"
	"unsafe"
)

// rawMode puts fd into raw mode and returns a restore func. Darwin uses
// TIOCGETA/TIOCSETA for the termios get/set ioctls.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TIOCSETA, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TIOCSETA, &old) }, nil
}

func ioctl(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}
```

Create `internal/tui/term_linux.go`:

```go
//go:build linux

package tui

import (
	"syscall"
	"unsafe"
)

// rawMode puts fd into raw mode and returns a restore func. Linux uses
// TCGETS/TCSETS for the termios get/set ioctls.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, &old) }, nil
}

func ioctl(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}
```

- [ ] **Step 6: Verify the package builds on this host**

Run: `go build ./internal/tui/ && go test ./internal/tui/`
Expected: builds (the darwin file compiles here) and tests pass. To confirm the
linux file at least compiles, run: `GOOS=linux GOARCH=amd64 go build ./internal/tui/`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/term.go internal/tui/term_darwin.go internal/tui/term_linux.go internal/tui/decode_test.go
git commit -m "feat(tui): stdlib raw-terminal layer and key decoding"
```

---

## Task 7: Dashboard run loop (wires model + term + fetchers + attach)

**Files:**
- Create: `internal/tui/run.go`
- Create: `cmd/rbg/dash.go`
- Modify: `cmd/rbg/main.go` (no-args default + `dash` verb)
- Modify: `cmd/rbg/main_test.go` (parse `dash`)

- [ ] **Step 1: Write the failing test (parse dash verb)**

Add to `cmd/rbg/main_test.go`:

```go
func TestParse_Dash(t *testing.T) {
	in, err := parse([]string{"dash"})
	if err != nil || in.verb != "dash" {
		t.Fatalf("dash: inv=%+v err=%v", in, err)
	}
}

func TestParse_NoArgsDefaultsToDash(t *testing.T) {
	in, err := parse([]string{})
	if err != nil || in.verb != "dash" {
		t.Fatalf("no-args: inv=%+v err=%v", in, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rbg/ -run TestParse_Dash`
Expected: FAIL — `parse([]string{})` currently returns a usage error, and `dash` is an unknown verb.

- [ ] **Step 3: Make `parse` accept no-args and `dash`**

In `cmd/rbg/main.go` `parse`, change the empty-args guard and add the `dash`
verb. Replace the opening of `parse`:

```go
func parse(args []string) (*inv, error) {
	if len(args) == 0 {
		return &inv{verb: "dash"}, nil // no args → dashboard
	}
	in := &inv{verb: args[0]}
	rest := args[1:]
	switch in.verb {
	case "ls", "ping", "deploy", "dash":
		return in, nil
```

(the rest of the switch stays as-is)

- [ ] **Step 4: Run the parse tests**

Run: `go test ./cmd/rbg/ -run TestParse`
Expected: `ok`.

- [ ] **Step 5: Write the run loop**

Create `internal/tui/run.go`:

```go
package tui

import (
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// Deps are the loop's collaborators, injected so the loop stays thin.
type Deps struct {
	Fetch      func() ([]Session, error)            // list agents
	Transcript func(name string) (string, error)    // rendered transcript
	Attach     func(name string) error              // hand terminal to claude
}

// Session is re-exported minimal shape the loop needs (kept local to avoid a
// hard import cycle in tests; mirrors session.Session fields used by the model).
type Session = sessionAlias

// Run drives the dashboard until the user quits. It enters raw mode on the
// terminal fd, draws on each key, and fulfills model Actions via deps.
func Run(d Deps, io Stdio) error {
	sessions, err := d.Fetch()
	if err != nil {
		return err
	}
	m := New(sessions)

	restore, err := rawMode(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer restore()

	draw(io.Out, m)
	for {
		k := readKey(io.In)
		var act Action
		m, act = Update(m, k)
		switch act {
		case ActionQuit:
			return nil
		case ActionLoadTranscript:
			if text, err := d.Transcript(m.SelectedName()); err == nil {
				m = m.SetTranscript(text)
			}
		case ActionRefresh:
			if s, err := d.Fetch(); err == nil {
				m = m.SetSessions(s)
			}
		case ActionAttach:
			name := m.SelectedName()
			restore()                 // cooked mode for interactive claude
			_ = d.Attach(name)        // blocks until the user exits claude
			restore, _ = rawMode(os.Stdin.Fd()) // back to raw
		}
		draw(io.Out, m)
	}
}

// NewDeps builds Deps from config + runner + an attach func, using the client
// fetchers. Declared here so cmd/rbg can wire it without re-importing client in
// the loop. The actual client wiring lives in cmd/rbg/dash.go.
var _ = config.Config{}
var _ = run.Exec{}
```

The `sessionAlias` and the cross-package wiring need care: to keep the model
typed on `session.Session` (Task 5) AND avoid an import cycle, define the alias
in model.go instead. Apply this small change to `internal/tui/model.go`:

- Add import `"github.com/divkov575/rbg/internal/session"` (already present from Task 5).
- Below the imports add: `type sessionAlias = session.Session`

Then in `run.go` remove the `var _ = config.Config{}` / `var _ = run.Exec{}`
lines and the `config`/`run` imports — they are not used by the loop (wiring is
in cmd/rbg). Final `run.go` imports are just `"os"`. And change `Fetch func()
([]Session, error)` to `Fetch func() ([]session.Session, error)` by importing
session; simplest: drop the `Session` alias entirely and type Deps directly:

```go
import (
	"os"

	"github.com/divkov575/rbg/internal/session"
)

type Deps struct {
	Fetch      func() ([]session.Session, error)
	Transcript func(name string) (string, error)
	Attach     func(name string) error
}
```

(Delete the `Session = sessionAlias` line and the `sessionAlias` note above;
they were a dead-end. The model already uses `session.Session`, so `Deps` using
it directly is consistent and cycle-free — `tui` imports `session`, not the
reverse.)

- [ ] **Step 6: Write the cmd wiring**

Create `cmd/rbg/dash.go`:

```go
package main

import (
	"github.com/divkov575/rbg/internal/client"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
	"github.com/divkov575/rbg/internal/tui"
)

// dash launches the interactive dashboard.
func dash(cfg *config.Config, r run.Runner) int {
	deps := tui.Deps{
		Fetch: func() ([]session.Session, error) {
			return client.FetchSessions(cfg, r)
		},
		Transcript: func(name string) (string, error) {
			return client.FetchTranscript(cfg, r, name)
		},
		Attach: func(name string) error {
			// reuse the existing attach path (resolves id + ssh -t claude).
			if code := attach(cfg, r, name); code != 0 {
				return errAttach
			}
			return nil
		},
	}
	if err := tui.Run(deps, tui.DefaultStdio()); err != nil {
		return 1
	}
	return 0
}
```

Add the sentinel error to `cmd/rbg/support.go` (append at end):

```go
import "errors" // add to support.go's import block if not already present

var errAttach = errors.New("attach failed")
```

(If `support.go` already imports `errors`, don't duplicate; just add the var.)

- [ ] **Step 7: Dispatch `dash` in main**

In `cmd/rbg/main.go` `main`, add a case to the dispatch switch:

```go
	case "dash":
		os.Exit(dash(cfg, r))
```

- [ ] **Step 8: Build and test the whole module**

Run: `go build ./... && go test ./...`
Expected: all `ok`. Also: `GOOS=linux GOARCH=amd64 go build ./...` → no errors.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/run.go internal/tui/model.go cmd/rbg/dash.go cmd/rbg/support.go cmd/rbg/main.go cmd/rbg/main_test.go
git commit -m "feat(tui): dashboard run loop and cmd wiring"
```

---

## Task 8: gofmt + vet + full sweep

**Files:** (whole module)

- [ ] **Step 1: Format and vet**

Run:
```bash
gofmt -w .
go vet ./...
```
Expected: `gofmt -l .` prints nothing; `go vet` reports nothing.

- [ ] **Step 2: Full test run + cross-compile check**

Run: `go test ./... && GOOS=linux GOARCH=amd64 go build ./...`
Expected: all packages `ok`; linux build succeeds.

- [ ] **Step 3: Commit (only if gofmt changed anything)**

```bash
git add -A
git commit -m "chore(tui): gofmt and vet" || echo "nothing to format"
```

---

## Task 9: Integration — dashboard data path + auto-name over SSH

**Files:**
- Create: `test/integration_v2/test_tui_integration.py`

This reuses the v2 sandboxed sshd harness. It does NOT drive the interactive
raw-terminal loop (no PTY in the harness); it verifies the two things the TUI
depends on that cross SSH: (a) launch with NO name auto-derives a slug, and
(b) the data the dashboard reads (`ls` JSON, `read` text) is correct end to end.

- [ ] **Step 1: Write the test**

Create `test/integration_v2/test_tui_integration.py`:

```python
"""Integration coverage for the TUI's data path and auto-naming, over real SSH.

The interactive raw-terminal loop needs a PTY and is not driven here; instead we
assert the cross-SSH behaviors the dashboard relies on: auto-derived names on
launch, and correct ls/read data.
"""
import json
import os
import shutil
import subprocess
import sys

import pytest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))

sys.path.insert(0, HERE)
from v2_harness import SimDesktop, SSHD_BIN  # noqa: E402

pytestmark = pytest.mark.integration

_REASON = None
if shutil.which("go") is None:
    _REASON = "go not available"
elif not os.path.exists(SSHD_BIN):
    _REASON = f"{SSHD_BIN} not available"
needs_env = pytest.mark.skipif(_REASON is not None, reason=_REASON or "")


def _build(name, tmp):
    out = os.path.join(tmp, name)
    res = subprocess.run(
        ["go", "build", "-o", out, f"github.com/divkov575/rbg/cmd/{name}"],
        cwd=REPO_ROOT, capture_output=True, text=True,
    )
    assert res.returncode == 0, f"build {name} failed: {res.stderr}"
    return out


@pytest.fixture
def env(tmp_path):
    rbg = _build("rbg", str(tmp_path))
    agent = _build("rbg-agent", str(tmp_path))
    sim = SimDesktop(str(tmp_path / "sim"))
    sim.start()
    sim.install_agent(agent)
    yield sim, rbg
    sim.stop()


def run_rbg(sim, rbg, client_home, *args, timeout=30):
    e = sim.rbg_env()
    e["HOME"] = str(client_home)
    e["RBG_AGENT_PATH"] = "rbg-agent"
    return subprocess.run([rbg, *args], env=e, capture_output=True, text=True, timeout=timeout)


@needs_env
def test_launch_without_name_autoderives_slug(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    res = run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    assert res.returncode == 0, res.stderr
    assert "fix-flaky-test" in res.stdout


@needs_env
def test_two_unnamed_launches_dedup(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    ls = run_rbg(sim, rbg, ch, "ls")
    assert ls.returncode == 0, ls.stderr
    names = [s["name"] for s in json.loads(ls.stdout)]
    assert "fix-flaky-test" in names
    assert "fix-flaky-test-2" in names


@needs_env
def test_ls_json_is_dashboard_consumable(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "say hello")
    ls = run_rbg(sim, rbg, ch, "ls")
    data = json.loads(ls.stdout)  # must be a JSON array of objects with name + claudeSessionId
    assert isinstance(data, list) and data
    assert "name" in data[0] and "claudeSessionId" in data[0]
```

- [ ] **Step 2: Run the integration suite**

Run: `pytest test/integration_v2/test_tui_integration.py -m integration -v`
Expected: 3 passed (or skipped cleanly if `go`/`sshd` unavailable).

- [ ] **Step 3: Run the FULL integration suite to confirm no regressions**

Run: `pytest -m integration -q`
Expected: all pass (v1 8 + v2 6 + these 3 = 17, plus any others).

- [ ] **Step 4: Commit**

```bash
git add test/integration_v2/test_tui_integration.py
git commit -m "test(tui): integration coverage for auto-name and dashboard data path"
```

---

## Task 10: Manual smoke (deferred — needs a real terminal)

**Files:** none (manual).

The pure model and data paths are covered by unit + integration tests, but the
raw-terminal rendering can only be eyeballed on a real TTY.

- [ ] **Step 1: Build and run the dashboard locally**

```bash
go build -o /tmp/rbg ./cmd/rbg
export RBG_HOST=<your-desktop>   # and RBG_SSH / RBG_CWD as needed
/tmp/rbg            # no args → dashboard
```

- [ ] **Step 2: Verify interactively**
- Arrow ↑/↓ (and j/k) move the selection.
- ⏎ or `v` loads the selected agent's transcript into the right area.
- `r` refreshes the list.
- `a` drops into interactive claude for the selected agent; on exit you return
  to the dashboard with the terminal intact (no broken echo / cooked mode).
- `q` (or Ctrl-C) exits cleanly and the terminal is restored (typing echoes
  normally afterward).

- [ ] **Step 3: Verify auto-name UX**

```bash
/tmp/rbg launch "fix the flaky payments test"   # prints e.g. "fix-flaky-payments-test"
/tmp/rbg launch "fix the flaky payments test"   # prints "...-2"
```

---

## Self-Review

**1. Spec coverage:**
- Auto-name, name optional on launch → Tasks 1 (slug), 2 (resolveName), 3 (wired end-to-end: agent, client, both CLIs). ✓
- Dedup on the agent → Task 2 + Task 3 (Launch loads store, resolveName dedups). ✓
- Dashboard, two panes, list + transcript → Task 5 (model+View). ✓
- Keys ↑/↓/⏎/v/a/r/q → Task 5 (Update) + Task 6 (decodeKey). ✓
- View + attach only; launch/send stay CLI → Task 5 (no launch/send actions) + Task 7 (attach via existing path). ✓
- Attach suspends/restores terminal → Task 7 (restore→attach→re-raw). ✓
- No background polling; fetch on open/select/r → Task 7 (Fetch on start, ActionRefresh, ActionLoadTranscript). ✓
- stdlib only, raw mode, build-tagged per OS → Task 6 (term_darwin/linux). ✓
- Structured fetchers shared by verbs + TUI → Task 4. ✓
- TUI pure logic unit-tested, term layer integration-only → Tasks 5/6 (pure tests) + Task 9 (integration). ✓
- No deps added → confirmed: only stdlib + existing internal packages. ✓

**2. Placeholder scan:** No TBD/TODO. One hazard addressed: Task 7 Step 5
initially sketched a `sessionAlias`/`Session` re-export that creates confusion;
the step explicitly walks the implementer to the final cycle-free form (`Deps`
typed directly on `session.Session`, `tui` imports `session`). All code blocks
are complete.

**3. Type consistency:**
- `slug.FromTask(string) string` — Tasks 1, 2. ✓
- `resolveName(*session.Store, explicit, task string) string` — Tasks 2, 3. ✓
- `client.FetchSessions(*config.Config, run.Runner) ([]session.Session, error)` and `client.FetchTranscript(*config.Config, run.Runner, string) (string, error)` — Tasks 4, 7. ✓
- `tui.Model`, `tui.New([]session.Session) Model`, `Update(Model, Key) (Model, Action)`, `View(Model) string`, `Model.SelectedName()`, `Model.SetSessions`, `Model.SetTranscript` — Tasks 5, 6, 7. ✓
- `tui.Key` constants (KeyUp/Down/View/Attach/Refresh/Quit/None) — Tasks 5, 6. ✓
- `tui.Action` constants (ActionNone/LoadTranscript/Attach/Refresh/Quit) — Tasks 5, 7. ✓
- `tui.Deps{Fetch, Transcript, Attach}`, `tui.Run(Deps, Stdio)`, `tui.Stdio{In,Out}`, `tui.DefaultStdio()` — Tasks 7. ✓
- `decodeKey([]byte) Key`, `rawMode(uintptr) (func(), error)`, `draw`, `readKey` — Task 6. ✓
- existing `attach(cfg, r, name) int` reused by Task 7's Deps.Attach. ✓
