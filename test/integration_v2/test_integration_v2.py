"""End-to-end tests for rbg v2 (Go) against a simulated SSH desktop.

Builds the rbg-agent binary, installs it into a sandboxed non-root sshd's PATH
alongside a fake claude, then drives the rbg client binary against it over real
SSH. No tmux anywhere. Proves the agent-binary mechanics end-to-end, including
that the shell-quoted remote command survives the desktop login shell.

Skips automatically if go or sshd is unavailable.
"""
import os
import shutil
import subprocess
import sys

import pytest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))

sys.path.insert(0, HERE)
from v2_harness import SimDesktop, SSHD_BIN  # noqa: E402

pytestmark = pytest.mark.integration

_REASON = None
if shutil.which("go") is None:
    _REASON = "go not available"
elif not os.path.exists(SSHD_BIN):
    _REASON = f"{SSHD_BIN} not available"
needs_env = pytest.mark.skipif(_REASON is not None, reason=_REASON or "")


def _build(name, tmp):
    out = os.path.join(tmp, name)
    res = subprocess.run(
        ["go", "build", "-o", out, f"github.com/divkov575/rbg/cmd/{name}"],
        cwd=REPO_ROOT, capture_output=True, text=True,
    )
    assert res.returncode == 0, f"build {name} failed: {res.stderr}"
    return out


@pytest.fixture
def env(tmp_path):
    """Built binaries + a running sim desktop with the agent installed."""
    rbg = _build("rbg", str(tmp_path))
    agent = _build("rbg-agent", str(tmp_path))
    sim = SimDesktop(str(tmp_path / "sim"))
    sim.start()
    sim.install_agent(agent)
    yield sim, rbg
    sim.stop()


def run_rbg(sim, rbg, client_home, *args, timeout=30):
    e = sim.rbg_env()
    e["HOME"] = str(client_home)
    e["RBG_AGENT_PATH"] = "rbg-agent"  # on PATH in the sandbox
    return subprocess.run(
        [rbg, *args], env=e, capture_output=True, text=True, timeout=timeout
    )


@needs_env
def test_ping(env, tmp_path):
    sim, rbg = env
    res = run_rbg(sim, rbg, tmp_path / "ch", "ping")
    assert res.returncode == 0, res.stderr
    assert "reachable" in res.stdout


@needs_env
def test_launch_then_ls(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    launch = run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    assert launch.returncode == 0, launch.stderr
    assert "alpha" in launch.stdout

    ls = run_rbg(sim, rbg, ch, "ls")
    assert ls.returncode == 0, ls.stderr
    assert "alpha" in ls.stdout


@needs_env
def test_read_replays_seeded_transcript(env, tmp_path):
    import time
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    # launch spawns the claude child detached and returns immediately, so the
    # transcript may not be written yet; poll read until it appears.
    seen = ""
    for _ in range(20):
        read = run_rbg(sim, rbg, ch, "read", "alpha")
        seen = read.stdout
        if "user: say hello" in seen:
            break
        time.sleep(0.25)
    assert "user: say hello" in seen


@needs_env
def test_send_appends_and_read_shows_response(env, tmp_path):
    import time
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    send = run_rbg(sim, rbg, ch, "send", "alpha", "count to three")
    assert send.returncode == 0, send.stderr

    seen = ""
    for _ in range(20):
        seen = run_rbg(sim, rbg, ch, "read", "alpha").stdout
        if "assistant: ack: count to three" in seen:
            break
        time.sleep(0.25)
    assert "user: count to three" in seen
    assert "assistant: ack: count to three" in seen


@needs_env
def test_send_unknown_agent_fails(env, tmp_path):
    sim, rbg = env
    res = run_rbg(sim, rbg, tmp_path / "ch", "send", "ghost", "x")
    assert res.returncode != 0


@needs_env
def test_shell_metacharacters_in_task_are_quoted_not_executed(env, tmp_path):
    """The headline v2 safety property: a task with shell metacharacters must be
    passed through to claude as data, NOT executed by the desktop login shell.

    We launch with a task that, if the remote command were unquoted, would run
    `touch ~/PWNED` on the desktop. After the round-trip, that file must NOT
    exist in the sandbox HOME — proving sshx.RemoteCommand quoting holds across
    the real ssh + login-shell path.
    """
    sim, rbg = env
    ch = tmp_path / "ch"
    pwned = os.path.join(sim.home, "PWNED")
    malicious = "hello; touch ~/PWNED"
    res = run_rbg(sim, rbg, ch, "launch", "alpha", malicious)
    assert res.returncode == 0, res.stderr
    assert not os.path.exists(pwned), "injection ran: remote command was not quoted"
    # and the task survived intact as data in the transcript (launch is detached,
    # so poll read until the transcript is written)
    import time
    seen = ""
    for _ in range(20):
        seen = run_rbg(sim, rbg, ch, "read", "alpha").stdout
        if "touch ~/PWNED" in seen:
            break
        time.sleep(0.25)
    assert "touch ~/PWNED" in seen
