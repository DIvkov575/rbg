# HLD: rbg — Delegate and Synchronize Agent Work Across Machines (`rbg`)

**Date:** 2026-06-30 · **Author:** divkov · **Status:** Design

## 1. Overview

### 1.1 Background
`rbg` is a laptop CLI for running Claude Code agents on other machines — chiefly
an always-on dev-desktop reached over SSH. The transport is proven and shipped: a
multiplexed SSH channel, a small desktop helper binary, and a verified contract
for launching and resuming headless `claude` sessions. What has *not* settled is
how rbg **models and presents** the work: several overlapping notions of "an
agent" have accreted, and each new capability has been harder to add than the
last. This HLD redesigns that model and its surface from the problem down.

### 1.2 Problem Statement
Delegating a task to another machine, today, without rbg's help, means:
- **Manual SSH juggling** — remembering hosts, `cd`-ing to the right directory,
  invoking `claude` by hand, then SSH-ing back to check on it, across many
  terminals and with all the state in the operator's head.
- **No unified view** — agents run in scattered places (laptop, desktop, and some
  started by hand outside rbg) with no single list of what is running where and in
  what state.
- **Code/repo drift** — the target machine's checkout is stale or diverged, so
  delegating a task first requires reconciling git state, which is easy to get
  wrong and easy to forget.

And rbg's own model has drifted: overlapping agent notions with parallel storage
and parallel code paths, so a new view or capability touches many places at once.

### 1.3 Goals
- **Delegate seamlessly** — compose a task and run it on a chosen machine without
  manual SSH, either immediately or held for manual launch later.
- **One unified view** — a single, accurate picture of every agent on every
  managed machine, whoever started it.
- **Keep machines in sync** — both the *code* a task runs against and the *agent
  list itself* stay aligned across machines; conversation transcripts are a
  secondary convenience.
- **Stay simple to extend** — one coherent model so a new view or action is a
  small, local change, not a cross-cutting one.

## 2. Requirements

### 2.1 Functional
- **F1 — Delegate a task to a machine.** Choose a machine and a working context
  (a repository/directory), give a task, and run it there. "Machine" includes the
  laptop itself, so local and remote delegation are the same operation.
- **F2 — Fire now or hold for later.** A delegated task may run immediately, or be
  prepared and held until the operator manually launches it (e.g. after a prior
  job finishes). Held tasks always carry a real task description — there are no
  empty placeholder agents. Launching is always a manual act; rbg does not
  schedule.
- **F3 — Unified inventory.** Present one list of all agents across all managed
  machines, each showing where it runs, its live state, and whether rbg started
  it. Agents launched outside rbg on a managed machine appear here too and can be
  taken under management.
- **F4 — Act on any agent.** From the inventory: read its output, continue it with
  a follow-up, and stop it — regardless of which machine it is on.
- **F5 — Code synchronization.** Before running a delegated task, reconcile the
  target's repository state so the task runs against the intended code; surface
  each context's sync status (aligned / ahead / behind / dirty) in the view.
- **F6 — Agent-list synchronization.** The inventory reflects reality on every
  managed machine, reconciling rbg's own records with what is actually running
  there, so the list does not drift from the machines it describes.
- **F7 — Group by project.** Offer a view that groups agents by the repository
  they belong to, carrying that context's sync status, since a repo is the natural
  unit an operator reasons about when delegating.
- **F8 — Transcript access (secondary).** Allow reading a remote agent's
  conversation from the laptop, copying its transcript across machines on demand.
- **F9 — Scriptable surface.** Every operation is available non-interactively (not
  only through the interactive dashboard), so agents and scripts can drive rbg.

### 2.2 Non-Functional
- Single self-contained binary per machine; no third-party runtime dependencies;
  no long-running daemon on the desktop.
- The interactive surface is testable without a real terminal or a real SSH
  connection (I/O is injected, not hard-wired).
- Adding a view or an action should be a local change, not a change rippling
  through parallel type hierarchies.

### 2.3 Out of Scope
- Scheduling or time/event-triggered execution (F2 is manual-launch only).
- Empty/blank placeholder agents (a held task always has a task).
- Managing arbitrary machines beyond the operator's laptop and configured
  desktop(s).
- Transcript *merging* or live process migration between machines.
- Credential/auth brokering for the remote machine (private-repo auth on the
  remote is a known limitation, not solved here).

## 3. Solution Options

The central decision is **how to model "an agent"** so that local/remote,
now/later, and rbg-started/foreign all coexist without multiplying types and
storage. Everything else (views, sync, actions) follows from this choice.

**Option A — Distinct types per notion.** A separate type, store, and code path
for each notion (remote agent, local agent, held task, foreign agent). *This is
essentially today's shape.* Straightforward to read one path in isolation, but
every capability that spans notions (a unified list, a shared action, a new view)
must be implemented once per type. Cross-cutting features are expensive and drift
apart — the observed failure mode.

**Option B ★ — One agent concept described by independent attributes.** There is a
single notion of "a delegated unit of work," and the distinctions are *attributes*
of it, not separate types:
- *where* it runs (a machine — the laptop is just one machine),
- *how far along* it is (prepared-but-held → running → finished),
- *whether rbg started it* (managed vs. discovered-and-adoptable),
- its *code sync status* (derived from the repository it targets).

A held task is one whose lifecycle attribute is "not yet launched." A local agent
and a remote agent differ only in *where*. A foreign agent differs only in
*whether rbg started it*, and adopting it just flips that attribute. Because the
distinctions are independent attributes rather than a fixed enumeration of types,
a new combination needs no new type, and every view is a *filter or grouping* over
the one inventory. Cross-cutting features — the very ones that are expensive under
Option A — become the cheap, default case. **Chosen.**

**Option C — Model per lifecycle stage.** Separate "task to run" and "running
session" as different first-class things joined by reference. Cleaner than A, but
re-introduces two vocabularies for what the operator experiences as one thing
moving through stages, and forces every view to join across them. Rejected in
favor of B's single-vocabulary model.

## 4. Current State

```
        laptop (rbg)                              dev-desktop
   ┌────────────────────┐                    ┌──────────────────┐
   │ remote-agent store │──── SSH (mux) ────▶│  helper binary   │
   │ held-task store    │                    │  runs `claude`   │
   │ local-agent store  │  (each notion its  │  (headless)      │
   │ + foreign flag     │   own type + path) │                  │
   └────────┬───────────┘                    └──────────────────┘
            │ interactive surface driven by many mode flags
            ▼
     views are bespoke per notion; a shared action is written N times
```

What breaks: the transport works, but the *model* fragments the same idea into
several types with parallel storage and parallel handling. A unified list, a
shared action, or a new view must be built once per notion, so features that span
notions are expensive and tend to drift out of agreement. This is the concrete
motivation for the redesign — not a transport problem.

## 5. Design Proposal

### 5.1 Architecture
```
   Interactive dashboard  ────┐        ┌──── Scriptable command surface (F9)
   (views = filters/groups)   │        │
                              ▼        ▼
                     ┌────────────────────────┐
                     │  Presentation (pure)    │  no I/O: given the inventory +
                     │  views, navigation      │  input, produce screen + intent
                     └───────────┬────────────┘
                                 │ intents
                     ┌───────────▼────────────┐
                     │  Domain (pure)          │  the ONE agent concept;
                     │  inventory, lenses,     │  reconcile records vs. reality;
                     │  sync-status derivation │  filter / group (F3,F5,F6,F7)
                     └───────────┬────────────┘
                                 │ capability calls
                     ┌───────────▼────────────┐
                     │  Capabilities (I/O)     │  narrow interfaces, per concern:
                     │  • list agents on a host│   (F3,F6)
                     │  • run / continue / stop│   (F1,F2,F4)
                     │  • repo sync + status   │   (F5)
                     │  • transcript transfer  │   (F8)
                     └───────────┬────────────┘
                                 │
                   ┌─────────────▼──────────────┐
                   │  Existing transport (kept)  │  SSH mux · desktop helper ·
                   │                             │  claude contract · config
                   └─────────────────────────────┘
```
Four layers, dependencies pointing inward. **Presentation** and **Domain** are
pure — no I/O — so both the dashboard and the scriptable surface (F9) are thin
consumers of the same core, and the interactive surface is testable without a
terminal or SSH (NFR). **Capabilities** are narrow interfaces (one per concern)
with a real implementation over the existing transport and a fake for tests. The
transport layer is proven and unchanged.

### 5.2 The one agent concept (Option B, abstractly)
Domain holds a single kind of thing — a *delegated unit of work* — carrying its
identity (a stable handle, the repository/directory it targets, its task text, and
its session identity once it has run) plus the independent attributes from §3:
*where* it runs, *how far along* it is, *whether rbg started it*, and its *derived
code-sync status*. No separate types for local/remote/held/foreign; those are
readings of these attributes.

### 5.3 One inventory, reconciled (F3, F6)
There is a single inventory of agents, not one store per notion. It is produced by
**reconciling rbg's own records with live reality on each managed machine**: query
what agents actually exist on each host, match them against what rbg recorded, and
from the match set each agent's attributes — *where* (which host answered), *how
far along* (the host's reported state), and *whether rbg started it* (in records →
managed; present on a host but not in records → foreign, and adoptable per F3).
Code-sync status (§5.5) is filled per repository/context. The result is one list
that *is* the reconciled truth across machines, refreshed on open and on demand
rather than continuously.

### 5.4 Views as filters and groupings (F3, F7)
Because there is one inventory, every view is a **lens** over it, not a bespoke
screen bound to a type:
- by machine — agents on the laptop, or on the desktop,
- combined — everything, sectioned by machine,
- **by project** — grouped by repository, each group carrying its sync status
  (F5, F7); this doubles as the delegation surface, since a group *is* the repo
  context a new task would target.
Held tasks (F2) and foreign agents (F3) are not separate views — they are just
agents with a particular lifecycle or origin attribute, shown inline with a
distinct marker and the appropriate available actions.

### 5.5 Code synchronization (F5)
Delegation is **sync-first**: before a task runs on a machine, rbg reconciles the
target repository so the task runs against the intended code, and it surfaces each
context's status — aligned, ahead, behind, or dirty — in the by-project view so
the operator sees drift before acting. Status derivation is a pure function of
observed repository facts; performing the reconciliation is a capability call.

### 5.6 Actions, gated by attributes (F1, F2, F4, F8)
There is one vocabulary of actions; which are *offered* for a given agent follows
from its attributes:
- **Run / launch** — for a fire-now task, or to manually launch a held one (F1,
  F2); always sync-first (§5.5); dispatched to local execution or to the remote
  helper according to *where*.
- **Continue** (follow-up), **Read** (output/transcript), **Stop** — for a running
  agent, dispatched by *where* (F4, F8).
- **Adopt** — for a foreign agent, flips it to managed (F3).
- **Sync transcript** — copy a conversation across machines on demand (F8).
The interactive dashboard and the scriptable surface (F9) invoke the *same*
actions against the *same* inventory; the dashboard is one front-end, scripting is
another.

## 6. Design Analysis

### 6.1 Key Improvements
- **Cross-cutting features become the default.** A unified list, a shared action,
  and a new view — the things that were expensive per-notion — are now a filter, a
  gate on an attribute, and a lens. New capability = a new attribute reading or a
  new lens, not a new type replicated across storage and handling.
- **The operator's mental model matches the code.** One "agent" that moves through
  states and lives somewhere, exactly as experienced — no parallel vocabularies.
- **Sync is first-class, not bolted on.** Code-sync status lives on the agent and
  drives sync-first delegation; agent-list sync is the reconciliation that builds
  the inventory, so "the list is accurate everywhere" is structural, not a feature
  to remember to run.
- **Testable and scriptable by construction.** Pure Domain/Presentation + injected
  Capabilities means the dashboard is testable without a TTY/SSH, and the
  scriptable surface reuses the identical core.

### 6.2 Risks
| Risk | Mitigation |
|---|---|
| Collapsing notions into one concept over-generalizes and the concept becomes a catch-all | The attributes are few and independent; behavior lives in lenses/actions/capabilities, not on the concept — it stays data, not a god-object |
| Reconciliation mis-classifies managed vs. foreign agents | Match on the stable session identity first, fall back to handle+context; adoption is explicit and idempotent |
| Querying live reality on every refresh is slow | Refresh on open and on explicit request, not continuously; cache per-context sync status briefly |
| Sync-first delegation blocks on repo state rbg can't resolve (e.g. remote auth) | Surface the drift/failure in the view and let the operator choose; do not silently proceed against wrong code (auth brokering is explicitly out of scope) |
| Reintroducing drift by letting the scriptable and interactive surfaces diverge | Both are thin consumers of the same Domain actions and inventory; no logic lives in either front-end |

### 6.3 Delivery Order
Requirements suggest a natural sequence, each independently verifiable: the pure
**Domain** (the one concept, reconciliation, lenses, sync-status) first; then the
**Capabilities** over the existing transport (with fakes for test); then the
**scriptable surface** (F9) — fully usable and testable headless; then the
interactive **dashboard** as the second front-end. Transcript transfer (F8) and
foreign-agent adoption (F3) are small additions on top of that spine.
