# `rbg` Remote Claude Agent CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `rbg`, a single laptop-side Python 3 CLI that launches, sends tasks to, reads output from, lists, and attaches to remote Claude `--bg` agents over SSH.

**Architecture:** One stdlib-only Python module (`rbg.py`) exposing small testable functions: config loading, SSH command construction, a connection gate, a local name→sessionId map, JSON parsing for the `claude agents` listing and the JSONL transcript, and one `cmd_*` function per verb. Every function that touches the network takes an injectable `runner` (default wraps `subprocess.run`) so the whole CLI is unit-testable without SSH. Remote side uses `claude --bg`, headless `claude -p --resume` inside a per-session tmux window, and `tail` of the transcript file.

**Tech Stack:** Python 3 (stdlib only: `argparse`, `json`, `subprocess`, `shlex`, `os`, `sys`, `dataclasses`), `pytest` for tests. Remote dependencies: `ssh`, `tmux`, `claude` CLI (v2.1.187+).

---

## Design Reference (read before starting)

**Source spec:** `docs/HLD-remote-agent-management.md`.

**Config** (env overrides `~/.rbg.conf`, `KEY=value` lines):
- `RBG_HOST` (required) — desktop hostname.
- `RBG_CWD` — remote project dir; commands `cd` into it before running `claude`.
- `RBG_SSH` — extra ssh options, space-separated (parsed with `shlex.split`).

**Local state:** `~/.rbg/sessions.json` — a flat `{ "<name>": "<sessionId>" }` map.

**Remote command contracts:**
- launch: `cd <cwd> && claude --bg -n <name> <task>`
- list / resolve id: `claude agents --json --all`
- send: a tmux one-liner that ensures a detached `rbg` session exists, rejects (exit 3) if a window named `<name>` is already open, else opens a new window running `cd <cwd> && claude -p <task> --resume <id> --output-format stream-json`
- read: `tail [-f] -n +1 ~/.claude/projects/*/<id>.jsonl` (glob avoids deriving the project slug)
- attach: `cd <cwd> && claude --resume <id>` over `ssh -t`

**ASSUMED, verify in Task 2:** the JSON shape of `claude agents --json --all`. This plan assumes a top-level array (or `{"agents": [...]}`) of objects each having a `"name"` and one of `"sessionId" | "session_id" | "id"`. The parser is written defensively to tolerate this being slightly different; Task 2 confirms it on the real desktop and the only change needed if it differs is the `ID_KEYS` tuple / `parse_agents` unwrap key.

**Connection gate:** every networked verb calls `ensure_reachable()` first, which runs `ssh -o BatchMode=yes -o ConnectTimeout=5 <host> true`; on failure prints `cannot reach '<host>' — disconnected` to stderr and exits 1.

---

## Task 1: Project skeleton + shared test helpers

**Files:**
- Create: `rbg.py`
- Create: `conftest.py`
- Create: `tests/__init__.py`
- Create: `tests/helpers.py`
- Create: `tests/test_helpers.py`

- [ ] **Step 1: Initialize git and create the empty module + package files**

```bash
cd /Users/divkov/workplace/remote-ccbg
git init -q 2>/dev/null || true
mkdir -p tests
```

Create `rbg.py` with exactly this content:

```python
#!/usr/bin/env python3
"""rbg — manage remote Claude --bg agents from the laptop."""
import json
import os
import shlex
import subprocess
import sys
from dataclasses import dataclass, field
```

Create `conftest.py` (empty file — its presence puts the repo root on `sys.path` so `import rbg` works):

```python
# Present so pytest adds the repo root to sys.path and `import rbg` resolves.
```

Create `tests/__init__.py` (empty):

```python
```

- [ ] **Step 2: Write the failing test for the shared test helpers**

Create `tests/helpers.py`:

```python
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
```

Create `tests/test_helpers.py`:

```python
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
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_helpers.py -v`
Expected: 2 passed.

- [ ] **Step 4: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py conftest.py tests/__init__.py tests/helpers.py tests/test_helpers.py
git commit -m "chore: project skeleton and shared test helpers"
```

---

## Task 2: Verify the `claude agents --json` shape on the desktop

**Files:** none (verification + a note).

This is the one fact the HLD flags as unconfirmed. Do it before building parsing on top of an assumption.

- [ ] **Step 1: Inspect the real JSON on the desktop**

Run (substitute your desktop host; if `RBG_HOST` is already set you can `ssh "$RBG_HOST"`):

```bash
ssh <desktop-host> 'claude agents --json --all' | head -c 2000
```

Expected: a JSON array, or an object wrapping an array. Note (a) whether it is a bare array `[...]` or wrapped like `{"agents": [...]}`, and (b) the exact key holding the session id (`sessionId`, `session_id`, or `id`) and the key holding the friendly name.

- [ ] **Step 2: Record the finding**

If it matches the assumption in the Design Reference, write one line at the bottom of `docs/HLD-remote-agent-management.md` under "5.4 Verified facts": ``- `claude agents --json --all` shape confirmed: <bare array | agents-wrapped>, id key `<key>`, name key `<key>`.``

If it differs, note the real keys here and adjust `ID_KEYS` and `parse_agents`/`find_agent_id` in Task 6 accordingly (the parsing functions are the only code that depends on this).

- [ ] **Step 3: Commit the note**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add docs/HLD-remote-agent-management.md
git commit -m "docs: record verified claude agents --json shape"
```

---

## Task 3: Config loading

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_config.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_config.py`:

```python
import pytest
import rbg


def test_env_overrides_file(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('RBG_HOST=fromfile\nRBG_CWD=/proj\n')
    cfg = rbg.load_config(env={"RBG_HOST": "fromenv"}, conf_path=str(conf))
    assert cfg.host == "fromenv"      # env wins
    assert cfg.cwd == "/proj"         # file fills the gap


def test_ssh_opts_split(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('RBG_HOST=h\nRBG_SSH=-p 2222 -i ~/k\n')
    cfg = rbg.load_config(env={}, conf_path=str(conf))
    assert cfg.ssh_opts == ["-p", "2222", "-i", "~/k"]


def test_missing_host_raises(tmp_path):
    conf = tmp_path / "nope.conf"
    with pytest.raises(rbg.ConfigError):
        rbg.load_config(env={}, conf_path=str(conf))


def test_quoted_values_and_comments(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('# comment\nRBG_HOST="quoted-host"\n\n')
    cfg = rbg.load_config(env={}, conf_path=str(conf))
    assert cfg.host == "quoted-host"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_config.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'load_config'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py` (after the imports):

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_config.py -v`
Expected: 4 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_config.py
git commit -m "feat: config loading from env and ~/.rbg.conf"
```

---

## Task 4: SSH command building + connection gate

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_ssh.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_ssh.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_ssh.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'build_ssh_cmd'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_ssh.py -v`
Expected: 6 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_ssh.py
git commit -m "feat: ssh command builder and connection gate"
```

---

## Task 5: Local session map (`~/.rbg/sessions.json`)

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_sessions.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_sessions.py`:

```python
import rbg


def test_load_missing_returns_empty(tmp_path):
    assert rbg.load_sessions(path=str(tmp_path / "none.json")) == {}


def test_load_corrupt_returns_empty(tmp_path):
    p = tmp_path / "sessions.json"
    p.write_text("{not json")
    assert rbg.load_sessions(path=str(p)) == {}


def test_save_then_load_roundtrip(tmp_path):
    p = tmp_path / "sub" / "sessions.json"  # parent dir does not exist yet
    rbg.save_sessions({"alpha": "id-1"}, path=str(p))
    assert rbg.load_sessions(path=str(p)) == {"alpha": "id-1"}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_sessions.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'load_sessions'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_sessions.py -v`
Expected: 3 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_sessions.py
git commit -m "feat: local name->sessionId map persistence"
```

---

## Task 6: Parse `claude agents --json` and resolve id by name

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_agents_parse.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_agents_parse.py`:

```python
import rbg


def test_parse_bare_array():
    agents = rbg.parse_agents('[{"name": "a", "sessionId": "x"}]')
    assert agents == [{"name": "a", "sessionId": "x"}]


def test_parse_agents_wrapped_object():
    agents = rbg.parse_agents('{"agents": [{"name": "a", "id": "x"}]}')
    assert agents == [{"name": "a", "id": "x"}]


def test_parse_empty_or_garbage():
    assert rbg.parse_agents("") == []
    assert rbg.parse_agents("not json") == []


def test_find_agent_id_prefers_known_keys():
    agents = [
        {"name": "alpha", "session_id": "sid-alpha"},
        {"name": "beta", "id": "sid-beta"},
    ]
    assert rbg.find_agent_id(agents, "alpha") == "sid-alpha"
    assert rbg.find_agent_id(agents, "beta") == "sid-beta"


def test_find_agent_id_missing_returns_none():
    assert rbg.find_agent_id([{"name": "alpha", "id": "x"}], "ghost") is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_agents_parse.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'parse_agents'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py` (adjust `ID_KEYS` / the unwrap key here if Task 2 found a different shape):

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_agents_parse.py -v`
Expected: 5 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_agents_parse.py
git commit -m "feat: parse claude agents json and resolve id by name"
```

---

## Task 7: Render transcript JSONL lines

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_render.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_render.py`:

```python
import io
import rbg


def test_render_string_content():
    line = '{"type": "user", "message": {"role": "user", "content": "hello"}}'
    assert rbg.render_line(line) == "user: hello"


def test_render_text_block_list():
    line = (
        '{"type": "assistant", "message": {"role": "assistant",'
        ' "content": [{"type": "text", "text": "hi there"}]}}'
    )
    assert rbg.render_line(line) == "assistant: hi there"


def test_render_tool_use_block():
    line = (
        '{"message": {"role": "assistant",'
        ' "content": [{"type": "tool_use", "name": "Bash"}]}}'
    )
    assert rbg.render_line(line) == "assistant: [tool: Bash]"


def test_render_skips_blank_unparseable_and_empty():
    assert rbg.render_line("") is None
    assert rbg.render_line("   ") is None
    assert rbg.render_line("{bad json") is None
    assert rbg.render_line('{"type": "system", "message": {"content": []}}') is None


def test_render_tolerates_unknown_keys():
    line = '{"type": "assistant", "weird": 1, "message": {"role": "assistant", "content": "ok"}}'
    assert rbg.render_line(line) == "assistant: ok"


def test_render_stream_prints_only_renderable():
    lines = [
        '{"message": {"role": "user", "content": "q"}}',
        "garbage",
        '{"message": {"role": "assistant", "content": "a"}}',
    ]
    buf = io.StringIO()
    rbg.render_stream(lines, out=buf)
    assert buf.getvalue() == "user: q\nassistant: a\n"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_render.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'render_line'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_render.py -v`
Expected: 6 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_render.py
git commit -m "feat: render transcript jsonl lines to text"
```

---

## Task 8: Remote command builders

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_remote_cmds.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_remote_cmds.py`:

```python
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
    assert "claude -p 'next step' --resume sid-1 --output-format stream-json" in cmd
    assert "cd /proj" in cmd


def test_read_cmd_replay_and_follow():
    assert rbg.remote_read_cmd("sid-1", follow=False) == (
        "tail -n +1 ~/.claude/projects/*/sid-1.jsonl 2>/dev/null"
    )
    assert rbg.remote_read_cmd("sid-1", follow=True) == (
        "tail -f -n +1 ~/.claude/projects/*/sid-1.jsonl 2>/dev/null"
    )


def test_attach_cmd():
    assert rbg.remote_attach_cmd("/proj", "sid-1") == (
        "cd /proj && claude --resume sid-1"
    )
    assert rbg.remote_attach_cmd("", "sid-1") == "claude --resume sid-1"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_remote_cmds.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'remote_launch_cmd'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
    flag = "-f " if follow else ""
    return f"tail {flag}-n +1 ~/.claude/projects/*/{session_id}.jsonl 2>/dev/null"


def remote_attach_cmd(cwd, session_id):
    return f"{_cd_prefix(cwd)}claude --resume {shlex.quote(session_id)}"
```

Note: `remote_read_cmd` interpolates `session_id` directly because session ids resolved from `claude` are UUID-like (`[A-Za-z0-9-]`) and embedding inside the glob path must stay literal. If you ever accept arbitrary ids, validate against `^[A-Za-z0-9-]+$` here.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_remote_cmds.py -v`
Expected: 5 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_remote_cmds.py
git commit -m "feat: remote command string builders"
```

---

## Task 9: `cmd_ping` and `cmd_launch`

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_cmd_launch.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_cmd_launch.py`:

```python
import io
import pytest
import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


def test_ping_reachable(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    assert rbg.cmd_ping(cfg(), runner=runner) == 0
    assert "desk: reachable" in capsys.readouterr().out


def test_ping_unreachable(capsys):
    runner = RecordingRunner(default=result(returncode=255))
    assert rbg.cmd_ping(cfg(), runner=runner) == 1
    assert "cannot reach 'desk' — disconnected" in capsys.readouterr().err


def test_launch_records_mapping():
    agents_json = '[{"name": "alpha", "sessionId": "sid-alpha"}]'
    runner = RecordingRunner(
        by_substring={"claude agents": result(stdout=agents_json)},
        default=result(returncode=0),
    )
    saved = {}
    sessions = {}

    rc = rbg.cmd_launch(
        cfg(), "alpha", "build it",
        runner=runner, sessions=sessions, save=lambda s: saved.update(s),
    )
    assert rc == 0
    assert saved == {"alpha": "sid-alpha"}
    # the launch command ran before the resolve query
    assert "claude --bg -n alpha 'build it'" in runner.remote_cmds[1]
    assert "claude agents --json --all" in runner.remote_cmds[2]


def test_launch_unresolved_id_errors(capsys):
    runner = RecordingRunner(
        by_substring={"claude agents": result(stdout="[]")},
        default=result(returncode=0),
    )
    rc = rbg.cmd_launch(
        cfg(), "alpha", "task",
        runner=runner, sessions={}, save=lambda s: None,
    )
    assert rc == 1
    assert "could not resolve" in capsys.readouterr().err
```

Note: `remote_cmds[0]` is the reachability `true` check; index 1 is launch, index 2 is the resolve query.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_launch.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'cmd_ping'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_launch.py -v`
Expected: 4 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_cmd_launch.py
git commit -m "feat: ping and launch commands"
```

---

## Task 10: `cmd_send`

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_cmd_send.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_cmd_send.py`:

```python
import rbg
from tests.helpers import RecordingRunner, result


def cfg():
    return rbg.Config(host="desk", cwd="/proj", ssh_opts=[])


def test_send_known_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_send(cfg(), "alpha", "next", runner=runner,
                      sessions={"alpha": "sid-1"})
    assert rc == 0
    assert "sent to alpha" in capsys.readouterr().out
    assert "new-window -t rbg -n alpha" in runner.remote_cmds[1]


def test_send_unknown_agent(capsys):
    runner = RecordingRunner(default=result(returncode=0))
    rc = rbg.cmd_send(cfg(), "ghost", "x", runner=runner, sessions={})
    assert rc == 1
    assert "unknown agent 'ghost'" in capsys.readouterr().err


def test_send_busy_session(capsys):
    runner = RecordingRunner(default=result(returncode=3))
    rc = rbg.cmd_send(cfg(), "alpha", "x", runner=runner,
                      sessions={"alpha": "sid-1"})
    assert rc == 3
    assert "busy" in capsys.readouterr().err
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_send.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'cmd_send'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_send.py -v`
Expected: 3 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_cmd_send.py
git commit -m "feat: send command via per-session tmux window"
```

---

## Task 11: `cmd_read` (replay + follow)

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_cmd_read.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_cmd_read.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_read.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'cmd_read'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_read.py -v`
Expected: 3 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_cmd_read.py
git commit -m "feat: read command with replay and live follow"
```

---

## Task 12: `cmd_ls` and `cmd_attach`

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_cmd_ls_attach.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_cmd_ls_attach.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_ls_attach.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'cmd_ls'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cmd_ls_attach.py -v`
Expected: 3 passed.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_cmd_ls_attach.py
git commit -m "feat: ls and attach commands"
```

---

## Task 13: CLI wiring (`argparse`) + executable entrypoint

**Files:**
- Modify: `rbg.py`
- Test: `tests/test_cli.py`

- [ ] **Step 1: Write the failing test**

Create `tests/test_cli.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cli.py -v`
Expected: FAIL with `AttributeError: module 'rbg' has no attribute 'build_parser'`.

- [ ] **Step 3: Add the implementation to `rbg.py`**

Append to `rbg.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest tests/test_cli.py -v`
Expected: 4 passed.

- [ ] **Step 5: Make the script executable and verify the full suite**

```bash
cd /Users/divkov/workplace/remote-ccbg
chmod +x rbg.py
pytest -v
```

Expected: all tests across all files pass (no failures).

- [ ] **Step 6: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add rbg.py tests/test_cli.py
git commit -m "feat: argparse CLI wiring and executable entrypoint"
```

---

## Task 14: End-to-end smoke test against the real desktop (manual)

**Files:** none (manual verification).

- [ ] **Step 1: Configure**

```bash
cat > ~/.rbg.conf <<'EOF'
RBG_HOST=<your-desktop-host>
RBG_CWD=<remote-project-dir>
EOF
```

- [ ] **Step 2: Exercise the full loop**

```bash
cd /Users/divkov/workplace/remote-ccbg
./rbg.py ping                                   # -> "<host>: reachable"
./rbg.py launch demo "say hello and stop"       # -> "launched demo (<id>)"
./rbg.py ls                                      # demo appears with its id
./rbg.py read demo                               # replays the transcript
./rbg.py send demo "now count to three"          # -> "sent to demo"
./rbg.py read demo -f                            # follows live; Ctrl-C to stop
```

Verify: Ctrl-C during `read -f` returns you to the shell while the remote tmux window keeps running (`./rbg.py read demo` still shows new output afterward).

- [ ] **Step 3: Verify the disconnection gate**

Temporarily point at an unreachable host and confirm the gate:

```bash
RBG_HOST=10.255.255.1 ./rbg.py ls   # -> "cannot reach '10.255.255.1' — disconnected", exit 1
```

- [ ] **Step 4: Confirm append-vs-fork behavior (HLD risk)**

After a `send`, run `./rbg.py read demo` and confirm the new task's output was **appended** to the same transcript (same session id, conversation continues) rather than forking. If it forks, add `--fork-session`'s inverse handling: the HLD notes `-p --resume` appends by default; if your `claude` version forks, document it and revisit `remote_send_cmd`.

---

## Self-Review

**1. Spec coverage** (against `docs/HLD-remote-agent-management.md` §2.1, §5.2):
- launch → Task 9 ✓
- send (non-blocking, tmux, serialized per session) → Task 8 (`remote_send_cmd` busy-reject) + Task 10 ✓
- read (replay + `-f` follow, Ctrl-C stops tail only) → Task 8 + Task 11 ✓
- ls (`claude agents --json --all`) → Task 12 ✓
- attach (`ssh -t … claude --resume`) → Task 8 + Task 12 ✓
- ping → Task 9 ✓
- Connection gate (`BatchMode`, `ConnectTimeout=5`, exact message, exit 1, before doing anything) → Task 4, applied first in every `cmd_*` ✓
- Config via env / `~/.rbg.conf` (`RBG_HOST` required, `RBG_CWD`, `RBG_SSH`) → Task 3 ✓
- Local name→id map → Task 5, populated in Task 9 ✓
- Read from documented JSONL, tolerant of unknown keys → Task 7 ✓
- Risk: two concurrent sends corrupt ordering → mitigated by per-name tmux window busy-reject (Task 8) ✓
- Risk: undocumented transcript schema → defensive `render_line` (Task 7) ✓
- Risk: `agents --json` shape unverified → Task 2 verifies before parsing is built on it ✓
- Risk: append-vs-fork on `-p --resume` → Task 14 step 4 confirms on real desktop ✓

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N" — all code is complete and inline.

**3. Type consistency:** `Config(host, cwd, ssh_opts)` used identically everywhere. `runner` signature `(cmd, **kwargs) -> CompletedProcess` consistent across all `cmd_*`. `sessions` is `{name: id}` throughout (`save`/`load_sessions`, `find_agent_id` output stored as value). `ID_KEYS` defined once (Task 6), reused in Task 12. `remote_*_cmd` names and arg orders match their call sites in Tasks 9–12. `render_stream(lines, out)` and `render_line(line)` signatures match Task 7 definitions used in Task 11.
