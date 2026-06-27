import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


def test_ls_lists_agents_with_local_names(capsys, monkeypatch):
    monkeypatch.setattr(rbg, "load_sessions", lambda: {"alpha": "sid-a"})
    agents_json = (
        '[{"name": "alpha", "sessionId": "sid-a"},'
        ' {"name": "beta", "sessionId": "sid-b"}]'
    )
    runner = RecordingRunner(
        by_substring={"claude agents": result(stdout=agents_json)},
        default=result(returncode=0),
    )
    rc = rbg.cmd_ls(cfg(), runner=runner)
    assert rc == 0
    out = capsys.readouterr().out
    assert "alpha\tsid-a\talpha" in out
    assert "beta\tsid-b" in out


def test_attach_known_agent_uses_tty():
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_attach(cfg(), "alpha", runner=runner,
                        sessions={"alpha": "sid-1"})
    assert rc == 0
    attach_call = runner.calls[1]
    assert "-t" in attach_call
    assert "claude --resume sid-1" in attach_call[-1]


def test_attach_unknown_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_attach(cfg(), "ghost", runner=runner, sessions={})
    assert rc == 1
    assert "unknown agent 'ghost'" in capsys.readouterr().err
