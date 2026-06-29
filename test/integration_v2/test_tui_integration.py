"""Integration coverage for the TUI's data path and auto-naming, over real SSH.

The interactive raw-terminal loop needs a PTY and is not driven here; instead we
assert the cross-SSH behaviors the dashboard relies on: auto-derived names on
launch, dedup, and correct ls/read data the dashboard consumes.
"""
import json
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
    e["RBG_AGENT_PATH"] = "rbg-agent"
    return subprocess.run([rbg, *args], env=e, capture_output=True, text=True, timeout=timeout)


@needs_env
def test_launch_without_name_autoderives_slug(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    res = run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    assert res.returncode == 0, res.stderr
    assert "fix-flaky-test" in res.stdout


@needs_env
def test_two_unnamed_launches_dedup(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    run_rbg(sim, rbg, ch, "launch", "fix the flaky test")
    ls = run_rbg(sim, rbg, ch, "ls")
    assert ls.returncode == 0, ls.stderr
    names = [s["name"] for s in json.loads(ls.stdout)]
    assert "fix-flaky-test" in names
    assert "fix-flaky-test-2" in names


@needs_env
def test_ls_json_is_dashboard_consumable(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "say hello")
    ls = run_rbg(sim, rbg, ch, "ls")
    data = json.loads(ls.stdout)
    assert isinstance(data, list) and data
    assert "name" in data[0] and "claudeSessionId" in data[0]
