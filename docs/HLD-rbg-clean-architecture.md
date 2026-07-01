# HLD: rbg Clean Architecture — One Agent Model, Orthogonal Axes (`rbg-clean`)

**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design (greenfield)
**Framing:** A clean-slate design of rbg's client/model/UI layer, unconstrained by
the current implementation. Not a migration of today's code — the target shape as
if built fresh. Supersedes all prior TUI/agent design HLDs as the forward plan.
**Kept as-is (proven, shipped, out of scope for the rewrite):** the SSH transport
(`sshx`, mux), the desktop `rbg-agent` binary, the `claude` contract
(`claudecli`), config loading (`config`). These are the verified plumbing; the
rewrite is everything that *models and presents* agents.

## 1. The one idea

Today rbg has three notions — remote agents, local agents, queue items — plus a
bolt-on "foreign" flag, each with its own type, store, and code path. The clean
insight: **these are not different things. They are one thing — an Agent —
described by orthogonal attributes.** Collapse the taxonomy into axes:

```
Agent {
    Name    string          // stable handle
    Repo    string          // git URL / identity  (grouping key)
    Dir     string          // working directory on its host
    Task    string          // the prompt (empty = blank/draft)
    Session string          // claude sessionId once run ("" = never run)

    Where   Location        // Local | Remote        — which machine
    State   Lifecycle       // Draft | Running | Done — where in its life
    Origin  Origin          // Native | Foreign       — did rbg create it
    Sync    SyncState        // derived: repo git state
    RunAt   string          // last run time
}
```

Everything the session asked for is now a *value on one struct*, not a new type:
- **Queue** = `State==Draft` agents (created, not yet run). Not a separate store.
- **Local vs remote agents** = `Where`. Same struct, same list, same actions.
- **Blank agent pinned to a repo** = `State==Draft, Task==""`.
- **Foreign agent** = `Origin==Foreign` (discovered from `claude agents --json`,
  not in rbg's records). Adopt = flip to `Native`.
- **Sync state / cross-device** = `Sync` + a transcript-copy action.

One noun, one store, one list. The four views are pure *lenses* over `[]Agent`.

## 2. Goals
- **One domain type (`Agent`), one repository (`Store`)** — no `Kind` union, no
  parallel stores, no typed-pointer soup.
- **Orthogonal axes** (Where / State / Origin) instead of a flat enum, so new
  combinations don't need new types.
- **Screen = an interface; navigation = a stack.** No boolean mode-flags.
- Invariants: pure model (no I/O in Update/View), stdlib-only, TTY-free tests,
  single static binary, no desktop daemon.
- Because it's greenfield: optimize for the *end shape*, not migration. (Delivery
  is still sliced — §6 — but the design isn't shaped by "don't break today.")

## 3. Verified facts (the plumbing the clean layer sits on)
- `claude agents --json --all` → every background session on a host
  (`{id,cwd,sessionId,name,state,startedAt}`), regardless of spawner — the single
  source for both native and foreign agents. (Live desktop.)
- Transcripts: `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl`, identical layout
  both devices → transcript sync is a whole-file scp over the SSH mux.
- Git sync: `git rev-list --left-right --count @{u}...HEAD`, `git status
  --porcelain`, `git rev-parse @{u}`.
- Headless run: `claude -p <task> --session-id <uuid> --dangerously-skip-permissions`
  (launch), `--resume <uuid>` (continue). Proven against the live host.
- SSH mux (`~/.rbg/cm-*`), `rbg-agent` verbs, and `sshx` quoting all shipped.

## 4. Architecture

### 4.1 Layers (clean separation)
```
 cmd/rbg            entrypoint: raw CLI verbs + launch the dashboard
   │
 ui/  (pure)        Screen interface + screen stack; lenses over []Agent
   │  Update(Model,Key)->(Model,Action)   View(Model)->string     (no I/O)
 core/ (pure)       Agent, Store (in-mem ops), lenses (filter/group/sync-badge)
   │
 host/ (I/O)        one interface per capability, impls behind it:
                      AgentSource  (list agents on a host: local + remote)
                      Runner       (spawn/resume claude; local exec | ssh)
                      Repo         (clone/pull/sync-state; git)
                      Transcripts  (read/copy .jsonl across devices)
   │
 [kept] sshx · claudecli · rbg-agent · config
```
The dashboard loop wires `host/` impls into the pure `ui/`+`core/` via one
`Deps`-style struct of function values (as today), but the *surface* is small
because there's one Agent model, not three.

### 4.2 The Store (one place agents come from)
`Store.Refresh(ctx)` builds `[]Agent` by reconciling three inputs into the one
model:
1. rbg's **persisted records** (the agents you created/ran — `~/.rbg/agents.json`,
   one file replacing today's queue + local-agent stores).
2. **Live local** `claude agents --json` (laptop).
3. **Live remote** `claude agents --json` over SSH (desktop).

Reconciliation by `sessionId`/name sets `Where`, `State` (from claude's live
state), and `Origin` (in records → Native; live-but-not-in-records → Foreign).
Sync state is filled per distinct `Repo`/`Dir` from the `Repo` capability. Result:
one `[]Agent` that *is* reality across both machines.

### 4.3 Views as pure lenses
```
Remote   = filter Where==Remote
Local    = filter Where==Local
Combined = all, sectioned by Where
Project  = all, group by Repo, each group carries a Sync badge   ← keystone
```
`ctrl-s` cycles them. Project view is the launcher+lens: a group is a repo, its
rows are every agent (any Where/Origin) on it, header shows the sync badge, and
you can dispatch a new agent into the group (local or remote). Draft agents
(the old "queue") render inline with a distinct glyph and a "run" action.

### 4.4 Screen + stack (navigation)
```go
type Screen interface {
    Update(m *Model, k Key) (Screen, Action) // self | pushed child | nil=pop
    View(m *Model, w, h int) string
    Hints() string
}
```
`Model{ agents []Agent; view ViewMode; stack []Screen; ... }`. Screens: the four
lenses (or one list-screen parameterized by `view`), plus small pushed children
for text entry (task), dir-browse, config. `esc` pops. One key-decode helper;
each screen interprets keys. No mode booleans anywhere.

### 4.5 Actions (uniform verb set, gated by axes)
One `Action` set the loop fulfills; which are *offered* depends on the selected
agent's axes:
- `Run` (Draft→Running): local = `claude -p` in `Dir`; remote = clone/pull +
  `rbg-agent` launch. **Sync-first** always (pull before run).
- `Send`, `Read`, `Attach`, `Kill` — as today, dispatched by `Where`.
- `SyncTranscript` — scp the agent's `.jsonl` between devices.
- `Adopt` (Foreign→Native), `Create` (new Draft, optionally blank).
- Prompt injection: `Run`/`Send` pass the task through `claudecli.WithHandoff`
  (read-on-launch, write-on-both; `RBG_HANDOFF=0` off).

### 4.6 CLI parity (agent-usable, TTY-free)
Every action has a `raw` verb (`rbg raw run|send|read|kill|create|sync|adopt|ls`)
operating on the same `Store`, so automation/non-interactive agents use rbg
without the dashboard. The dashboard is one consumer of core+host; the CLI is
another.

## 5. Why this is cleaner than the current design
| Current | Clean |
|---|---|
| `Kind{Remote,Local,Queued}` + `Foreign bool` + 3 typed pointers | orthogonal `Where`/`State`/`Origin` on one `Agent` |
| 3 stores (sessions, queue, local-agents) | 1 `Store` (`~/.rbg/agents.json`) reconciled with live `claude agents` |
| 8 boolean mode-flags, 5 decoders, chained Update/View | `Screen` stack, 1 decode helper, `top.Update`/`top.View` |
| queue is a sibling subsystem | queue = `State==Draft` |
| foreign agents invisible / bolt-on | foreign = `Origin==Foreign`, first-class |
| local run fire-and-forget & untracked; 2 local paths | one `Runner`, tracked, sync-first |

New capability = a value on `Agent` or a new `Screen`, never a new type×3-files.

## 6. Delivery (greenfield, but still staged)
1. **`core/`** — `Agent`, `Store` (+ reconcile), lenses, sync-badge. Pure, fully
   tested. No UI yet.
2. **`host/`** — the four capability interfaces + real impls (reuse `sshx`/
   `claudecli`/`rbg-agent`); a fake impl for tests.
3. **`ui/`** — `Screen` + stack + the list-screen with the four lenses; project
   grouping; text/browse child screens.
4. **`cmd/rbg`** — raw verbs on the Store; wire the dashboard.
5. **Transcript sync + foreign adopt + handoff** — small additions on the above.

Old `internal/{queue,localagent,tui}` are replaced, not migrated (this is the
"forget what we have" mandate); `session`/`agent`/`sshx`/`claudecli`/`config`
stay. Ship `core`+`host`+`raw` CLI first (usable headless), dashboard second.

## 7. Non-goals
No scheduling; no third-party deps / no Bubble Tea; no desktop daemon; no
transcript merge (whole-file, newest-wins); laptop↔configured-desktop only; no
live process migration.

## 8. Risks
| Risk | Mitigation |
|---|---|
| Greenfield throws away working TUI code | The *transport* (proven) is kept; only the drifted model/UI is rewritten. Ship the headless `core+host+raw` path first — it's verifiable without a TTY — then the dashboard |
| One `Agent` struct becomes a god-object | Axes are small enums + strings; behavior lives in lenses/actions/host, not on the struct; struct is data only |
| Reconcile mis-classifies native/foreign | Match on `sessionId` (stable), fall back to name+dir; Adopt is explicit and idempotent |
| Live `claude agents` latency per refresh | Refresh on open/`r`, not per keystroke; per-repo sync cached with a short TTL |
