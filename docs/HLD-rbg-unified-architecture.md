# HLD: rbg Unified Architecture — Agents, Views, Sync (`rbg-unified`)

**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design
**Consolidates & supersedes (as the forward plan):**
`HLD-tui-screen-entry-refactor.md`, `HLD-project-view-sync-discovery.md`,
`specs/2026-06-30-local-agents-and-views-design.md`. Historical/built context:
`HLD-rbg-as-built.md` (v2, shipped), `HLD-rbg-v2-agent-binary.md`,
`HLD-remote-agent-management.md`.

> **Why this doc exists.** The design drifted across a long session into 5 HLDs,
> 4 plans, 2 specs — several overlapping. The user asked to *embrace the scope*
> ("be okay with bloating this; this is a massive change") and get one coherent
> account of how everything integrates idiomatically. This is that account. It is
> deliberately large; it is the single source of truth going forward.

---

## 1. Overview

### 1.1 What rbg is today (shipped, `main`)
A laptop CLI + a desktop agent binary (`rbg-agent`) over SSH. Verified working:
launch/send/read/ls/attach/kill/ping/deploy, `raw <verb>` cleanup, SSH mux
(hot connection), a raw-terminal dashboard TUI, a client-only queue with
local/remote dispatch + preview, an in-dash config screen, and
`--dangerously-skip-permissions` headless runs. Local dispatch runs the laptop's
`claude` in a repo checkout.

### 1.2 The accumulated asks (this session's drift, now unified)
1. **Local agents as first-class, persistent, blank-able, repo-pinned** entities,
   run manually later with **sync-first** (pull before run). Core package built
   & tested (`internal/localagent`).
2. **Views** over the agent population: remote, local, combined, and a
   **dir/project-grouped** view.
3. **Sync-state badges** — show whether each repo is pushed/clean.
4. **Cross-device transcript sync** (`*.jsonl`) over SSH.
5. **Foreign-agent discovery** — surface `claude` sessions not spawned by rbg.
6. **Handoff prompt injection** (read HANDOFF on launch, write on finish).
7. A **structural refactor** so the above stop compounding complexity.

### 1.3 The core problem (why a *unified* design, not more patches)
The TUI has become an additive-only structure. **Verified counts today:**
14 `Deps` methods, 8 boolean mode-flags on one `Model`, 5 parallel `decodeKey*`
functions, 15 `Action` constants, 21 `Key` constants — each new surface adds to
all five, in multiple files. And the three execution systems (remote / local /
queue) are three parallel data pipelines with no shared row type, so
"remote + local for the same repo, together" has nowhere to live. Layering asks
1–6 onto this multiplies the mess. The refactor (ask 7) is the *enabling*
substrate for everything else.

### 1.4 Goals
- **One agent population, one screen abstraction.** Every feature above becomes a
  small addition to two clean seams, not a new flag×3-files.
- Preserve invariants: **pure model** (no I/O in Update/View), **stdlib-only**
  (no third-party deps — the Go proxy is unreachable here), **TTY-free tests**,
  single static binary, no desktop daemon.
- **Incremental, tests-green migration** — no big-bang rewrite of working code.
- Embrace scope: this is a platform for managing many agents across two devices,
  not a launcher. Bloat is acceptable where it buys coherence.

## 2. The two integrating abstractions

Everything reduces to two seams. Get these right and asks 1–6 are small.

### 2.1 `Entry` — one row type for the whole population
```go
type Kind int
const ( KindRemote Kind = iota; KindLocal; KindQueued )

type Entry struct {
    Name, Repo, Dir, Status string
    Kind    Kind
    Foreign bool            // KindRemote not in rbg's store (ask 5)
    Sync    SyncState       // repo git state for its Dir/Repo (ask 3)
    // typed detail (nil for other kinds):
    remote *SessionItem     // KindRemote/Foreign
    local  *LocalAgentItem  // KindLocal
    queued *QueueItem       // KindQueued
}
```
`buildEntries(remote, local, queued, sync)` — a **pure** combiner assembling the
population from the three sources + a sync-state map. Every view is a pure
filter/group over `[]Entry`. This single type absorbs asks 1, 3, 5.

### 2.2 `Screen` — one interface for every surface
```go
type Screen interface {
    Update(m *Model, k Key) (Screen, Action) // next: self, pushed child, or nil=pop
    View(m *Model, w, h int) string
    Hints() string
}
```
`Model` holds a **screen stack** `[]Screen`. `Update`→`top.Update`; `View`→
`top.View`; push/replace/pop applied by the loop; `esc`=pop is free. Text-entry
sub-modes (task input, dir-name, config-edit, queue-add) become **child screens**,
not nested booleans. Each screen decodes its own keys via one shared helper —
retiring the 5 `decodeKey*` functions. This absorbs ask 2 and dissolves the
flag/decoder/Action sprawl.

**The payoff:** the 8 flags → a stack; 5 decoders → 1 helper + per-screen
intent; 15 Actions stay but are owned by their screen; a *new* view/screen =
one small type, no edits to a central switch.

## 3. How each ask maps onto the two seams

| Ask | Integration (no new subsystem) |
|---|---|
| 1. Local agents | `internal/localagent` (built) → `KindLocal` entries; a `LocalScreen`; `raw local` verbs |
| 2. Views | Screens that filter/group `[]Entry`: Remote / Local / Combined / **Project** (group by `Dir`/repo). `ctrl-s` cycles them |
| 3. Sync badges | `SyncState` on `Entry`, computed in the Deps data-build (git primitives), rendered by the project/group screen |
| 4. Transcript sync | A `SyncTranscript` Dep (scp over the SSH mux) + a key/`raw sync` verb; whole-file, newest-mtime-wins |
| 5. Foreign discovery | Reconcile `claude agents --json --all` vs rbg's store → `Foreign=true` entries; `adopt` writes them into the store |
| 6. Handoff injection | One string transform in `internal/claudecli` (launch adds read+write, send adds write); config-gated |
| 7. Refactor | §2 — the substrate the rest ride on |

**The unified Project view is the keystone** (asks 2+3+4+5 converge): agents of
all kinds grouped by directory, each group showing a **sync badge**, each row a
**kind glyph** (● remote / ○ local / ◆ foreign), with per-row actions
(attach/read/kill vs run/edit/delete) and group actions (sync-transcripts, adopt,
launch-here). It answers "what's happening on `mymemories`, across both machines,
and is it pushed?" in one place.

## 4. Verified facts (observed this session)
- Transcript layout identical on laptop & desktop:
  `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl`.
- `claude agents --json --all` lists **all** background sessions
  (`{id,cwd,sessionId,name,state,startedAt,kind}`) regardless of who spawned
  them — the foreign-discovery primitive. (Live desktop.)
- Git sync primitives: `git rev-list --left-right --count @{u}...HEAD` (ahead/
  behind), `git status --porcelain` (dirty), `git rev-parse @{u}` (upstream).
- `session.Session{Name,ClaudeSessionID,TranscriptPath,PID,StartedAt,Dir}`;
  `localagent.Agent{Name,Repo,Task,LastRun,LastTask}` (built, tests green);
  `queue.Item{Prompt,Repo}`. tui already mirrors external types locally
  (`QueueItem`,`DirItem`,`ConfigField`) — `Entry`/`SessionItem`/`LocalAgentItem`
  follow that precedent (model stays free of the source packages).
- SSH mux live (`~/.rbg/cm-*`); `--dangerously-skip-permissions` accepted;
  `dispatchLocal` + `localagent.Run` both exist (two local paths → unify).
- Current TUI drift counts (the motivation): 14 Deps / 8 flags / 5 decoders /
  15 Actions / 21 Keys.

## 5. Current State
```
 Model{ 8 bool flags } ── Update: if QueueOpen{…}if ConfigOpen{…}if Browsing{…}if Input{…}
                          View:   same chain repeated
                          decode: 5 × decodeKey*
 3 pipelines: Fetch()→sessions   LoadQueue()→items   local: untracked/fire-and-forget
 no shared row · no dir grouping · no sync state · transcripts don't cross devices ·
 foreign agents invisible · handoff not injected
```

## 6. Design Proposal

### 6.1 Architecture
```
 Deps (all I/O, in the loop)                     pure model
 ─────────────────────────────────────          ────────────────────────────────
 claude agents --json --all (ssh) ─┐ reconcile
 rbg remote store                  ├──────────→ []Entry ─ buildGroups ─→ Screen.View
 localagent.List / queue.Load      ┘  (+Foreign)   ▲         (filter/group)
 git status per dir (ssh|local) ───→ SyncState ────┘              │ Key
 SyncTranscript / Launch / Run / Kill / Adopt ◄── Action ◄── Screen.Update
 claudecli.WithHandoff (launch/send prompt) 
```
Model purity preserved: git, ssh, scp, claude all run in the loop via `Deps`;
`buildEntries`/`buildGroups`/screens are pure.

### 6.2 Package plan
- **new `internal/tui/screen.go`** — `Screen` interface, stack ops on `Model`.
- **new `internal/tui/entry.go`** — `Entry`, `Kind`, `SyncState`, `DirGroup`,
  `buildEntries`, `buildGroups`. Pure, table-tested.
- **`internal/tui/*_screen.go`** — one file per screen (agents/remote, local,
  combined, project, queue, config, browser, text-input). Migrated incrementally.
- **`internal/localagent`** (built) — persistent local agents; sync-then-run.
- **`internal/claudecli`** — add `WithHandoff` (ask 6).
- **`cmd/rbg`** — `raw local|sync|discover|adopt` verbs; `dash.go` supplies the
  fatter `Deps`; delete `dispatchLocal` (unify onto `localagent.Run`).
- Unchanged: `sshx`, `config`, `run`, `render`, `agent`, `session`, `queue`.

### 6.3 Migration (incremental; each step ships green)
**Slice A — substrate (no user-visible change):** introduce `Screen`+`Entry`+
`buildEntries` as tested types; convert the *existing* screens (remote list,
queue, config, browser, preview) to `Screen`s one commit at a time; delete the 8
flags + 5 decoders in a final cleanup. Also: unify the local-run path; add
`raw local` + handoff injection (both self-contained).
**Slice B — the new value on the clean base:** Local screen; **Project screen**
(dir grouping); sync-state badges; foreign discovery + adopt; transcript sync;
`ctrl-s` view cycling. Each independently shippable.

### 6.4 Decisions captured (user)
- `ctrl-s` cycles remote → local → combined → project (reaches app: `rawMode`
  clears `IXON`).
- Project view = **launcher + lens** (group existing agents by repo AND dispatch
  new sessions into a selected project).
- Transcript sync = **rbg pulls over the existing SSH** (whole-file, newest-wins).
- Handoff: on by default, `RBG_HANDOFF=0` off; read-on-launch, write-on-both;
  `HANDOFF.md` in the repo (agent does the I/O).
- Two-slice delivery; substrate first.

## 7. Analysis

### 7.1 Key improvements
- New screen/view/kind = a small type on a stable seam, not flag×3-files×decoder.
- One `Entry` population makes dir-grouping, sync badges, foreign agents, and the
  remote↔local handoff *fall out of the data model* instead of being special-cased.
- The queue stops being a sibling concept — it's `KindQueued` entries.
- One local-run path; one prompt-injection point; one key-decode helper.

### 7.2 Risks
| Risk | Mitigation |
|---|---|
| Big refactor regresses working screens | Slice A converts one screen/commit, tests green each; flags deleted only after all converted |
| Scope is genuinely large | Two slices; Slice A is mergeable and valuable alone (substrate + unify + handoff + raw local) before any new view |
| Git/ssh per group adds latency | Compute sync in the Deps data-build with a short TTL cache; read-only; visible groups only |
| Transcript copy clobbers newer file | Whole-file, newest-mtime-wins, append-only logs — never merge |
| `Entry` typed pointers leak kind-logic into views | Views branch only on `Kind`; kind-specific rendering in small helpers |
| Doc drift recurs | This doc is the single source of truth; the superseded HLDs stay as history, not forward plan |

### 7.3 Non-goals
No scheduling; no third-party deps / no Bubble Tea; no desktop daemon; no
transcript merging; no local↔local (only laptop↔the configured desktop);
no remote↔local live process migration.
