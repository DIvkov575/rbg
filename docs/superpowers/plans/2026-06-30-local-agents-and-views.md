# Local Agents + View Ergonomics — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make local agents first-class and persistent — create (incl. blank) agents pinned to a repo, run them manually later with a repo-sync-first step — and add view ergonomics (remote / local / combined / project) to the dashboard.

**Architecture:** A new client-only `internal/localagent` package (store + run logic, mirroring `internal/queue`) — ALREADY PROTOTYPED AND TESTED on this branch. The dashboard gains a `View` enum (pure model) re-slicing the agent population; local-agent actions and a `raw local <verb>` CLI group. stdlib only.

**Tech Stack:** Go 1.26 stdlib; existing internal packages; pytest integration. No new deps.

---

## Design Reference

**Spec:** `docs/superpowers/specs/2026-06-30-local-agents-and-views-design.md` — read it first.

**Already done on branch `feat/local-agents` (verify, don't rewrite):**
- `internal/localagent/localagent.go` + `_test.go` — `Store{Load,Add,Get,Delete,List,Save}`, `Agent{Name,Repo,Task,LastRun,LastTask}`. Tests green.
- `internal/localagent/run.go` + `_test.go` — `RepoDir`, `PlanRun(a, task, dirHasGit)`, `Run(ex Exec, a, task)`, `Exec` seam. Tests green.

**Existing patterns to mirror:**
- CLI verbs: `cmd/rbg/main.go` `parse()`/`main()` switch; help in `usage()`.
- TUI pure model: `internal/tui/model.go` (Update→Action, View, screen flags). Loop: `internal/tui/run.go` (Deps, decode-by-mode). Decode: `internal/tui/term.go`.
- Store+deps wiring: `cmd/rbg/dash.go`.
- Local fire-and-forget today: `dispatchLocal`/`localRepoDir` in `cmd/rbg/dash.go` (will be partly superseded by `localagent.Run`).

**File-writing note:** prefer Edit/Write; if blocked by bg-isolation, use Bash heredocs / `python3 - <<'PY'`.

---

## Task 1: Confirm the localagent core (already built)

- [ ] **Step 1:** Run `go test ./internal/localagent/` — expect ok. If the package is absent (fresh checkout), create it from the spec's "validated core" description.
- [ ] **Step 2:** `go vet ./internal/localagent/ && gofmt -l internal/localagent/` — clean.
- [ ] **Step 3:** Commit if uncommitted: `git add internal/localagent/ && git commit -m "feat(localagent): persistent local-agent store + sync-then-run logic"`

---

## Task 2: `rbg raw local <verb>` CLI group (TTY-free, agent-usable)

**Files:** Create `cmd/rbg/local.go`, `cmd/rbg/local_test.go`; Modify `cmd/rbg/main.go`.

- [ ] **Step 1: Failing test** — in `cmd/rbg/local_test.go`, test a `parseLocal(args)` helper:
```go
func TestParseLocal(t *testing.T) {
	cases := []struct{ args []string; sub, name, repo, task string; err bool }{
		{[]string{"add", "fixer", "github.com/me/svc"}, "add", "fixer", "github.com/me/svc", "", false},
		{[]string{"add", "fixer", "repo", "fix the bug"}, "add", "fixer", "repo", "fix the bug", false},
		{[]string{"ls"}, "ls", "", "", "", false},
		{[]string{"run", "fixer"}, "run", "fixer", "", "", false},
		{[]string{"run", "fixer", "new task"}, "run", "fixer", "", "new task", false},
		{[]string{"rm", "fixer"}, "rm", "fixer", "", "", false},
		{[]string{"show", "fixer"}, "show", "fixer", "", "", false},
		{[]string{"add", "onlyname"}, "", "", "", "", true}, // add needs a repo
		{[]string{"bogus"}, "", "", "", "", true},
	}
	for _, c := range cases {
		got, err := parseLocal(c.args)
		if c.err { if err == nil { t.Errorf("%v: want err", c.args) }; continue }
		if err != nil || got.sub != c.sub || got.name != c.name || got.repo != c.repo || got.task != c.task {
			t.Errorf("%v → %+v err=%v", c.args, got, err)
		}
	}
}
```
- [ ] **Step 2:** `go test ./cmd/rbg/ -run TestParseLocal` → FAIL (undefined).
- [ ] **Step 3: Implement** `cmd/rbg/local.go`:
  - `type localInv struct{ sub, name, repo, task string }`
  - `func parseLocal(args []string) (localInv, error)` — sub-verbs add/ls/run/rm/show; validate required args (add needs name+repo; run/rm/show need name).
  - `func localCmd(args []string) int` — `parseLocal`, then operate on `localagent.Load(localAgentsPath())`:
    - `add`: `s.Add(localagent.Agent{Name,Repo,Task})`, Save, print "added <name>".
    - `ls`: print each `List()` agent as `name\trepo\tlastRun\ttask`.
    - `run`: load agent; `localagent.Run(execImpl{}, a, task)`; on success update `LastRun`(now)/`LastTask`, Save, print "ran <name>"; on error print to stderr, return 1.
    - `rm`: Delete, Save.
    - `show`: print the agent's full record (incl. task).
  - `func localAgentsPath() string { return os.ExpandEnv("$HOME/.rbg/local-agents.json") }`
  - `type execImpl struct{}` implementing `localagent.Exec` via `os/exec` (claude step detached, like `dispatchLocal`; git steps synchronous).
- [ ] **Step 4:** In `cmd/rbg/main.go`: add `localArgs []string` to `inv`; in `parse()` add `case "local": in.localArgs = rest; return in, nil`; in `main()` BEFORE `config.Load`, add `if in.verb == "local" { os.Exit(localCmd(in.localArgs)) }` (local agents need no RBG_HOST). Add `local` to `usage()`.
- [ ] **Step 5:** `go test ./cmd/rbg/` → ok. Manual: `rbg raw local add t /tmp/x "say hi"; rbg raw local ls; rbg raw local show t; rbg raw local rm t`.
- [ ] **Step 6:** Commit: `feat(cli): rbg raw local add/ls/run/rm/show`.

---

## Task 3: View enum in the pure model

**Files:** Modify `internal/tui/model.go`, `internal/tui/model_test.go`.

- [ ] **Step 1: Failing tests** — add to `model_test.go`:
```go
func TestViewCycle(t *testing.T) {
	m := sample()
	if m.View != ViewRemote { t.Fatal("default view should be Remote") }
	m, _ = Update(m, KeyView) // cycle
	if m.View != ViewLocal { t.Fatalf("after cycle = %v", m.View) }
	m, _ = Update(m, KeyView); m, _ = Update(m, KeyView); m, _ = Update(m, KeyView)
	if m.View != ViewRemote { t.Fatal("cycle should wrap to Remote") }
}
func TestSetLocalAgents(t *testing.T) {
	m := sample().SetLocalAgents([]LocalAgentItem{{Name:"a",Repo:"r",LastRun:"2026-06-30T00:00:00Z"}})
	if len(m.LocalAgents) != 1 { t.Fatal("local agents not set") }
}
```
(Reuse the `KeyView` constant if free; the old `KeyView` was removed earlier — re-add it as the view-cycle key, decoded from `Tab`/`\t` and `1..4`. Confirm no clash via grep.)
- [ ] **Step 2:** Run → FAIL (`ViewRemote`/`m.View`/`SetLocalAgents` undefined).
- [ ] **Step 3: Implement** in `model.go`:
  - `type ViewMode int; const (ViewRemote ViewMode = iota; ViewLocal; ViewCombined; ViewProject)`
  - `Model` fields: `View ViewMode`, `LocalAgents []LocalAgentItem`.
  - `type LocalAgentItem struct{ Name, Repo, Task, LastRun string }`
  - `SetLocalAgents([]LocalAgentItem) Model`.
  - In `Update` normal branch: `KeyView` cycles `m.View = (m.View+1)%4`. (Number keys optional.)
  - This task is state only; rendering is Task 4.
- [ ] **Step 4:** `go test ./internal/tui/` → ok.
- [ ] **Step 5:** Commit: `feat(tui): view-mode enum (remote/local/combined/project)`.

---

## Task 4: Render the four views

**Files:** Modify `internal/tui/model.go` (View), `internal/tui/model_test.go`.

- [ ] **Step 1: Failing tests** — assert each view's `View(m)` output:
  - `ViewLocal` lists local-agent names + their repo + last-run.
  - `ViewCombined` shows "REMOTE" and "LOCAL" section headers.
  - `ViewProject` groups by repo: a repo header with its remote sessions and local agents under it.
  - Footer hints differ per view (e.g. local view shows "run/edit/delete", remote shows "attach/kill").
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3: Implement** — extend `View(m)` to branch on `m.View`. Add `localView`, `combinedView`, `projectView` render helpers mirroring the existing bordered-pane style (`labelRule`/`padTo`/`displayWidth`/`wrapText`). Project grouping key = repo string (spec open-Q2: repo URL). Keep within width.
- [ ] **Step 4:** `go test ./internal/tui/` → ok. Render-demo each view to eyeball (temp `zz_demo_test.go`, then delete).
- [ ] **Step 5:** Commit: `feat(tui): render remote/local/combined/project views`.

---

## Task 5: Local-agent actions in the dashboard (run / create-blank / delete)

**Files:** Modify `internal/tui/model.go`, `term.go`, `run.go`, `cmd/rbg/dash.go`, tests.

- [ ] **Step 1: Failing model tests** — in `ViewLocal`: a key (`Enter` or `R`) on a selected local agent → `ActionRunLocal` carrying the selected agent; `D` → `ActionDeleteLocal`; the existing `n`-create flow, when `View==ViewLocal`, allows an EMPTY task to create a *blank* agent (Enter on empty task → `ActionCreateLocalBlank` instead of refusing).
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3: Implement:**
  - model: `ActionRunLocal`/`ActionDeleteLocal`/`ActionCreateLocalBlank`; selection within the local list (reuse a `LocalSel` index or generalize `Selected` per view — keep it explicit and tested); `SelectedLocalAgent() LocalAgentItem`.
  - term: decode the local-view keys.
  - run.go: `Deps` gains `LocalList func() []LocalAgentItem`, `RunLocal func(name, task string) error`, `CreateLocal func(name, repo, task string) error`, `DeleteLocal func(name string) error`; loop fulfills the actions and refreshes; set a status line from the result (reuse the queue StatusMsg pattern).
  - dash.go: wire those to `internal/localagent` (`Load`, `Run` via `execImpl`, `Add`, `Delete`, persist `LastRun`/`LastTask` after a run).
- [ ] **Step 4:** `go build ./... && GOOS=linux GOARCH=amd64 go build ./... && go vet ./... && go test ./...` → all ok.
- [ ] **Step 5:** Commit: `feat(tui): run/create-blank/delete local agents in the dashboard`.

---

## Task 6: Wire view data + create-from-dir-browser

**Files:** Modify `cmd/rbg/dash.go`, `internal/tui/run.go`, tests.

- [ ] **Step 1:** On dashboard load and on refresh (`r`), populate `SetLocalAgents(d.LocalList())` alongside the remote `Fetch`, so all views have data. Test the loop wiring via the existing run-loop test style (synthetic Deps).
- [ ] **Step 2:** Extend the `n` create-flow so that in `ViewLocal` it pins the browsed dir as the repo and (empty task allowed) creates a blank local agent via `Deps.CreateLocal`. Reuse the dir browser already built.
- [ ] **Step 3:** `go test ./...` → ok.
- [ ] **Step 4:** Commit: `feat(tui): populate views + create local agents from the dir browser`.

---

## Task 7: Integration + manual smoke

**Files:** Modify `test/integration_v2/test_integration_v2.py`.

- [ ] **Step 1:** Add a test that builds `rbg`, then exercises the local CLI group with a local temp git repo (no network): `rbg raw local add <name> <tmprepo> "<task>"`, `rbg raw local ls` (shows it), `rbg raw local show`, `rbg raw local rm`. Assert exit codes + output. (Local agents need no desktop, so this needs no sshd — but keep it under `-m integration` with the build step, or add a fast `cmd/rbg` unit test if simpler.)
- [ ] **Step 2:** `pytest -m integration -q` → all pass.
- [ ] **Step 3: Manual smoke (needs a real TTY + claude on the laptop):**
  - `rbg raw local add demo ~/workplace/mymemories ""` (blank, pinned)
  - `rbg` → Tab to Local view → see `demo` → run it with a task → confirm it pulls then runs claude in the repo.
  - `rbg raw local run demo "summarize HANDOFF.md"` → confirm sync-then-run, `lastRun` updates.
- [ ] **Step 4:** Commit: `test: local-agent CLI integration`.

---

## Self-Review

**Spec coverage:**
- Blank local agents pinned to a repo → Task 1 (store allows empty Task) + Task 5/6 (create-blank). ✓
- Manual run later, sync-first → Task 1 (`PlanRun` pulls before claude) + Task 2/5 (run verb/action). ✓ No scheduling (non-goal). ✓
- Local/remote/combined/project views → Task 3 (enum) + Task 4 (render). ✓
- Agent-usable (TTY-free) → Task 2 (`raw local` verbs). ✓
- Ergonomic principles (per-view actions, status visibility, uniform create) → Tasks 4/5/6. ✓

**Placeholder scan:** none — every task has concrete code or precise instructions referencing real symbols.

**Type consistency:** `localagent.Agent{Name,Repo,Task,LastRun,LastTask}` (Task 1) ↔ `tui.LocalAgentItem{Name,Repo,Task,LastRun}` (Task 3), converted in dash.go (Task 5/6). `localagent.Run(Exec, Agent, task)` (Task 1) called by `execImpl` in dash.go (Task 2/5). `ViewMode`/`View` consistent Tasks 3→6. Deps additions (LocalList/RunLocal/CreateLocal/DeleteLocal) defined in run.go (Task 5) and supplied in dash.go (Task 5/6).

**Open questions (carry to review, default picks noted):** view-switch key = `Tab` cycle (default); project grouping by repo URL (default); local run logging deferred (default: no log file in v1 — `lastRun` only); queue→local promotion deferred. These don't block the plan; they're refinements.
