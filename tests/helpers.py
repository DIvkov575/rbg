"""Shared stubs for rbg tests: a fake subprocess runner and result builder."""
import subprocess


def result(returncode=0, stdout="", stderr=""):
    return subprocess.CompletedProcess(
        args=[], returncode=returncode, stdout=stdout, stderr=stderr
    )


class RecordingRunner:
    """Stub for rbg's subprocess runner.

    Records every command. Returns a canned result chosen by the first
    `by_substring` key found in the remote command string (cmd[-1]),
    else `default`.
    """

    def __init__(self, by_substring=None, default=None):
        self.calls = []
        self.by_substring = by_substring or {}
        self.default = default if default is not None else result(0)

    def __call__(self, cmd, **kwargs):
        self.calls.append(cmd)
        remote = cmd[-1] if cmd else ""
        for sub, res in self.by_substring.items():
            if sub in remote:
                return res
        return self.default

    @property
    def remote_cmds(self):
        return [c[-1] for c in self.calls if c]
