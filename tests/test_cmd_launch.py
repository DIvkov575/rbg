import io
import pytest
import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


def test_ping_reachable(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    assert rbg.cmd_ping(cfg(), runner=runner) == 0
    assert "desk: reachable" in capsys.readouterr().out


def test_ping_unreachable(capsys):
    runner = RecordingRunner(default=result(returncode=255))
    assert rbg.cmd_ping(cfg(), runner=runner) == 1
    assert "cannot reach 'desk' — disconnected" in capsys.readouterr().err


def test_launch_records_mapping():
    agents_json = '[{"name": "alpha", "sessionId": "sid-alpha"}]'
    runner = RecordingRunner(
        by_substring={"claude agents": result(stdout=agents_json)},
        default=result(returncode=0),
    )
    saved = {}
    sessions = {}

    rc = rbg.cmd_launch(
        cfg(), "alpha", "build it",
        runner=runner, sessions=sessions, save=lambda s: saved.update(s),
    )
    assert rc == 0
    assert saved == {"alpha": "sid-alpha"}
    # the launch command ran before the resolve query
    assert "claude --bg -n alpha 'build it'" in runner.remote_cmds[1]
    assert "claude agents --json --all" in runner.remote_cmds[2]


def test_launch_unresolved_id_errors(capsys):
    runner = RecordingRunner(
        by_substring={"claude agents": result(stdout="[]")},
        default=result(returncode=0),
    )
    rc = rbg.cmd_launch(
        cfg(), "alpha", "task",
        runner=runner, sessions={}, save=lambda s: None,
    )
    assert rc == 1
    assert "could not resolve" in capsys.readouterr().err
