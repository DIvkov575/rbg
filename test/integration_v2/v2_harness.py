"""Boot a throwaway, non-root sshd that simulates the rbg v2 "desktop".

`SimDesktop` stands up a user-space OpenSSH daemon on a random high port,
authenticated by a generated key, with a sandboxed environment:

  - PATH points at a bin/ dir holding a `claude` shim (wrapping fake_claude.py)
    and the prebuilt `rbg-agent` binary (installed via install_agent()).
  - HOME points at a throwaway dir, so the fake claude's ~/.claude transcripts
    and the agent's ~/.rbg-agent state never touch the real home.

Unlike v1 there is NO tmux: the Go agent detaches its own child (setsid) and
serializes with flock, so the harness needs no tmux symlink and no TMUX_TMPDIR
socket-length workaround.

No root, no Docker: just `/usr/sbin/sshd -f <config>` on loopback.
"""
import os
import shutil
import socket
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
FAKE_CLAUDE = os.path.join(HERE, "fake_claude.py")
SSHD_BIN = "/usr/sbin/sshd"


def _free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class SimDesktop:
    """A sandboxed sshd instance that looks like the remote dev desktop."""

    def __init__(self, root):
        self.root = os.path.abspath(root)
        self.host = "127.0.0.1"
        self.port = _free_port()
        self.proc = None
        self.bin_dir = os.path.join(self.root, "bin")
        self.home = os.path.join(self.root, "home")
        self.client_key = os.path.join(self.root, "client")
        self._config = os.path.join(self.root, "sshd_config")
        self._log = os.path.join(self.root, "sshd.log")
        self._pid = os.path.join(self.root, "sshd.pid")

    # -- setup ----------------------------------------------------------------

    def _keygen(self):
        for name in ("host_ed25519", "client"):
            path = os.path.join(self.root, name)
            subprocess.run(
                ["ssh-keygen", "-q", "-t", "ed25519", "-f", path, "-N", "",
                 "-C", name],
                check=True, capture_output=True,
            )
        authorized = os.path.join(self.root, "authorized_keys")
        shutil.copy(self.client_key + ".pub", authorized)
        os.chmod(authorized, 0o600)
        return authorized

    def _make_bin(self):
        os.makedirs(self.bin_dir, exist_ok=True)
        # claude shim → fake_claude.py with the current interpreter.
        shim = os.path.join(self.bin_dir, "claude")
        with open(shim, "w") as f:
            f.write(f'#!/bin/sh\nexec "{sys.executable}" "{FAKE_CLAUDE}" "$@"\n')
        os.chmod(shim, 0o755)

    def install_agent(self, agent_binary):
        """Copy a prebuilt rbg-agent into the sandbox PATH."""
        dest = os.path.join(self.bin_dir, "rbg-agent")
        shutil.copy(agent_binary, dest)
        os.chmod(dest, 0o755)

    def _write_config(self, authorized):
        os.makedirs(self.home, exist_ok=True)
        path_env = f"{self.bin_dir}:/usr/bin:/bin"
        with open(self._config, "w") as f:
            f.write(
                f"Port {self.port}\n"
                f"ListenAddress {self.host}\n"
                f"HostKey {os.path.join(self.root, 'host_ed25519')}\n"
                f"PidFile {self._pid}\n"
                f"AuthorizedKeysFile {authorized}\n"
                "PasswordAuthentication no\n"
                "ChallengeResponseAuthentication no\n"
                "UsePAM no\n"
                "StrictModes no\n"
                "LogLevel ERROR\n"
                f"SetEnv PATH={path_env} HOME={self.home}\n"
            )

    # -- lifecycle ------------------------------------------------------------

    def start(self):
        os.makedirs(self.root, exist_ok=True)
        authorized = self._keygen()
        self._make_bin()
        self._write_config(authorized)
        self.proc = subprocess.Popen(
            [SSHD_BIN, "-f", self._config, "-E", self._log],
        )
        self._wait_until_accepting()
        return self

    def _wait_until_accepting(self, timeout=10.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                with socket.create_connection((self.host, self.port), timeout=1):
                    return
            except OSError:
                time.sleep(0.1)
        raise RuntimeError(
            f"sshd did not start accepting on {self.host}:{self.port}; "
            f"log:\n{self._read_log()}"
        )

    def _read_log(self):
        try:
            with open(self._log) as f:
                return f.read()
        except FileNotFoundError:
            return "(no log)"

    def stop(self):
        try:
            with open(self._pid) as f:
                pid = int(f.read().strip())
            os.kill(pid, 15)
        except (FileNotFoundError, ValueError, ProcessLookupError):
            pass
        if self.proc:
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()

    # -- client config --------------------------------------------------------

    def ssh_opts(self):
        """The RBG_SSH option string a client needs to reach this sim desktop."""
        return (
            f"-p {self.port} -i {self.client_key} "
            "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
        )

    def rbg_env(self, cwd=""):
        """Environment dict for invoking the rbg client against this sim desktop."""
        env = dict(os.environ)
        env["RBG_HOST"] = self.host
        env["RBG_SSH"] = self.ssh_opts()
        if cwd:
            env["RBG_CWD"] = cwd
        return env

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.stop()
        return False
