# Design: Local Agents + View Ergonomics

**Date:** 2026-06-30 · **Status:** Draft for review

## Motivation

Today rbg has two execution surfaces that don't compose well:
- **Remote agents** — real, tracked `claude` sessions on the desktop (`ls`/`read`/`send`/`kill`, dashboard). First-class.
- **Queue → local dispatch** — fire-and-forget `claude -p` on the laptop. Untracked: once dispatched it vanishes; no list, no read, no re-run, no status.

The user wants **local agents to be first-class and persistent**: create a *blank* local agent pinned to a repo now, then **manually run it later** — typically after a remote agent has finished and pushed changes, so the local run picks up synced work. This is explicitly *manual invocation*, not scheduling.

The second half is **ergonomics**: as local + remote agents proliferate, the single agents list is insufficient. We need views that slice the agent population usefully (local / remote / combined / by-project).

## Part 1 — Local agents (persistent, manually re-runnable)

### Concept
A **local agent** is a named, persistent work unit pinned to a repo, stored client-side (`~/.rbg/local-agents.json`). It is created once and run on demand, any number of times. It may be **blank** (no task yet) — created as a placeholder pinned to a repo, with the task supplied at run time.

```
local agent = { name, repo, task?, lastRun?, lastTask? }
```

### Lifecycle
1. **Create** — `name` + `repo` (+ optional `task`). Blank = no task.
2. **Run (manual)** — sync the pinned repo (`git pull --ff-only` if cloned, else clone), then run local `claude -p <task>` in it. `task` defaults to the agent's stored task; a per-run task overrides it. Records `lastRun`/`lastTask`.
3. **Edit / delete** — change task or repo; remove.

The **sync-before-run** is the crux of the user's workflow: a remote agent finishes and pushes; later the user runs the local agent, which pulls those changes first, then works on top of them. rbg never schedules — the user invokes.

### Why a separate store from the queue
- Queue items are *staged for one dispatch* then consumed. Local agents *persist and re-run*. Different lifecycle → different store (`internal/localagent`, mirroring `internal/queue`).
- A local agent's run history (`lastRun`) makes "run later after sync" legible — you can see when it last ran and against what task.

### Validated core (already prototyped, tests green)
- `internal/localagent/localagent.go` — `Store` (Load/Add/Get/Delete/List/Save), `Agent` struct. List order: most-recently-run first, never-run last, name asc tie-break.
- `internal/localagent/run.go` — `RepoDir` (repo→local checkout path), `PlanRun` (clone-or-pull then claude, injectable git-check), `Run` (executes via an `Exec` seam; blank+no-task errors). Fully unit-tested without spawning git/claude.

## Part 2 — View ergonomics

### The problem
One flat agents list conflates: remote sessions (desktop, tracked), and now persistent local agents (laptop). They have different state (remote: live transcript; local: last-run history) and different actions. Cramming both into one list is confusing.

### Proposed views (cycle with `Tab`, or `1`/`2`/`3`/`4`)
1. **Remote view** — desktop sessions only (today's agents list). Hover→transcript, attach, kill, launch.
2. **Local view** — persistent local agents only. Shows name · repo · last-run. Run (manual), edit task, delete, create-blank.
3. **Combined view** — both, visually grouped (REMOTE / LOCAL section headers). A single place to see everything in flight + everything stand-by.
4. **Project view** — group ALL agents (local + remote) by their repo/dir. Answers "what's happening on `mymemories`?" — shows the remote agent currently working it AND the local agent pinned to it, together. This is the highest-value view for the user's actual workflow (remote finishes → run the local one on the same repo).

### Ergonomic principles (the "fleshing out")
- **Consistent verbs across views.** Selection + hover is universal; the action set adapts (remote: attach/kill/read; local: run/edit/delete). A status footer always shows what keys do *in the current view*.
- **A view is a lens, not a mode.** Switching views never changes underlying state; it re-slices the same agent population. The pure `Model` gains a `View` enum + a derived ordering; `Update`/`View` already pure → fully testable.
- **Project view is the integration point.** It's where "run local after remote syncs" becomes obvious: the repo's remote agent shows `done`, the repo's local agent sits ready — select it, run it.
- **Local runs need visibility** (today they vanish). A local agent shows `last run: 3m ago` and, optionally, captures run output to `~/.rbg/local-logs/<name>.log` so a `read` works for local agents too — closing the gap with remote `read`.
- **Creation is uniform.** The existing `n`-flow (browse dir → task) extends: a local-agent create reuses the dir browser to pin a repo, and allows an *empty* task (blank agent) — `Enter` on empty task creates blank rather than refusing.

### Surfaces
- **Dashboard (human):** the views above, view-switch key, per-view actions.
- **`raw` verbs (scriptable / agent-usable, TTY-free):** `rbg raw local add <name> <repo> [task]`, `local ls`, `local run <name> [task]`, `local rm <name>`, `local show <name>`. These make local agents usable by automation and by a non-interactive agent (which can't drive the TUI) — important since the user works through agents.

## Non-goals
- **No scheduling.** Runs are always manual. (`lastRun` is a record, not a trigger.)
- **No remote↔local migration** of a running session.
- **No new third-party deps** (stdlib only, per project invariant).

## Open questions for review
1. View switching: `Tab`-cycle vs. number keys vs. both?
2. Project view grouping key: by repo URL, or by resolved local dir? (URL groups remote+local that share an origin even if local path differs.)
3. Local run output capture: always log to `~/.rbg/local-logs/`, or only on request? (Logging enables `read` for local agents but adds files.)
4. Should a queue item be promotable to a persistent local agent (so the queue stays "one-shot" and local agents are "keep around")?
