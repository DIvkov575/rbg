#!/usr/bin/env python3
"""rbg — manage remote Claude --bg agents from the laptop."""
import json
import os
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass, field


class ConfigError(Exception):
    pass


@dataclass
class Config:
    host: str
    cwd: str = ""
    ssh_opts: list = field(default_factory=list)


def _read_conf_file(path):
    vals = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, _, value = line.partition("=")
                vals[key.strip()] = value.strip().strip('"').strip("'")
    except FileNotFoundError:
        pass
    return vals


def load_config(env=None, conf_path=None):
    env = os.environ if env is None else env
    conf_path = conf_path or os.path.expanduser("~/.rbg.conf")
    file_vals = _read_conf_file(conf_path)

    def get(key):
        return env.get(key) if env.get(key) is not None else file_vals.get(key, "")

    host = get("RBG_HOST")
    if not host:
        raise ConfigError("RBG_HOST not set (export it or put it in ~/.rbg.conf)")
    return Config(host=host, cwd=get("RBG_CWD"), ssh_opts=shlex.split(get("RBG_SSH")))


def _default_runner(cmd, **kwargs):
    kwargs.setdefault("capture_output", True)
    kwargs.setdefault("text", True)
    return subprocess.run(cmd, **kwargs)


def build_ssh_cmd(config, remote_cmd, tty=False, batch=False):
    cmd = ["ssh"]
    if batch:
        cmd += ["-o", "BatchMode=yes", "-o", "ConnectTimeout=5"]
    if tty:
        cmd += ["-t"]
    cmd += config.ssh_opts
    cmd += [config.host, remote_cmd]
    return cmd


def check_reachable(config, runner=None):
    runner = runner or _default_runner
    return runner(build_ssh_cmd(config, "true", batch=True)).returncode == 0


def ensure_reachable(config, runner=None):
    if not check_reachable(config, runner=runner):
        print(f"cannot reach '{config.host}' — disconnected", file=sys.stderr)
        sys.exit(1)


def sessions_path():
    return os.path.expanduser("~/.rbg/sessions.json")


def load_sessions(path=None):
    path = path or sessions_path()
    try:
        with open(path) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def save_sessions(sessions, path=None):
    path = path or sessions_path()
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        json.dump(sessions, f, indent=2)


ID_KEYS = ("sessionId", "session_id", "id")


def parse_agents(text):
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    if isinstance(data, dict):
        data = data.get("agents", [])
    return data if isinstance(data, list) else []


def find_agent_id(agents, name):
    for agent in agents:
        if isinstance(agent, dict) and agent.get("name") == name:
            for key in ID_KEYS:
                if agent.get(key):
                    return agent[key]
    return None


def render_line(line):
    line = line.strip()
    if not line:
        return None
    try:
        obj = json.loads(line)
    except json.JSONDecodeError:
        return None
    message = obj.get("message") or {}
    content = message.get("content")
    parts = []
    if isinstance(content, str):
        parts.append(content)
    elif isinstance(content, list):
        for block in content:
            if not isinstance(block, dict):
                continue
            btype = block.get("type")
            if btype == "text":
                parts.append(block.get("text", ""))
            elif btype == "tool_use":
                parts.append(f"[tool: {block.get('name', '?')}]")
            elif btype == "tool_result":
                parts.append("[tool result]")
    text = "\n".join(p for p in parts if p)
    if not text:
        return None
    role = message.get("role") or obj.get("type") or "?"
    return f"{role}: {text}"


def render_stream(lines, out=None):
    out = out or sys.stdout
    for line in lines:
        rendered = render_line(line)
        if rendered is not None:
            print(rendered, file=out)


def _cd_prefix(cwd):
    return f"cd {shlex.quote(cwd)} && " if cwd else ""


def remote_launch_cmd(cwd, name, task):
    return f"{_cd_prefix(cwd)}claude --bg -n {shlex.quote(name)} {shlex.quote(task)}"


def remote_send_cmd(cwd, name, session_id, task):
    inner = (
        f"{_cd_prefix(cwd)}claude -p {shlex.quote(task)} "
        f"--resume {shlex.quote(session_id)} --output-format stream-json"
    )
    win = shlex.quote(name)
    return (
        "tmux has-session -t rbg 2>/dev/null || tmux new-session -d -s rbg; "
        f"if tmux list-windows -t rbg -F '#W' 2>/dev/null | grep -qx {win}; "
        "then echo 'rbg: busy' >&2; exit 3; fi; "
        f"tmux new-window -t rbg -n {win} {shlex.quote(inner)}"
    )


def remote_read_cmd(session_id, follow=False):
    if not re.fullmatch(r"[A-Za-z0-9-]+", session_id or ""):
        raise ValueError(f"unsafe session id: {session_id!r}")
    flag = "-f " if follow else ""
    return f"tail {flag}-n +1 ~/.claude/projects/*/{session_id}.jsonl 2>/dev/null"


def remote_attach_cmd(cwd, session_id):
    return f"{_cd_prefix(cwd)}claude --resume {shlex.quote(session_id)}"


def cmd_ping(config, runner=None):
    if check_reachable(config, runner=runner):
        print(f"{config.host}: reachable")
        return 0
    print(f"cannot reach '{config.host}' — disconnected", file=sys.stderr)
    return 1


def cmd_launch(config, name, task, runner=None, sessions=None, save=None):
    runner = runner or _default_runner
    ensure_reachable(config, runner=runner)
    runner(build_ssh_cmd(config, remote_launch_cmd(config.cwd, name, task)))
    listing = runner(build_ssh_cmd(config, "claude agents --json --all"))
    session_id = find_agent_id(parse_agents(listing.stdout), name)
    if not session_id:
        print(
            f"rbg: launched '{name}' but could not resolve its id from "
            "'claude agents --json --all'",
            file=sys.stderr,
        )
        return 1
    sessions = load_sessions() if sessions is None else sessions
    sessions[name] = session_id
    (save or save_sessions)(sessions)
    print(f"launched {name} ({session_id})")
    return 0


def cmd_send(config, name, task, runner=None, sessions=None):
    runner = runner or _default_runner
    ensure_reachable(config, runner=runner)
    sessions = load_sessions() if sessions is None else sessions
    session_id = sessions.get(name)
    if not session_id:
        print(f"rbg: unknown agent '{name}'", file=sys.stderr)
        return 1
    res = runner(
        build_ssh_cmd(config, remote_send_cmd(config.cwd, name, session_id, task))
    )
    if res.returncode == 3:
        print(f"rbg: session '{name}' busy — a send is already running", file=sys.stderr)
        return 3
    print(f"sent to {name}")
    return 0


def cmd_read(config, name, follow=False, runner=None, popen=None,
             sessions=None, out=None):
    runner = runner or _default_runner
    out = out or sys.stdout
    ensure_reachable(config, runner=runner)
    sessions = load_sessions() if sessions is None else sessions
    session_id = sessions.get(name)
    if not session_id:
        print(f"rbg: unknown agent '{name}'", file=sys.stderr)
        return 1
    cmd = build_ssh_cmd(config, remote_read_cmd(session_id, follow=follow))
    if follow:
        popen = popen or (
            lambda c: subprocess.Popen(c, stdout=subprocess.PIPE, text=True)
        )
        proc = popen(cmd)
        try:
            render_stream(proc.stdout, out=out)
        except KeyboardInterrupt:
            pass  # Ctrl-C stops the local tail only; remote task untouched
        return 0
    res = runner(cmd)
    render_stream(res.stdout.splitlines(), out=out)
    return 0


def cmd_ls(config, runner=None, out=None):
    runner = runner or _default_runner
    out = out or sys.stdout
    ensure_reachable(config, runner=runner)
    listing = runner(build_ssh_cmd(config, "claude agents --json --all"))
    agents = parse_agents(listing.stdout)
    by_id = {sid: name for name, sid in load_sessions().items()}
    for agent in agents:
        if not isinstance(agent, dict):
            continue
        session_id = next((agent[k] for k in ID_KEYS if agent.get(k)), "?")
        local = by_id.get(session_id, "")
        name = agent.get("name", "?")
        print(f"{name}\t{session_id}\t{local}".rstrip(), file=out)
    return 0


def cmd_attach(config, name, runner=None, sessions=None):
    runner = runner or _default_runner
    ensure_reachable(config, runner=runner)
    sessions = load_sessions() if sessions is None else sessions
    session_id = sessions.get(name)
    if not session_id:
        print(f"rbg: unknown agent '{name}'", file=sys.stderr)
        return 1
    cmd = build_ssh_cmd(config, remote_attach_cmd(config.cwd, session_id), tty=True)
    return runner(cmd).returncode


def build_parser():
    import argparse

    parser = argparse.ArgumentParser(
        prog="rbg", description="Manage remote Claude --bg agents from the laptop."
    )
    sub = parser.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("launch", help="start a named --bg agent on the desktop")
    p.add_argument("name")
    p.add_argument("task")

    p = sub.add_parser("send", help="send a follow-up task to an ongoing session")
    p.add_argument("name")
    p.add_argument("task")

    p = sub.add_parser("read", help="replay or follow an agent's transcript")
    p.add_argument("name")
    p.add_argument("-f", "--follow", action="store_true", help="stream live")

    sub.add_parser("ls", help="list remote agents")

    p = sub.add_parser("attach", help="attach interactively over a tty")
    p.add_argument("name")

    sub.add_parser("ping", help="check desktop reachability")
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        config = load_config()
    except ConfigError as e:
        print(f"rbg: {e}", file=sys.stderr)
        return 2

    if args.cmd == "ping":
        return cmd_ping(config)
    if args.cmd == "launch":
        return cmd_launch(config, args.name, args.task)
    if args.cmd == "send":
        return cmd_send(config, args.name, args.task)
    if args.cmd == "read":
        return cmd_read(config, args.name, follow=args.follow)
    if args.cmd == "ls":
        return cmd_ls(config)
    if args.cmd == "attach":
        return cmd_attach(config, args.name)
    return 2


if __name__ == "__main__":
    sys.exit(main())
