# HLD: Remote Claude Agent Management (`rbg`)

**Date:** 2026-06-26 · **Author:** divkov · **Status:** Design

## 1. Overview
### 1.1 Background
Claude agents run on a persistent dev desktop (GPU box) reached over SSH. Today
they're managed by SSHing in and driving `claude` by hand. There's no laptop-side
way to fire a task at an ongoing session and read its output back.

### 1.2 Problem Statement
- No one command to launch a remote background agent from the laptop.
- No way to send a follow-up task to an *ongoing* remote session non-interactively.
- No way to read an agent's output back locally, live or after the fact.
- Want interactive access to the same sessions when hands-on steering is needed.

### 1.3 Goals
- Launch, send-to, read-from, and attach-to remote `--bg` agents via one laptop CLI.
- Reading output never depends on undocumented internal formats.
- Forward when connected; clear disconnection error when not.

## 2. Requirements
### 2.1 Functional
- **launch** — start a named `--bg` agent on the desktop; return immediately.
- **send** — give an ongoing session a new task headlessly; non-blocking.
- **read** — stream or replay an agent's output locally (tail its transcript).
- **attach** — drop into a session interactively over a tty.
- **ls** — list remote agents.
- **Connection gate** — every command checks reachability first
  (`ssh -o BatchMode=yes -o ConnectTimeout=5`); on failure: print
  `cannot reach '<host>' — disconnected`, exit 1, before doing anything.

### 2.2 Out of Scope
Mirroring agents into the laptop's native `claude agents` view (separate
deferred project); multi-host pools; ephemeral EC2 lifecycle; a warm resident
agent process between tasks.

## 3. Solution Options
Decision: how to inject a follow-up task into an ongoing session.
- **Option A — message the live `--bg` process.** No CLI verb exists for this
  (`claude agents` has no `send`/`dispatch`); only the interactive TUI can steer a
  live agent. Rejected for the non-interactive path.
- **Option B ★ — resume the session transcript headlessly.**
  `claude -p "<task>" --resume <id> --output-format stream-json`. A fresh
  short-lived run appends to the same conversation and streams structured output.
  Documented, scriptable, and the output path is a plain JSONL file we can tail.

Detach/stream model: run the `send` invocation inside a per-session **tmux**
window on the desktop so it survives laptop disconnect; `read` follows the
transcript file. Streaming without blocking, decoupled viewer from worker.

Implementation language: a single **Python 3 (stdlib-only)** executable, not
bash. `rbg` parses three different JSON surfaces (the `claude agents --json`
listing, the per-session name→id map, and the line-delimited transcript) and
orchestrates ssh/tmux subprocesses with live streaming. bash needs `jq` plus
fragile string-munging for each of these; Python's built-in `json` and
`subprocess` handle them directly, the result is unit-testable with `pytest`,
and it still ships as one self-contained file with no third-party dependencies.

## 4. Current State
```
 laptop ── ssh ──▶ desktop:  $ claude ...   (manual, interactive only)
```
Everything is hand-driven over an SSH shell; nothing is scriptable from the laptop
and output stays on the desktop.

## 5. Design Proposal
### 5.1 Architecture
```
 LAPTOP                                   DEV DESKTOP
 rbg <cmd> ─ reachable? ─ no ─▶ "disconnected", exit 1
              │ yes
              ├─ launch : ssh   → claude --bg -n <name> "<task>"
              ├─ send   : ssh   → tmux: claude -p "<task>" --resume <id> --output-format stream-json
              ├─ read   : ssh   → tail [-f] ~/.claude/projects/<slug>/<id>.jsonl  (parsed)
              └─ attach : ssh -t → claude --resume <id>   (or `claude agents` TUI)
```
Single Python 3 stdlib-only executable (`rbg`). Config via env / `~/.rbg.conf`:
`RBG_HOST` (required), `RBG_CWD` (remote project dir), `RBG_SSH` (extra ssh
opts). A local `~/.rbg/sessions.json` map lets you address agents by friendly
name → sessionId; `launch` populates it by resolving the new agent's id from
`claude agents --json --all` (matched by name) after starting it.

### 5.2 Commands
| Command | Maps to | Notes |
|---|---|---|
| `rbg launch <name> "<task>"` | `claude --bg -n <name> "<task>"` | records name→id |
| `rbg send <name> "<task>"` | `claude -p --resume <id>` in a tmux window | non-blocking follow-up |
| `rbg read <name> [-f]` | `tail [-f]` the transcript, rendered to text | output back locally |
| `rbg ls` | `claude agents --json --all` | list remote agents |
| `rbg attach <name>` | `ssh -t … claude --resume <id>` | interactive |
| `rbg ping` | `ssh … true` | connection check |

### 5.3 Reading output
Transcript at `~/.claude/projects/<slug>/<sessionId>.jsonl` is clean per-line
JSON. `read` tails it (`ssh … tail [-f]`) and Python parses each line with the
stdlib `json` module, rendering assistant/tool text and tolerating unknown keys.
`read -f` follows live; Ctrl-C stops the tail only — the remote task and its
tmux window are untouched.

### 5.4 Verified facts (v2.1.187)
- `claude --bg "<task>"` launches a supervisor-owned bg agent, returns at once;
  `-n` names it.
- `claude agents` is a TUI; `--json [--all]` lists. **No** `send`/`dispatch` verb.
- `claude -p "<task>" --resume <id> --output-format stream-json` runs headless
  against an existing session; `--fork-session` branches instead of appending.
- Transcript JSONL is line-delimited and tail-able (keys: `type`, `message`,
  `sessionId`, `timestamp`, …).

## 6. Design Analysis
### 6.1 Key Improvements
- One laptop CLI for the full launch → send → read → attach loop.
- Output read from a documented JSONL transcript, not the internal job registry.
- Long tasks survive laptop disconnect (tmux); viewer attaches/detaches freely.

### 6.2 Risks
| Risk | Mitigation |
|---|---|
| `--bg` agent ≠ the `-p --resume` run; they share a transcript, not a process | Use `attach` (TUI) when you must steer the *live* agent; `send` is for task→output |
| Two concurrent `send`s to one session corrupt ordering | Serialize per session (one tmux window per name; queue or reject if busy) |
| `-p --resume` on a `--bg`-started session may need `--fork-session` | Confirm append-vs-fork on the desktop before relying on it |
| Undocumented transcript schema could change | `read` tolerates unknown keys; render defensively |
