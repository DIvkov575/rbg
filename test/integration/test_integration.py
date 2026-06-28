"""End-to-end integration tests for rbg against a simulated SSH desktop.

These drive the real `rbg.py` CLI as a subprocess over a real (non-root,
sandboxed) sshd, using a fake `claude` shim on the remote side. They exercise
the actual ssh/tmux/tail machinery — not mocks — so they cover everything the
49 unit tests deliberately stub out.

Marked `integration` so they're opt-in:  pytest -m integration
They are skipped automatically if /usr/sbin/sshd or tmux is unavailable.

What they DON'T prove: that the real `claude` emits this JSON shape or that
`-p --resume` appends rather than forks. Those are assumptions baked into the
fake shim; confirming them needs a real claude (plan Task 2).
"""
import os
import subprocess
import sys

import pytest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
RBG = os.path.join(REPO_ROOT, "rbg.py")

sys.path.insert(0, HERE)
from sshd_harness import SimDesktop, SSHD_BIN, _find_tmux  # noqa: E402

pytestmark = pytest.mark.integration

_REASON = None
if not os.path.exists(SSHD_BIN):
    _REASON = f"{SSHD_BIN} not available"
elif _find_tmux() is None:
    _REASON = "tmux not available"
needs_env = pytest.mark.skipif(_REASON is not None, reason=_REASON or "")


@pytest.fixture
def desktop(tmp_path):
    """A running sandboxed sim desktop, torn down after the test."""
    sim = SimDesktop(str(tmp_path / "sim"))
    sim.start()
    yield sim
    sim.stop()


def run_rbg(desktop, client_home, *args, cwd="", timeout=30):
    """Invoke rbg.py against the sim desktop with a sandboxed client HOME."""
    env = desktop.rbg_env(cwd=cwd)
    env["HOME"] = str(client_home)  # isolate ~/.rbg/sessions.json
    return subprocess.run(
        [sys.executable, RBG, *args],
        env=env, capture_output=True, text=True, timeout=timeout,
    )


@needs_env
def test_ping_reaches_sim_desktop(desktop, tmp_path):
    res = run_rbg(desktop, tmp_path / "chome", "ping")
    assert res.returncode == 0, res.stderr
    assert "reachable" in res.stdout


@needs_env
def test_ping_unreachable_host_fails(tmp_path):
    # No desktop running on this port → gate must fail with the exact message.
    sim = SimDesktop(str(tmp_path / "sim"))
    sim.start()
    port = sim.port
    sim.stop()  # now nothing is listening on `port`
    env = dict(os.environ)
    env["HOME"] = str(tmp_path / "chome")
    env["RBG_HOST"] = "127.0.0.1"
    env["RBG_SSH"] = (
        f"-p {port} -i {sim.client_key} "
        "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
    )
    res = subprocess.run(
        [sys.executable, RBG, "ls"],
        env=env, capture_output=True, text=True, timeout=30,
    )
    assert res.returncode == 1
    assert "cannot reach '127.0.0.1' — disconnected" in res.stderr


@needs_env
def test_launch_then_ls_shows_agent(desktop, tmp_path):
    chome = tmp_path / "chome"
    launch = run_rbg(desktop, chome, "launch", "alpha", "say hello")
    assert launch.returncode == 0, launch.stderr
    assert "alpha" in launch.stdout

    ls = run_rbg(desktop, chome, "ls")
    assert ls.returncode == 0, ls.stderr
    # ls prints "<name>\t<id>\t<localname>"; alpha was just launched & recorded.
    assert "alpha" in ls.stdout
    assert "sid-1" in ls.stdout


@needs_env
def test_launch_records_local_session_map(desktop, tmp_path):
    chome = tmp_path / "chome"
    run_rbg(desktop, chome, "launch", "alpha", "task one")
    sessions_file = chome / ".rbg" / "sessions.json"
    assert sessions_file.exists()
    import json
    mapping = json.loads(sessions_file.read_text())
    assert mapping == {"alpha": "sid-1"}


@needs_env
def test_read_replays_seeded_transcript(desktop, tmp_path):
    chome = tmp_path / "chome"
    run_rbg(desktop, chome, "launch", "alpha", "say hello")
    read = run_rbg(desktop, chome, "read", "alpha")
    assert read.returncode == 0, read.stderr
    # launch seeds one user line with the task.
    assert "user: say hello" in read.stdout


@needs_env
def test_send_appends_and_read_shows_response(desktop, tmp_path):
    chome = tmp_path / "chome"
    run_rbg(desktop, chome, "launch", "alpha", "say hello")

    send = run_rbg(desktop, chome, "send", "alpha", "count to three")
    assert send.returncode == 0, send.stderr
    assert "sent to alpha" in send.stdout

    # The tmux window runs the fake claude, which appends to the transcript.
    # Poll read until the appended assistant line shows up.
    import time
    seen = ""
    for _ in range(20):
        read = run_rbg(desktop, chome, "read", "alpha")
        seen = read.stdout
        if "assistant: ack: count to three" in seen:
            break
        time.sleep(0.25)
    assert "user: count to three" in seen
    assert "assistant: ack: count to three" in seen


@needs_env
def test_send_to_unknown_agent_fails(desktop, tmp_path):
    res = run_rbg(desktop, tmp_path / "chome", "send", "ghost", "x")
    assert res.returncode == 1
    assert "unknown agent 'ghost'" in res.stderr


@needs_env
def test_two_agents_have_distinct_sessions(desktop, tmp_path):
    chome = tmp_path / "chome"
    run_rbg(desktop, chome, "launch", "alpha", "first")
    run_rbg(desktop, chome, "launch", "beta", "second")
    import json
    mapping = json.loads((chome / ".rbg" / "sessions.json").read_text())
    assert mapping == {"alpha": "sid-1", "beta": "sid-2"}
    # Each transcript replays only its own task.
    a = run_rbg(desktop, chome, "read", "alpha").stdout
    b = run_rbg(desktop, chome, "read", "beta").stdout
    assert "user: first" in a and "user: second" not in a
    assert "user: second" in b and "user: first" not in b
