# HLD: `rbg` v2 — Remote Agent as a Real Binary

**Date:** 2026-06-28 · **Author:** divkov · **Status:** Design · **Supersedes:** v1 (`HLD-remote-agent-management.md`)

## 1. Why v2

v1 worked but was **programming in bash over SSH**: every remote operation was a
shell string — `tmux new-window`, `tail -f`, a `*` glob over the transcript,
and `shlex.quote` on every interpolated value. The symptoms followed directly:
tmux abused as a process supervisor, serialization by grepping
`tmux list-windows`, a shell-injection guard on `session_id`, and a
`TMUX_TMPDIR` socket-length bug in the test harness.

The root cause was never SSH — it was composing shell on the far end. v2 keeps
SSH purely as transport and replaces the shell strings with **a real program on
the desktop** invoked with structured arguments.

## 2. Goals / Non-goals

### Goals
- One laptop command per verb: `launch`, `send`, `read`, `ls`, `attach`, `ping`,
  plus `deploy` (install the agent binary on the desktop).
- **No persistent connection.** Each verb is one SSH round-trip: connect, run
  `rbg-agent <cmd> --flags`, receive JSON (or a stream), disconnect.
- **No shell on the far end.** SSH executes the agent binary directly with an
  argv; nothing is shell-interpolated, so injection is structurally impossible.
- **No runtime deps on the desktop**: no tmux, no flock, no jq, no Python. A
  single static Go binary, `scp`'d once.
- Tasks survive laptop disconnect (detachment is done in-process, not by tmux).

### Non-goals
- A long-running daemon / listening service on the desktop (explicitly rejected:
  the user wants no persistent connection and no service to supervise).
- Multi-host pools, EC2 lifecycle (same as v1).

## 3. Architecture

```
 LAPTOP                                  DEV DESKTOP
 rbg <cmd>                               ~/.local/bin/rbg-agent  (static binary)
   │  reachable? ssh -o BatchMode -o ConnectTimeout=5 host true
   │     no → "cannot reach '<host>' — disconnected", exit 1
   ├ deploy : scp rbg-agent → host:~/.local/bin/         (one-time / on update)
   ├ launch : ssh host -- rbg-agent launch --name x --task '...'   → JSON {id}
   ├ send   : ssh host -- rbg-agent send   --id <id> --task '...'   → JSON {ok}
   ├ read   : ssh host -- rbg-agent read   --id <id> [--follow]     → stream
   ├ ls     : ssh host -- rbg-agent ls                              → JSON [..]
   └ attach : ssh -t host -- claude --resume <id>                   (interactive)
```

**Same codebase, two roles.** One Go module builds two binaries:
- `rbg` — the laptop client. Resolves config, runs the connection gate, invokes
  the agent over SSH, parses its JSON, renders output.
- `rbg-agent` — the desktop program. Owns sessions: spawns the `claude` child
  detached, records state, serializes sends, streams transcripts. **Never sees a
  shell** — it's exec'd directly by sshd with structured flags.

The laptop builds the agent for the desktop's GOOS/GOARCH and `deploy` ships it.

### 3.1 How each bash dep dies

| v1 (bash over SSH) | v2 (agent binary) |
|---|---|
| `tmux new-window` to detach the send | agent forks the `claude` child with `setsid`/`Setpgid` + redirected stdio; it outlives the SSH session |
| grep `tmux list-windows` to serialize | a real advisory file lock (`flock(2)` via syscall) on the session's state file, held in code |
| `tail -f ~/.claude/projects/*/<id>.jsonl` | agent knows the path from its own state, opens and streams it; no glob |
| `shlex.quote` every arg; validate `id` vs `^[A-Za-z0-9-]+$` | args arrive as `os.Args`, never parsed by a shell; no injection surface |
| parse `claude agents --json` shape (unknown) | agent owns its own state file with a schema we define |

### 3.2 Session state (desktop-side)

The agent keeps `~/.rbg-agent/sessions.json`: a map of
`id → {name, claudeSessionId, transcriptPath, pid, startedAt}`. The agent writes
it; the laptop never parses desktop internals. `id` is the rbg-level handle
(also usable as a friendly name); `claudeSessionId` is what gets passed to
`claude --resume`.

### 3.3 Detachment & serialization (the tmux replacement)

- **launch**: agent resolves the new `claude` session, records it, returns its
  `id`. The `--bg` agent on the desktop is still claude's own background agent;
  rbg-agent just tracks it.
- **send**: agent acquires a non-blocking `flock` on `sessions/<id>.lock`; if
  already held → exit code mapped to "busy". Holding the lock, it spawns
  `claude -p <task> --resume <claudeId> --output-format stream-json` as a
  detached child (own process group, stdio → the transcript/devnull), then
  returns immediately. The child runs to completion unsupervised; `read` tails
  the transcript for results.

### 3.4 Connection gate

Unchanged contract from v1: before any verb, `ssh -o BatchMode=yes -o
ConnectTimeout=5 host true`; on failure print exactly
`cannot reach '<host>' — disconnected` to stderr, exit 1.

## 4. Config

`~/.rbg.conf` (`KEY=value`) and env, env wins:
- `RBG_HOST` (required), `RBG_CWD`, `RBG_SSH` (extra ssh opts),
- `RBG_AGENT_PATH` (default `~/.local/bin/rbg-agent` on the desktop).

## 5. Testing strategy

- **Unit (Go):** client config/SSH-arg construction, JSON parsing, render; agent
  session-state, locking, flag parsing. Pure, no network. `go test ./...`.
- **Integration:** the desktop is localhost. Build `rbg-agent`, drive the real
  `rbg` client against the sandboxed non-root `sshd` harness from v1 — but the
  remote command is now `rbg-agent <cmd>`, no tmux. The harness loses its tmux
  symlink and `TMUX_TMPDIR` hack entirely.
- **Still not proven by sim:** the real `claude`'s flags/JSON. The agent isolates
  that behind one `claude.go` so the contract lives in one file; a fake claude on
  PATH stands in for tests, real verification is a one-time manual check.

## 6. Risks

| Risk | Mitigation |
|---|---|
| Agent binary not deployed / stale on desktop | `rbg deploy`; client checks `rbg-agent version` and warns on mismatch |
| GOOS/GOARCH mismatch (laptop arm64 vs desktop amd64) | `deploy` cross-compiles for the desktop's arch (probe via `uname -m`) |
| Detached child orphaning / zombies | child is `setsid`'d into its own pgid, stdio redirected; agent does not wait |
| Real `claude` flags differ from assumption | isolated in `claude.go`; one place to fix after the manual check |
| `flock` semantics differ on the desktop FS (e.g. NFS home) | document; advisory lock is best-effort, send also records pid to detect liveness |
