import io
import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


TRANSCRIPT = (
    '{"message": {"role": "user", "content": "q"}}\n'
    '{"message": {"role": "assistant", "content": "a"}}\n'
)


def test_read_replay_renders(capsys):
    runner = RecordingRunner(
        by_substring={"tail": result(stdout=TRANSCRIPT)},
        default=result(returncode=0),
    )
    rc = rbg.cmd_read(cfg(), "alpha", follow=False, runner=runner,
                      sessions={"alpha": "sid-1"})
    assert rc == 0
    out = capsys.readouterr().out
    assert "user: q" in out
    assert "assistant: a" in out
    assert "tail -n +1 ~/.claude/projects/*/sid-1.jsonl" in runner.remote_cmds[1]


def test_read_unknown_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_read(cfg(), "ghost", runner=runner, sessions={})
    assert rc == 1
    assert "unknown agent 'ghost'" in capsys.readouterr().err


def test_read_follow_uses_popen():
    runner = RecordingRunner(default=result(returncode=0))
    captured = {}

    class FakeProc:
        stdout = iter(TRANSCRIPT.splitlines())

    def fake_popen(cmd):
        captured["cmd"] = cmd
        return FakeProc()

    buf = io.StringIO()
    rc = rbg.cmd_read(cfg(), "alpha", follow=True, runner=runner,
                      popen=fake_popen, sessions={"alpha": "sid-1"}, out=buf)
    assert rc == 0
    assert "tail -f -n +1 ~/.claude/projects/*/sid-1.jsonl" in captured["cmd"][-1]
    assert buf.getvalue() == "user: q\nassistant: a\n"
