# rbg v2 integration tests

Drives the real Go `rbg` client against a sandboxed non-root `sshd` running the
real `rbg-agent` binary plus a fake `claude` — **no tmux**. Proves the
agent-binary mechanics end-to-end: connection gate, launch, ls, read, send
(detached child + flock), all over real SSH with a structured (and shell-quoted)
remote command.

Includes a dedicated injection test
(`test_shell_metacharacters_in_task_are_quoted_not_executed`) that launches with
a task containing `; touch ~/PWNED` and asserts the file is NOT created on the
sandbox desktop — verifying the `sshx.RemoteCommand` quoting holds across the
real ssh + desktop-login-shell path. (OpenSSH re-parses the remote command
through `$SHELL -c`, so quoting is required; this test guards that property.)

What it does NOT prove: the real `claude` flags/JSON contract — that lives in
`internal/claudecli/claude.go` and is verified once manually on a box with
`claude` (deferred Task 15).

## Running

```sh
pytest test/integration_v2/ -m integration -v
```

Skips automatically without `go` or `/usr/sbin/sshd`.

## Files

- `fake_claude.py` — stand-in `claude` (launch / agents --json / resume-headless
  / resume-interactive), rooted at `$HOME`, transcript slug `sim-project` to
  match the agent's `transcriptPath`.
- `sshd_harness.py` — `SimDesktop`: boots/stops the sandboxed sshd, installs the
  prebuilt `rbg-agent` into its PATH, exposes `rbg_env()`. No tmux.
- `test_integration_v2.py` — the tests; build the binaries, drive them over SSH.
