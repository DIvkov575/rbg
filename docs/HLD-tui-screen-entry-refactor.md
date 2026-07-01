# HLD: TUI Refactor — Screen Interface + Unified Entry Model (`tui-refactor`)

> **Superseded by `HLD-rbg-unified-architecture.md`** as the forward plan; kept for detail/history.


**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design

## 1. Overview

### 1.1 Background
The `rbg` dashboard (`internal/tui`) integrates three execution systems:
- **Remote agents** — live desktop `claude` sessions (`session.Session`).
- **Local agents** — persistent, repo-pinned, manually re-run (`localagent.Agent`).
- **Queue** — one-shot staged dispatch (`queue.Item`).

The pure-model pattern (`Update`→Action, `View`→string, loop does I/O) is sound.
The *composition* of screens is not.

### 1.2 Problem Statement
- **Screens are ~10 boolean flags on one `Model`** (`Input`, `Browsing`,
  `MakingDir`, `ConfigOpen`, `ConfigEditing`, `QueueOpen`, `QueueAdding`,
  `Previewing`, …), mutually exclusive only by convention — nothing structural
  stops two being true at once.
- **`Update` and `View` are each a hand-ordered `if m.XOpen {…}` chain**; a new
  screen edits both, plus adds a bespoke decoder. Verified: **5 parallel
  `decodeKey*` functions** already exist (`decodeKey`, `decodeKeyInput`,
  `decodeKeyBrowse`, `decodeKeyConfig`, `decodeKeyQueue`).
- **The three systems are three parallel data pipelines** with no shared row
  type, so "show the remote agent and the local agent for the same repo
  together" has no home.

Adding the requested local / combined / project views on this structure means
flags #11–#13 in three places each.

### 1.3 Goals
- Make **"screen" a type**, not scattered booleans — mode becomes structural.
- Make the **three systems one data population** — the four views are
  filters/groupings over it.
- Preserve invariants: **pure model**, **stdlib-only**, **TTY-free unit tests**.
- Migrate **incrementally** — existing screens keep passing tests throughout.

## 2. Requirements

### 2.1 Functional
- A `Screen` type owns its own `Update`, `View`, and key hints; `Model` tracks
  the active screen (a stack, so `esc` pops).
- A unified `Entry` value represents any agent row; `Kind ∈ {Remote, Local,
  Queued}`. A pure `buildEntries` assembles `[]Entry` from the three sources.
- Four views as pure selectors over `[]Entry`: Remote (`Kind==Remote`), Local
  (`Kind==Local`), Combined (all, sectioned), Project (all, grouped by `Repo`).
- Per-view actions stay kind-appropriate (remote: attach/kill/read; local:
  run/edit/delete; queued: dispatch/remove), surfaced via the screen's hints.
- **ctrl-s** (byte `0x13`) cycles views. Verified reachable: `rawMode` clears
  `IXON` in both `term_darwin.go` and `term_linux.go`, so `0x13` arrives as a
  keystroke rather than terminal XOFF.

### 2.2 Out of Scope
- No third-party deps (no Bubble Tea); the raw-terminal render/IO layer stays.
- No user-visible behavior change to existing screens — this is structural.
- No scheduling, no remote↔local process migration.

### Verified facts (observed this session)
- `session.Session{Name, ClaudeSessionID, TranscriptPath, PID, StartedAt, Dir}`.
- `localagent.Agent{Name, Repo, Task, LastRun, LastTask}` — package built,
  tests green.
- `queue.Item{Prompt, Repo}`.
- `internal/tui` has tui-local mirrors already (`QueueItem`, `DirItem`,
  `ConfigField`) — the `Entry`/`LocalAgentItem` types follow that same
  "mirror in tui, convert in dash.go" precedent, keeping the model free of the
  `session`/`queue`/`localagent` packages.
- `cmd/rbg/dash.go` has `dispatchLocal` + `localRepoDir` (fire-and-forget local
  run) alongside the newer `localagent.Run` (sync-first, tracked) — two local
  paths (see §5.4).

## 3. Solution Options

**Screen composition —**
**Option A — more flags.** Keep the boolean-per-screen model; add three more for
the new views. Zero refactor, but compounds the exact problem (§1.2) and makes
the flag-exclusivity hazard worse.
**Option B ★ — `Screen` interface + stack.** Each surface is a small type; the
central `Update`/`View` chains collapse to `top.Update()`/`top.View()`;
exclusivity is structural (you're on one screen); `esc`=pop is free. Idiomatic
Elm/Bubbletea shape, hand-rolled in stdlib. Larger upfront change, mitigated by
incremental migration (§5.5).

**Systems integration —**
**Option A — keep three pipelines**, special-case a "project" join. Works, but
the join logic is bespoke and the queue stays an odd sibling.
**Option B ★ — one `Entry{Kind}` population.** Views become pure
filters/groupings; project-grouping-by-repo makes remote↔local handoff fall out
of the data; the queue dissolves into `Kind==Queued`. One row type to test.

## 4. Current State
```
Model{ Input,Browsing,MakingDir,ConfigOpen,ConfigEditing,QueueOpen,QueueAdding,Previewing,... }
                              │
      Update(m,k):  if QueueOpen{if Previewing…if Adding…} if ConfigOpen{…} if Browsing{…} if Input{…}
      View(m):      if QueueOpen{…} if ConfigOpen{…} if Browsing{…} ...           (chain repeated)
      decode:       decodeKey / decodeKeyInput / decodeKeyBrowse / decodeKeyConfig / decodeKeyQueue

 data:  Fetch()→[]session.Session   LoadQueue()→[]queue.Item   (local: untracked, not in the list)
        └── three separate render paths; no shared row; no place for "same repo, both kinds"
```
Each feature this session (queue, config, preview, dir-browser) added a flag +
two branches + a decoder. The next three views would triple that.

## 5. Design Proposal

### 5.1 Architecture
```
 sources (I/O in the loop, via Deps)         pure model
 ───────────────────────────────────        ─────────────────────────────────
 Deps.Fetch()     → []session.Session ┐
 Deps.LocalList() → []LocalAgentItem  ├─ buildEntries() → []Entry ─→ Screen.View
 Deps.LoadQueue() → []QueueItem       ┘        (pure)      (filter/group by view)
                                                                 │ Key
 loop fulfills Action ◄──────────────────── Screen.Update ◄──────┘
 (attach/kill/run/dispatch/create/…)         (pure → next Screen, Action)
```

### 5.2 `Screen` interface
```go
type Screen interface {
    Update(m *Model, k Key) (Screen, Action) // next screen: self, a pushed child, or nil=pop
    View(m *Model, w, h int) string
    Hints() string                            // footer key-hints for this screen
}
```
`Model` holds a stack `[]Screen`. `Update` → `top.Update(...)` then apply the
transition (push/replace/pop); `View` → `top.View(...)`. Text-entry sub-modes
(task input, dir-name, config-edit) become small **child screens** pushed on the
stack — replacing today's nested booleans. Each screen decodes its own keys via
a shared helper library, retiring the parallel `decodeKey*` family.

### 5.3 Unified `Entry` model
```go
type Kind int
const ( KindRemote Kind = iota; KindLocal; KindQueued )

type Entry struct {
    Name, Repo, Status string
    Kind   Kind
    remote *session.Session   // set iff KindRemote (via tui mirror)
    local  *LocalAgentItem    // set iff KindLocal
    queued *QueueItem         // set iff KindQueued
}
```
`buildEntries(remote, local, queued) []Entry` is a pure combiner. Views:

| View | Selector over `[]Entry` |
|---|---|
| Remote | `Kind==KindRemote` |
| Local | `Kind==KindLocal` |
| Combined | all, sectioned by `Kind` |
| Project | all, grouped by `Repo` |

**Project view is the integration point:** grouping by `Repo` places the remote
agent working a repo and the local agent pinned to it in one group — so "remote
finishes → run local" is visible in the data, not special-cased. It is also a
**launcher**: selecting a project dispatches a new session (local/remote toggle).

### 5.4 Related: unify the local-run path
`dispatchLocal` (fire-and-forget, no sync, untracked) and `localagent.Run`
(sync-first, tracked) both "run a task locally in a repo." Collapse to one: route
the queue's local dispatch through `localagent.Run`, delete `dispatchLocal`. One
code path, one behavior. This is the genuine *refactor* in the request; lands in
the same slice as the seam.

### 5.5 Migration (incremental; tests green each step)
1. **Add the seam, convert nothing** — `Screen`, `Entry`, `buildEntries` as new
   tested types. No existing screen changes. (Proves the shape, as the
   `localagent` core was proven before wiring.)
2. **Build the new views natively** as `Screen`s on `Entry` (local / combined /
   project). No legacy to preserve.
3. **Convert legacy screens as touched** — queue, config, dir-browser, preview
   become `Screen`s, one per commit, because they now read from `entries`.
4. **Delete the flag chain** in a final cleanup commit once all are converted.

Maps to the two slices: **Slice 1** = seam + `Entry` + unify local-run (§5.4) +
ctrl-s + `raw local` verbs; **Slice 2** = the four views + legacy conversion.

### 5.6 Data-model compatibility
No on-disk format changes. `Entry` and `LocalAgentItem` are in-memory tui types
built from existing stores (`~/.rbg/local-agents.json`, the queue store, remote
`ls`). Backward-compatible by construction.

## 6. Design Analysis

### 6.1 Key Improvements
- New screen = one type, not three edits + a decoder.
- Screen exclusivity is structural; `esc`=pop is free.
- One row type → four views are pure one-liners; project view makes the
  remote↔local handoff legible; the queue stops being a special case.
- One local-run path instead of two divergent ones.

### 6.2 Risks
| Risk | Mitigation |
|---|---|
| Refactor regresses working screens | Incremental (§5.5), one screen/commit, tests green each step; delete flags only after all converted |
| `Screen` stack over-built for trivial screens | Screens are tiny structs; 3-method interface; child-screen sub-modes are simpler than nested booleans |
| `Entry` typed pointers leak kind-logic into views | Views branch only on `Kind` + call accessors; kind-specific rendering isolated in small helpers |
| Doing refactor + features at once | Two slices; Slice 1 (seam + unify) is mergeable on its own before any view work |

### 6.3 Decisions captured
- **ctrl-s** cycles remote → local → combined → project (user).
- **Project view = launcher + lens** (user).
- **Two-slice delivery**: seam/foundation/unify first, views second (user).
