# HLD: `rbg` — Remote Claude Agent Management (as built)

**Date:** 2026-06-29 · **Author:** divkov · **Status:** Built (v2)

## 1. Problem

Claude agents run on a persistent dev desktop (GPU box) reached over SSH. There
was no laptop-side way to launch a background agent, send it a follow-up task,
read its output, or attach interactively — all of it was hand-driven over an SSH
shell.

## 2. Approach

A laptop command (`rbg`) drives a **real Go program on the desktop**
(`rbg-agent`), invoked over SSH per command — no persistent connection, no
daemon. The remote side is a structured binary with verbs and JSON, not ad-hoc
shell. One Go module builds both binaries; the agent ships to the desktop via
`rbg deploy` (cross-compiled for its arch, scp'd into `~/.local/bin`).

This replaced a v1 design that drove `tmux`, `tail`, and transcript globbing as
shell strings over SSH. v2 removes all of that: detachment via `setsid`,
serialization via `flock`, transcript path owned in agent state.

```
 LAPTOP                                   DEV DESKTOP
 rbg <cmd>                                ~/.local/bin/rbg-agent (static binary)
   | gate: ssh -o BatchMode -o ConnectTimeout=5 host true
   |   fail -> "cannot reach '<host>' — disconnected", exit 1
   |- deploy : scp cross-compiled rbg-agent -> host
   |- launch : ssh host -- rbg-agent launch --name x --task '…'  -> JSON {id}
   |- send   : ssh host -- rbg-agent send   --id x  --task '…'   -> JSON {ok}
   |- read   : ssh host -- rbg-agent read   --id x               -> rendered text
   |- ls     : ssh host -- rbg-agent ls                          -> JSON [...]
   \- attach : ssh -t host -- claude --resume <id>               (interactive)
```

## 3. Commands

| Command | Behavior |
|---|---|
| `rbg launch <name> "<task>"` | start a `claude --bg` agent; agent resolves & records its session id |
| `rbg send <name> "<task>"` | spawn a detached headless `claude -p --resume` child; non-blocking |
| `rbg read <name>` | render the session transcript to text |
| `rbg ls` | list recorded sessions |
| `rbg attach <name>` | interactive `claude --resume` over an ssh tty |
| `rbg ping` | connection check |
| `rbg deploy` | build & install `rbg-agent` on the desktop |

## 4. Design points

- **SSH is transport only.** The agent is exec'd with a structured argv. Because
  OpenSSH re-parses the remote command through the desktop login shell
  (`$SHELL -c`), the task string is shell-quoted at a **single chokepoint**
  (`sshx.RemoteCommand`) — not scattered per-command as in v1.
- **Detachment, not tmux.** `send` spawns the child with `setsid` and redirected
  stdio, so it outlives the SSH session. The agent returns immediately.
- **Serialization via `flock`.** A non-blocking per-session lock guards sends;
  busy maps to exit code 3.
- **`claude` contract isolated.** Every assumption about the real `claude`
  (argv shapes, `agents --json` parsing) lives only in
  `internal/claudecli/claude.go`.
- **Connection gate first.** Every networked verb probes reachability before
  acting; the exact disconnection message and exit 1 are preserved.
- **Config:** env over `~/.rbg.conf` — `RBG_HOST` (required), `RBG_CWD`,
  `RBG_SSH`, `RBG_AGENT_PATH`.

## 5. Layout

```
cmd/rbg/            laptop client entrypoint (+ attach, deploy)
cmd/rbg-agent/      desktop agent entrypoint
internal/config/    client config (env over ~/.rbg.conf)
internal/sshx/      ssh argv builder, connection gate, remote-command quoting
internal/render/    transcript JSONL -> text
internal/session/   agent session-state store + flock
internal/claudecli/ isolated claude argv/JSON contract
internal/agent/     desktop verbs (launch/send/read/ls) + detached spawn
internal/client/    laptop verbs over ssh
internal/run/        subprocess Runner seam (Exec real / Recording test stub)
test/integration_v2/ end-to-end tests over a sandboxed non-root sshd (no tmux)
```

## 6. Testing

- **Unit (Go):** all logic is testable without SSH/claude via an injectable
  `run.Runner`. `go test ./...`.
- **Integration:** a non-root sandboxed `sshd` on loopback runs the real
  `rbg-agent` + a fake `claude`; the real `rbg` client drives it over SSH.
  Includes an injection test proving the remote-command quoting survives the
  desktop login shell. `pytest -m integration`.
- **Not covered by simulation:** the real `claude` flag/JSON contract — verified
  once manually on a box with `claude`.

## 7. Known limitations

- `send` serialization is best-effort: the `flock` is released when the agent
  process exits (just after spawning), so concurrent sends during a long-running
  child are not rejected.
- `read --follow` (live streaming) is not implemented; `read` emits the current
  transcript once.
- Session `pid` and agent version-mismatch warnings are unimplemented.
