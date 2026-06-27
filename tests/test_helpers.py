from tests.helpers import RecordingRunner, result


def test_recording_runner_records_and_returns_default():
    runner = RecordingRunner(default=result(returncode=0, stdout="ok"))
    out = runner(["ssh", "host", "true"])
    assert out.returncode == 0
    assert out.stdout == "ok"
    assert runner.calls == [["ssh", "host", "true"]]
    assert runner.remote_cmds == ["true"]


def test_recording_runner_matches_by_substring():
    runner = RecordingRunner(by_substring={"agents": result(stdout="[]")})
    assert runner(["ssh", "host", "claude agents --json"]).stdout == "[]"
    assert runner(["ssh", "host", "true"]).stdout == ""
