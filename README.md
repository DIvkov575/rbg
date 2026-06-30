# rbg — remote Claude agent management

Manage Claude Code sessions running on a remote dev desktop (e.g. an Amazon
dev-dsk) from your laptop, over SSH. Launch a background task, send follow-ups,
read the transcript, attach interactively, or drive it all from a TUI dashboard.

> **For agents:** this README is the operating manual. Sections are ordered
> mental-model → commands → architecture → invariants → how-to-change → gotchas.
> The non-obvious constraints are in **Invariants** and **Gotchas** — read those
> before editing transport, quoting, or the claude contract.

---

## Mental model

Two Go binaries, one module (`github.com/divkov575/rbg`):

| Binary | Runs on | Role |
|---|---|---|
| `rbg` (`cmd/rbg`) | your laptop | client: loads config, runs the connection gate, invokes `rbg-agent` over SSH, renders results |
| `rbg-agent` (`cmd/rbg-agent`) | the desktop | agent: owns session state, spawns `claude`, locates/reads transcripts. Exec'd directly by sshd — **never via a shell** |

Per command: `rbg` opens SSH → runs `rbg-agent <verb> --flags` → gets JSON (or
rendered text) back → connection closes (but is reused; see Multiplexing).
There is **no daemon** and **no persistent agent process** — by design.

"agent" is overloaded; disambiguate:
- **`rbg-agent`** — the desktop Go binary (what "agent binary" means).
- **a launched agent** — a `claude` session doing a task (created by `rbg launch`).
- **`~/.rbg-agent/`** — the agent's state dir on the desktop.

---

## Quick start

```sh
# 1. install the client (→ ~/.local/bin/rbg, which must be on PATH)
make install

# 2. point it at your desktop (env, or ~/.rbg.conf as KEY=value lines; env wins)
export RBG_HOST=dev-dsk-xxxxx.us-east-1.amazon.com
export RBG_CWD=/local/home/$USER     # remote working dir for agents (optional)

# 3. install the agent binary on the desktop (cross-compiled + scp'd)
rbg deploy

# 4. use it
rbg launch "investigate the flaky payments test"   # auto-named from the task
rbg ls
rbg read <name>
rbg send <name> "now write the fix and run the tests"
rbg attach <name>     # interactive TTY
rbg                   # no args → dashboard TUI
rbg help              # full usage (works without RBG_HOST)
```

Prerequisite on the desktop: the `claude` CLI must be on the **non-login**
SSH PATH (see Gotchas). `rbg` itself needs only SSH + Go (to build/deploy).

---

## Commands

| Command | Behavior | Exit |
|---|---|---|
| `rbg launch "<task>"` | launch agent; name auto-derived from task (slug, deduped) | 0 ok |
| `rbg launch <name> "<task>"` | launch with explicit name | |
| `rbg send <name> "<task>"` | send a follow-up; non-blocking | 3 = busy |
| `rbg read <name>` | print the rendered transcript | |
| `rbg ls` | list recorded sessions (compact JSON) | |
| `rbg attach <name>` | interactive `claude --resume` over an ssh `-t` tty | |
| `rbg ping` | reachability check | 1 = down |
| `rbg deploy` | cross-compile `rbg-agent` for the desktop arch + scp it | |
| `rbg`, `rbg dash` | interactive dashboard (↑/↓ move, ⏎/v view, a attach, r refresh, q quit) | |
| `rbg help`, `-h`, `--help` | full usage; works with no config | 0 |

Connection gate: every networked verb first runs
`ssh -o BatchMode=yes -o ConnectTimeout=5 <host> true`; on failure it prints
exactly `cannot reach '<host>' — disconnected` to stderr and exits 1.

---

## Configuration

Env overrides `~/.rbg.conf` (KEY=value, `#` comments, quotes stripped).

| Var | Default | Meaning |
|---|---|---|
| `RBG_HOST` | — (required) | desktop hostname |
| `RBG_CWD` | "" | remote working dir for agents (`--cwd` flag to the agent) |
| `RBG_SSH` | "" | extra ssh options, space-split (e.g. `-i ~/.ssh/k -p 2222`) |
| `RBG_AGENT_PATH` | `.local/bin/rbg-agent` | remote agent path (relative → resolved from `$HOME`) |
| `RBG_MUX` | on | SSH connection multiplexing; `0`/`false`/`no`/`off` disables |
| `RBG_CONTROL_PATH` | `~/.rbg/cm-%C` | ssh ControlPath socket |
| `RBG_CONTROL_PERSIST` | `10m` | ssh ControlPersist idle window |

---

## Architecture (packages)

```
cmd/rbg/            client entrypoint, flag parse, attach, deploy, help, dash wiring
cmd/rbg-agent/      agent entrypoint, flag parse, verb dispatch
internal/config/    load config (env over ~/.rbg.conf); mux defaults
internal/sshx/      build ssh argv, connection gate, RemoteCommand quoting, mux injection
internal/client/    laptop verbs over ssh + structured fetchers (FetchSessions/FetchTranscript)
internal/agent/     desktop verbs: Launch/Send/Read/Ls, detached spawn, flock, transcript glob
internal/claudecli/ the ISOLATED claude contract: LaunchHeadlessArgs / ResumeHeadlessArgs
internal/session/   session-state store (JSON) + non-blocking flock
internal/slug/      task → agent-name slug
internal/render/    transcript JSONL → human text
internal/run/       Runner seam: Exec (real) / Recording (test stub)
internal/tui/       dashboard: pure model (Update/View) + build-tagged raw-terminal layer
```

Data flow (launch): `client.Launch` → ssh → `agent.Launch` generates a UUID,
spawns detached `claude -p <task> --session-id <uuid>`, records
`{name, claudeSessionId, transcriptPath}` in `~/.rbg-agent/sessions.json`,
prints JSON. `read` locates the transcript by **globbing the session id** across
`~/.claude/projects/*/` (claude picks the project dir from its cwd; the agent
cannot predict it).

---

## Invariants (do not break)

1. **No shell on the remote side.** SSH execs `rbg-agent` with a structured
   argv. But OpenSSH joins the remote args into one string the desktop login
   shell re-parses, so `sshx.RemoteCommand` POSIX-single-quotes **every** token.
   All quoting lives in that one function. Never build a remote command by
   string concatenation elsewhere; never wrap in `sh -c`.
2. **The claude contract lives only in `internal/claudecli`.** If real claude's
   flags/output change, that is the one file to edit. Verified contract:
   `claude -p <task> --session-id <uuid>` to launch (no `--bg`),
   `claude -p <task> --resume <uuid>` to send (appends, does not fork).
   Do **not** add `--output-format stream-json` (rejected by the ASBX build).
3. **Launch must not use `--bg`.** A `--bg` agent stays resident and *locks* the
   session, making `-p --resume` fail. Client-generated `--session-id` + plain
   `-p` leaves no resident process, so sends append cleanly.
4. **Every networked verb runs the connection gate first** (see Commands).
5. **Testability via the `run.Runner` seam.** All logic is unit-tested without
   SSH/claude by injecting `run.Recording`. Keep new subprocess calls behind it.
6. **TUI logic is pure.** `tui.Update(Model, Key) (Model, Action)` and
   `tui.View(Model) string` do no I/O and are fully unit-tested; the
   raw-terminal layer (`term*.go`) is thin and integration-only.
7. **Zero third-party Go dependencies.** stdlib only (`go.mod` has no `require`
   block). The Go module proxy is often unreachable here — do not add deps.

---

## Build, test, change

```sh
make build        # → ./bin/rbg
make install      # → $(PREFIX), default ~/.local/bin
make test         # go test ./...            (fast, no network)
make test-all     # go test ./... + pytest -m integration  (needs sshd + go)
make fmt vet
make deploy       # rbg deploy (needs RBG_HOST)
GOOS=linux GOARCH=amd64 go build ./...   # the agent ships to linux/amd64 desktops
```

Workflow for changes: write the failing test, implement, `make test`, then for
anything touching SSH/quoting/claude run `make test-all`. The integration suite
(`test/integration_v2/`) boots a **real non-root sshd** on loopback with a fake
`claude` and the real `rbg-agent`, exercising the full path over genuine SSH;
`test/integration/` is the older v1 (Python CLI) harness, kept for history.
Mark integration tests `@pytest.mark.integration` (they auto-skip without
`go`/`sshd`).

---

## Gotchas (learned the hard way, verified on a real dev-dsk)

- **claude must be on the non-login SSH PATH.** `ssh host -- cmd` uses a
  non-login, non-interactive shell. Toolbox installs `claude` to `~/.toolbox/bin`,
  which `.zshrc` adds — but that is not read for non-login shells. Put the PATH
  export in `~/.zshenv` (zsh reads it for every invocation) or the agent gets
  exit 127 / "could not resolve session id".
- **Don't quote `~` in the agent path.** `RemoteCommand` single-quotes every
  token, and the shell won't expand `~` inside single quotes. The default agent
  path is therefore the relative `.local/bin/rbg-agent` (resolved from the
  SSH-default `$HOME`), not `~/.local/bin/rbg-agent`.
- **`launch`/`send` are fire-and-forget.** They spawn the claude child detached
  (`setsid`) and return immediately; the transcript may not exist yet. Re-run
  `read` (or wait) — there is no polling.
- **`read` is a snapshot, not a live tail.** `-f/--follow` is reserved, not
  implemented.
- **First command after idle is slow.** Multiplexing opens a fresh master if the
  control socket expired (`ControlPersist`, default 10m); then commands are hot
  (~0.2s). Raise `RBG_CONTROL_PERSIST` to keep it warm longer.
- **`send` serialization is best-effort.** The per-session `flock` is released
  when the short-lived agent process exits, so two sends overlapping a long
  child run are not reliably rejected.
- **The slug caps at 4 words / 40 chars** and drops stopwords, so
  `"reply with exactly the word ALPHA"` → `reply-with-exactly-word`. Use the
  `id` printed by `launch` (or `rbg ls`) as the source of truth for names.

---

## Design docs

- `docs/HLD-rbg-as-built.md` — current architecture, one page.
- `docs/HLD-rbg-v2-agent-binary.md` — the Go agent-binary rewrite rationale.
- `docs/HLD-remote-agent-management.md` — original v1 (bash→Python) design.
- `docs/superpowers/{specs,plans}/` — feature specs and TDD implementation plans.
