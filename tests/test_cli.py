import subprocess
import sys
import rbg


def test_parser_launch():
    args = rbg.build_parser().parse_args(["launch", "alpha", "do the thing"])
    assert args.cmd == "launch"
    assert args.name == "alpha"
    assert args.task == "do the thing"


def test_parser_read_follow_flag():
    args = rbg.build_parser().parse_args(["read", "alpha", "-f"])
    assert args.cmd == "read"
    assert args.follow is True


def test_parser_ls_and_ping():
    assert rbg.build_parser().parse_args(["ls"]).cmd == "ls"
    assert rbg.build_parser().parse_args(["ping"]).cmd == "ping"


def test_main_missing_host_exits_nonzero():
    # Real subprocess, no ssh involved: missing RBG_HOST must fail fast.
    proc = subprocess.run(
        [sys.executable, "rbg.py", "ping"],
        capture_output=True, text=True,
        env={"PATH": "/usr/bin:/bin"},  # deliberately no RBG_HOST
    )
    assert proc.returncode != 0
    assert "RBG_HOST" in (proc.stdout + proc.stderr)
