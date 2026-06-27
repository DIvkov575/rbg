#!/usr/bin/env python3
"""rbg — manage remote Claude --bg agents from the laptop."""
import json
import os
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
