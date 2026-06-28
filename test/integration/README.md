# rbg integration tests

End-to-end tests that drive the real `rbg.py` CLI against a **simulated dev
desktop** — a throwaway, non-root `sshd` on a random high port, with a fake
`claude` shim and real `tmux` on the remote PATH. No Docker, no Kubernetes, no
admin rights, no real `~/.claude` touched.

## Why this exists (and what it does NOT prove)

The unit suite (`tests/`, 49 tests) stubs out SSH via an injectable runner, so
it never exercises the actual ssh / tmux / tail plumbing. These integration
tests fill that gap: they run `rbg.py` as a subprocess over a genuine SSH
connection and assert on real rendered output.

**They prove rbg's mechanics work:** the connection gate, ssh argument wiring,
the per-session tmux window, transcript tailing/rendering, name→id resolution,
and the local session map.

**They do NOT prove rbg's assumptions about the real `claude`:** the JSON shape
of `claude agents --json` and whether `claude -p --resume` appends vs. forks are
*baked into the fake shim* (`fake_claude.py`). Confirming the real contract
still requires running once against a real, authenticated `claude` — that is the
deferred manual check (plan Task 2 / Task 14). A green run here is necessary,
not sufficient.

## Why not Kubernetes / Docker

`rbg` targets **one** persistent SSH host, not a fleet — multi-host pools are
explicitly out of scope in the HLD. The whole "remote desktop" is faithfully
simulated by a single sandboxed `sshd` process. A container or cluster would add
orchestration that models nothing about the real target and gives more moving
parts to debug than the code under test.

## Running

```sh
# fast unit suite only (default for development)
pytest -m "not integration"

# the integration suite (opt-in)
pytest test/integration/ -m integration -v
```

The integration tests **skip automatically** if `/usr/sbin/sshd` or `tmux` is
unavailable.

## How the sandbox is isolated

`SimDesktop` (in `sshd_harness.py`) launches `sshd` with a generated host key and
a single authorized client key, and injects a sandboxed environment via the
config's `SetEnv`:

| Var | Points at | Why |
|---|---|---|
| `PATH` | a `bin/` with the `claude` shim + a `tmux` symlink | so the remote shell finds our fake claude and real tmux |
| `HOME` | a throwaway dir | so `~/.claude` transcripts never hit the real home |
| `TMUX_TMPDIR` | a short `/tmp/rbgt<port>` dir | tmux's unix socket has a ~104-char limit; pytest's deep `tmp_path` overflows it and the tmux server silently fails to start |

Client-side, each test points `rbg`'s `HOME` at its own `tmp_path` too, so the
local `~/.rbg/sessions.json` map is isolated per test.

## Files

- `fake_claude.py` — the stand-in `claude` binary (launch / agents --json /
  resume-headless / resume-interactive), rooted at `$HOME`.
- `sshd_harness.py` — `SimDesktop`: boots/stops the sandboxed sshd, exposes
  host/port/ssh-opts and an `rbg_env()` helper.
- `test_integration.py` — the tests themselves.
