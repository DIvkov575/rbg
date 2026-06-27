import pytest
import rbg


def test_launch_cmd_with_cwd_quotes_task():
    cmd = rbg.remote_launch_cmd("/proj dir", "alpha", "do; rm -rf /")
    assert cmd == "cd '/proj dir' && claude --bg -n alpha 'do; rm -rf /'"


def test_launch_cmd_without_cwd():
    cmd = rbg.remote_launch_cmd("", "alpha", "task")
    assert cmd == "claude --bg -n alpha task"


def test_send_cmd_structure():
    cmd = rbg.remote_send_cmd("/proj", "alpha", "sid-1", "next step")
    # ensures session, busy-rejects with exit 3, then opens a named window
    assert "tmux has-session -t rbg" in cmd
    assert "new-session -d -s rbg" in cmd
    assert "list-windows -t rbg" in cmd
    assert "exit 3" in cmd
    assert "new-window -t rbg -n alpha" in cmd
    # the inner claude command is shlex.quote'd as a single tmux argument, so the
    # task's own quoting is escaped; assert on the parts that survive intact
    assert "claude -p " in cmd
    assert "--resume sid-1 --output-format stream-json" in cmd
    assert "cd /proj" in cmd


def test_read_cmd_replay_and_follow():
    assert rbg.remote_read_cmd("sid-1", follow=False) == (
        "tail -n +1 ~/.claude/projects/*/sid-1.jsonl 2>/dev/null"
    )
    assert rbg.remote_read_cmd("sid-1", follow=True) == (
        "tail -f -n +1 ~/.claude/projects/*/sid-1.jsonl 2>/dev/null"
    )


def test_read_cmd_rejects_unsafe_session_id():
    for bad in ["../etc/passwd", "a b", "id;rm -rf /", "id*", "id$x", ""]:
        with pytest.raises(ValueError):
            rbg.remote_read_cmd(bad, follow=False)


def test_attach_cmd():
    assert rbg.remote_attach_cmd("/proj", "sid-1") == (
        "cd /proj && claude --resume sid-1"
    )
    assert rbg.remote_attach_cmd("", "sid-1") == "claude --resume sid-1"
