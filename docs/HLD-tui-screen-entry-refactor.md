# HLD: TUI Refactor — Screen Interface + Unified Entry Model

**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design

## 1. Overview

### 1.1 Problem
The dashboard integrates three execution systems — **remote agents** (live
desktop `claude` sessions), **local agents** (persistent, repo-pinned,
manually re-run), and the **queue** (one-shot staged dispatch). Today they are
bolted onto a single `Model` struct as ~10 mutually-exclusive-by-convention
boolean flags (`Input`, `Browsing`, `MakingDir`, `ConfigOpen`, `ConfigEditing`,
`QueueOpen`, `QueueAdding`, `Previewing`, …). `Update` and `View` are each a
hand-ordered precedence chain over those flags:

```
if m.QueueOpen { if m.Previewing {…} if m.QueueAdding {…} … }
if m.ConfigOpen { if m.ConfigEditing {…} … }
if m.Browsing { if m.MakingDir {…} … }
if m.Input { … }
```

Consequences that have made every recent feature harder:
- **Exclusivity is hoped-for, not enforced.** Nothing structurally prevents two
  screen flags being true at once; correctness relies on remembering to unset
  the previous one.
- **A new screen touches three places** — a flag, an `Update` branch, a `View`
  branch — plus a bespoke `decodeKey*` function. Adding the planned local /
  combined / project views this way means flags #11–#13.
- **The three systems are three parallel data pipelines.** Remote sessions,
  local agents, and queue items each have their own fetch/render path, so
  "show the remote agent and the local agent for the same repo together" has no
  natural home.

### 1.2 Goals
- Make **"screen" a first-class type**, so navigation state is structural
  (you are on exactly one screen) rather than a bag of booleans.
- Make the **three systems one data model**, so the four views (remote / local /
  combined / project) are *filters/groupings over a single population*, not
  separate pipelines.
- Preserve the project's invariants: **pure model** (no I/O in `Update`/`View`),
  **stdlib-only**, **fully unit-testable without a TTY**.
- Migrate **incrementally** — existing working screens keep passing tests
  throughout; no big-bang rewrite.

### 1.3 Non-goals
- No new third-party deps (no Bubble Tea — we keep the hand-rolled raw-terminal
  layer; this refactor is about *structure*, not the render/IO substrate).
- No behavior change to existing screens as observed by the user. This is a
  structural refactor; features ride on top of it (separate plan).
- No scheduling, no remote↔local process migration (out of scope here).

## 2. Design

### 2.1 Concern separation
Two tangled concerns are separated:

- **(A) Navigation / mode** → a `Screen` interface + a screen stack.
- **(B) The three systems** → a unified `Entry` value that all views render.

They are independent: (A) fixes "how do modes compose", (B) fixes "how do the
systems integrate". Either is useful alone; together they make the four views
nearly free.

### 2.2 (A) Screen interface

```go
// Screen is one interactive surface (agents list, queue, config, dir-browser,
// preview, …). Pure: Update returns an Action for the loop to fulfill; no I/O.
type Screen interface {
    Update(m *Model, k Key) (Screen, Action) // returns the next screen (self, a
                                             // pushed child, or nil to pop)
    View(m *Model, w, h int) string
    Hints() string                           // footer key-hints for this screen
}
```

`Model` holds a **screen stack** `[]Screen` (top = active). This gives `esc`
= pop for free (preview → back to queue; making-dir → back to browser), which
today is manual flag juggling.

`Update` collapses from the precedence chain to:

```go
func Update(m Model, k Key) (Model, Action) {
    top := m.stack[len(m.stack)-1]
    next, act := top.Update(&m, k)
    m.applyTransition(next) // push / replace / pop
    return m, act
}
```

`View` collapses to `m.top().View(&m, w, h)`. Adding a screen = implement the
interface + push it; **no edits to a central switch**.

Key decoding also moves per-screen: each `Screen` interprets raw keys itself
(via a shared `decode` helper library), removing the parallel `decodeKeyQueue` /
`decodeKeyBrowse` / `decodeKeyConfig` family in favor of the screen owning its
own key semantics. Text-entry sub-modes (task input, dir-name, config-edit)
become small child screens pushed on the stack rather than nested booleans.

### 2.3 (B) Unified Entry model

```go
type Kind int
const ( KindRemote Kind = iota; KindLocal; KindQueued )

// Entry is the unified row every agent-oriented view renders. Kind-specific
// data hangs off the typed pointers; nil for the others.
type Entry struct {
    Name   string
    Repo   string   // grouping key for project view
    Status string   // human status ("running","idle","done","staged","last run 3m")
    Kind   Kind
    remote *session.Session   // when KindRemote
    local  *LocalAgentItem    // when KindLocal
    queued *QueueItem         // when KindQueued
}
```

A single `entries []Entry` is assembled from the three sources (remote fetch,
local-agent list, queue). The four views are pure functions over it:

| View | Selector |
|---|---|
| Remote | `Kind==KindRemote` |
| Local | `Kind==KindLocal` |
| Combined | all, sectioned by `Kind` |
| Project | all, grouped by `Repo` |

**Project view is where integration becomes visible**: grouping by `Repo`
places the remote agent working `mymemories` and the local agent pinned to it in
the same group — the "remote finishes → run local" workflow falls out of the
data model instead of being special-cased. The **queue dissolves** into
`KindQueued` entries: no longer a separate concept, just one more source.

Per-view *actions* remain kind-appropriate (remote: attach/kill/read; local:
run/edit/delete; queued: dispatch/remove) — the view knows an entry's `Kind` and
offers the right verbs, surfaced via `Hints()`.

### 2.4 Data flow

```
 sources (I/O, in the loop via Deps)        pure model
 ─────────────────────────────────         ──────────────────────────
 Deps.Fetch()      → []session.Session ┐
 Deps.LocalList()  → []LocalAgentItem  ├─→ buildEntries() → []Entry ─→ Screen.View
 Deps.LoadQueue()  → []QueueItem       ┘         (pure)               (filter/group)
                                                                        │ Key
 loop fulfills Action ←──────────────────── Screen.Update ←────────────┘
 (attach/kill/run/dispatch/…)                (pure, returns Action)
```

The loop (`run.go`) still owns all I/O and fulfills `Action`s; the model stays
pure. `buildEntries` is a pure combiner — trivially unit-tested.

## 3. Migration strategy (incremental, tests green throughout)

Big-bang rewrites of working TUI code are the risk. Instead:

1. **Introduce the seam, don't convert yet.** Add `Screen`/`Entry`/`buildEntries`
   as new types with their own tests. No existing screen changes. (Proves the
   shape, like the `localagent` core was proven before wiring.)
2. **Build the NEW views natively as `Screen`s** on the `Entry` model
   (local / combined / project). These have no legacy to preserve.
3. **Convert existing screens opportunistically** — queue, config, dir-browser,
   preview — into `Screen`s *as we touch them* for the unified data, one per
   commit, each keeping its tests green. Not a separate rewrite; it happens
   because they now read from `entries`.
4. **Delete the flag chain** once the last screen is converted; the boolean
   fields and the `Update`/`View` precedence chains go away in a final cleanup
   commit.

This maps onto the two-slice plan: **Slice 1** introduces the seam + `Entry` +
the foundation (and unifies the local-run path — see §5); **Slice 2** builds the
views as `Screen`s and converts the legacy screens.

## 4. Testing
- `buildEntries`, per-view selectors/grouping: pure table tests over synthetic
  sources — no TTY, no SSH.
- Each `Screen`'s `Update`: feed keys, assert (next-screen, Action) — the pure
  discipline already in place.
- Stack transitions (push/replace/pop, `esc` pops): unit-tested on `Model`.
- The raw-terminal loop stays integration/manual as today.

## 5. Related refactor: unify the local-run path
Orthogonal but landing in the same slice: today `cmd/rbg/dash.go`'s
`dispatchLocal` (fire-and-forget, no sync, untracked) and the new
`localagent.Run` (sync-first, tracked) are two ways to "run locally". Collapse
to one — route the queue's local dispatch through `localagent.Run` and delete
`dispatchLocal` — so there is a single local-run code path. (Detailed in the
local-agents plan.)

## 6. Risks
| Risk | Mitigation |
|---|---|
| Refactor regresses working screens | Incremental conversion, one screen/commit, tests green each step; delete flags only after all converted |
| `Screen` stack over-engineered for simple screens | Screens are tiny structs; the interface is 3 methods; text sub-modes as child screens are simpler than the current nested booleans |
| `Entry`'s typed pointers leak kind-logic into views | Views branch only on `Kind` + call accessors; keep kind-specific rendering in small per-kind helpers |
| Scope creep (refactor + features at once) | Two slices: seam+foundation first (mergeable alone), views second |

## 7. Decisions captured
- **View cycling:** `ctrl-s` (byte `0x13`, reaches the app because `rawMode`
  clears `IXON`) cycles remote → local → combined → project. (User decision.)
- **Project view = launcher-and-lens:** groups existing agents by repo AND lets
  you select a project to dispatch a new session (local/remote). (User decision.)
- **Delivery:** two slices — foundation/seam/unify first, views second.
  (User decision.)
