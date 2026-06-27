import pytest
import rbg
from tests.helpers import RecordingRunner, result


def cfg(**kw):
    base = dict(host="desk", cwd="", ssh_opts=[])
    base.update(kw)
    return rbg.Config(**base)


def test_build_ssh_cmd_basic():
    assert rbg.build_ssh_cmd(cfg(), "true") == ["ssh", "desk", "true"]


def test_build_ssh_cmd_with_opts_and_tty():
    c = cfg(ssh_opts=["-p", "2222"])
    assert rbg.build_ssh_cmd(c, "claude", tty=True) == [
        "ssh", "-t", "-p", "2222", "desk", "claude"
    ]


def test_build_ssh_cmd_batch_gate():
    assert rbg.build_ssh_cmd(cfg(), "true", batch=True) == [
        "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "desk", "true"
    ]


def test_check_reachable_true():
    runner = RecordingRunner(default=result(returncode=0))
    assert rbg.check_reachable(cfg(), runner=runner) is True
    assert runner.calls[0] == [
        "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "desk", "true"
    ]


def test_check_reachable_false():
    runner = RecordingRunner(default=result(returncode=255))
    assert rbg.check_reachable(cfg(), runner=runner) is False


def test_ensure_reachable_exits_when_down(capsys):
    runner = RecordingRunner(default=result(returncode=255))
    with pytest.raises(SystemExit) as e:
        rbg.ensure_reachable(cfg(), runner=runner)
    assert e.value.code == 1
    assert "cannot reach 'desk' — disconnected" in capsys.readouterr().err
