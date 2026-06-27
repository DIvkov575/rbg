import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


def test_send_known_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_send(cfg(), "alpha", "next", runner=runner,
                      sessions={"alpha": "sid-1"})
    assert rc == 0
    assert "sent to alpha" in capsys.readouterr().out
    assert "new-window -t rbg -n alpha" in runner.remote_cmds[1]


def test_send_unknown_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_send(cfg(), "ghost", "x", runner=runner, sessions={})
    assert rc == 1
    assert "unknown agent 'ghost'" in capsys.readouterr().err


def test_send_busy_session(capsys):
    # Only the tmux send command returns 3; the reachability `true` probe must
    # return 0 (default) or ensure_reachable would exit 1 before the busy check.
    runner = RecordingRunner(
        by_substring={"new-window": result(returncode=3)},
        default=result(returncode=0),
    )
    rc = rbg.cmd_send(cfg(), "alpha", "x", runner=runner,
                      sessions={"alpha": "sid-1"})
    assert rc == 3
    assert "busy" in capsys.readouterr().err
