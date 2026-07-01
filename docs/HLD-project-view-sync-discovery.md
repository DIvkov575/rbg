# HLD: Project View, Transcript Sync & Foreign-Agent Discovery (`project-sync`)

> **Superseded by `HLD-rbg-unified-architecture.md`** as the forward plan; kept for detail/history.


**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design
**Builds on:** `docs/HLD-tui-screen-entry-refactor.md` (Screen interface + unified `Entry` model).

## 1. Overview

### 1.1 Background
rbg tracks **remote agents** (desktop `claude` sessions), **local agents**
(laptop, repo-pinned, persistent), and a **queue**. The companion refactor HLD
unifies these into one `Entry{Kind}` population rendered by pluggable `Screen`s.
This HLD specifies four capabilities that ride on that model.

### 1.2 Problem Statement
- Agents are addressed by name; there is **no repo/dir-oriented view**.
- No single view shows **remote and local agents together grouped by their
  directory**, nor whether each agent's **repo is synchronized**.
- Conversation **transcripts (`*.jsonl`) don't move between devices** — a
  session run on the desktop can't be read on the laptop.
- Agents spawned **outside rbg** (a hand-run `claude --bg`) are invisible to rbg.

### 1.3 Goals
- A **repo/dir-wrapped view**: agents grouped by their associated directory.
- A **unified grouped view** of remote+local agents by directory, each row
  showing its **kind** and its **repo sync state**.
- **On-demand transcript sync** laptop↔desktop over the existing SSH.
- **Automatic discovery** of foreign agents (not spawned by rbg), folded into
  the same views.

## 2. Requirements

### 2.1 Functional
- **Dir grouping.** Group `[]Entry` by a normalized directory key. Remote agents
  use `session.Dir` / the `cwd` from `claude agents --json`; local agents use
  their pinned repo's resolved dir; queued items use their repo.
- **Unified grouped (project) view.** One screen listing every agent under its
  dir-group header. Each row shows: **kind** (remote ● / local ○ / foreign ◆),
  agent name/status, and a **sync badge** for the group's repo.
- **Sync badge** per repo/dir, computed from git:
  - `synced` — upstream exists, ahead=0 behind=0, clean tree.
  - `ahead N` / `behind N` / `ahead N behind M` — from
    `git rev-list --left-right --count @{u}...HEAD` (**verified primitive**).
  - `dirty` — uncommitted changes (`git status --porcelain` non-empty).
  - `no-upstream` — `@{u}` fails.
  - `unknown` — not a git dir / unreachable.
- **Transcript sync.** For a selected agent (or all in a group), copy its
  `~/.claude/projects/<slug>/<sessionId>.jsonl` between devices over the live SSH
  mux. On-demand (a key / `raw` verb), newest-wins by mtime, never destructive
  merge (transcripts are append-only logs). Makes desktop-run sessions readable
  on the laptop and vice-versa.
- **Foreign-agent discovery.** Reconcile `claude agents --json --all` against
  rbg's own remote store: any session present in claude's list but absent from
  rbg's records is surfaced as a **foreign** `Entry` (attach/read/kill still
  work; it just wasn't rbg-launched). Adoptable into rbg's store.

### 2.2 Out of Scope
- Continuous/background sync or scheduling — all sync is user-invoked.
- Merging/editing transcripts — copy whole files only.
- Third-party sync services or new deps — SSH + git + stdlib only.
- Cross-device sync of *local↔local* laptops (only laptop↔the configured desktop).

### Verified facts (observed this session)
- Transcript layout identical on both devices:
  `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl` (laptop dirs incl.
  `-Users-divkov-workplace`; desktop incl. `-local-home-divkov-biostat`).
- `claude agents --json --all` returns **all** background sessions with
  `{id, cwd, sessionId, name, state, startedAt, kind}` regardless of who spawned
  them — the discovery primitive. (Verified against the live desktop.)
- Git sync primitives work: `git rev-list --left-right --count @{u}...HEAD`
  (ahead/behind), `git status --porcelain` (dirty), `git rev-parse @{u}`
  (upstream presence).
- `session.Session` has `Dir`; `localagent.Agent` has `Repo`; both feed the
  dir/repo grouping key.

## 3. Solution Options

**Sync-state computation — where does git run?**
**Option A — compute per repo on demand, cache in the Entry.** When a view
needs a group's badge, run the git primitives once (remote dirs over SSH, local
dirs locally), stash the result on the group. Simple, correct, a little latency
on first render.
**Option B ★ — compute in the data-build step (`buildEntries`/group), with a
short TTL cache.** Same primitives, but computed while assembling groups and
memoized for a few seconds so view-switching/redraw is instant. Chosen: keeps
the pure model pure (git runs in the loop/Deps, results passed in), avoids
per-keystroke git.

**Foreign-agent representation —**
**Option A — a separate "foreign" list.** Extra plumbing, splits the population.
**Option B ★ — a `Kind`/flag on `Entry`.** Reuse the unified model: foreign =
`KindRemote` with a `Foreign bool` (or a dedicated `KindForeign`). Falls into the
same views/grouping for free; "adopt" just writes it into rbg's store.

## 4. Current State
```
 rbg views: Remote (rbg-tracked sessions) | Local | Queue      ← no dir grouping
 remote store ⊂ claude's actual sessions   ← foreign agents invisible
 transcripts:  desktop ~/.claude/projects/…   laptop ~/.claude/projects/…   ← never copied
 sync state:   not shown anywhere
```
A desktop session you ran by hand doesn't appear in rbg; its transcript can't be
read from the laptop; and nothing tells you whether a repo is pushed.

## 5. Design Proposal

### 5.1 Architecture
```
 Deps (I/O, in the loop)                              pure model (Screen/Entry)
 ─────────────────────────────────────────           ───────────────────────────────
 claude agents --json --all (ssh)  ─┐
 rbg remote store                    ├─ reconcile ─→ []Entry (+Foreign flag)
 localagent.List / queue             ┘                     │
 git status per dir (ssh|local)  ─────→ SyncState map ─────┤
                                                            ▼
                                            buildGroups(entries, sync) → []DirGroup
                                                            ▼
                                              ProjectView.View  (grouped, badged)
 SyncTranscript(agent) ── scp over ssh mux ──→ ~/.claude/projects/<slug>/<id>.jsonl
```

### 5.2 Dir/project grouping (pure)
- `dirKey(Entry) string` — normalize to a comparable directory identity
  (resolve repo→dir, trim trailing slash). Remote and local agents for the same
  project land on the same key (repo-URL basis where dirs differ across devices,
  per the refactor HLD's project-grouping decision).
- `buildGroups([]Entry, map[string]SyncState) []DirGroup` where
  `DirGroup{Dir, Sync SyncState, Entries []Entry}`. Pure, table-tested.

### 5.3 Unified grouped (project) view — the requested view
A `Screen` rendering `[]DirGroup`:
```
▸ mymemories                                   [ahead 2 · dirty]
    ● kv-gap-agent        remote   done
    ◆ hand-run-session    foreign  running
    ○ mymemories-local    local    last run 3m
▸ biostat                                      [synced]
    ● biostat-fix         remote   idle
```
- Group header = dir + **sync badge** (§2.1). Rows = kind glyph + name + status.
- Actions adapt to the selected row's kind (remote/foreign: attach/read/kill;
  local: run/edit/delete) and add group-level: **sync-transcripts**, **adopt
  foreign**. This view *is* the "repo/dir-wrapped view" and the "unified grouped
  view" — they are the same screen (grouping is the wrapper).

### 5.4 Transcript sync (SSH pull, on demand)
- `SyncTranscript(name)` (Deps, runs in loop): resolve the agent's `sessionId`,
  `scp` its `<id>.jsonl` from desktop `~/.claude/projects/*/` to the laptop's
  matching `~/.claude/projects/<slug>/` (and reverse for a local→desktop case),
  over the existing `ControlPath` mux. **Newest-mtime wins; whole-file copy**
  (append-only logs, no merge). Group-level "sync all" iterates members.
- After sync, `read` works on either device — closing the cross-device gap.

### 5.5 Foreign-agent discovery (reconcile)
- In the remote-fetch Deps step: parse `claude agents --json --all`, diff against
  rbg's remote store by `sessionId`. Entries only in claude's list → `Entry` with
  `Foreign=true`. They render (◆), group, attach, read, and sync like any remote
  agent.
- **Adopt** (a key / `raw` verb): write a foreign session into rbg's store
  (name/sessionId/dir), after which it's an ordinary tracked agent.

### 5.6 `raw` verbs (TTY-free, agent-usable)
`rbg raw sync <name>` (pull transcript), `rbg raw discover` (list foreign
agents), `rbg raw adopt <name>` — so automation/agents use these without the TUI.

### 5.7 Data-model / compatibility
- `Entry` gains `Foreign bool`; `DirGroup`/`SyncState` are new pure tui types.
  No on-disk format change; adoption appends to the existing remote store.
- Sync-state git calls are read-only; transcript copy only writes under
  `~/.claude/projects/` (never touches repos).

## 6. Design Analysis

### 6.1 Key Improvements
- One screen answers "what's happening on `mymemories`, across both machines, and
  is it pushed?" — the core workflow.
- Desktop-run transcripts become readable on the laptop (and vice-versa).
- Hand-run `claude` sessions stop being invisible; rbg reflects reality.
- All four capabilities reuse the unified `Entry`/`Screen` model — little net-new
  surface.

### 6.2 Risks
| Risk | Mitigation |
|---|---|
| Git per group adds latency | Compute in data-build with short TTL cache (§3 Opt B); read-only; only for visible groups |
| Transcript copy clobbers a newer local file | Newest-mtime-wins, whole-file, append-only logs; never merge |
| Foreign-agent list churn / stale `done` sessions | Reconcile by sessionId each fetch; show state; adopt is explicit |
| Dir key mismatch across devices (different abs paths) | Group by repo identity, not raw path (refactor HLD decision) |
| Sync mis-resolves the transcript slug | Locate by `sessionId` glob across `projects/*` (existing rbg read strategy), not by reconstructing the slug |

### 6.3 Sequencing
Lands after the Screen+Entry seam (refactor HLD Slice 1). Order: dir-grouping +
project view → sync-state badges → foreign discovery/adopt → transcript sync
(heaviest). Each is independently shippable on the unified model.
