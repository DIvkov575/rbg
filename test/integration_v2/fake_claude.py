#!/usr/bin/env python3
"""A fake `claude` CLI for rbg integration tests.

This stands in for the real `claude` binary on the simulated "desktop". It
implements just enough of the surface rbg drives:

  claude --bg -n <name> "<task>"
      Register a background agent. Allocates a deterministic session id,
      records it in $HOME/.claude/agents.json, and seeds a transcript file at
      $HOME/.claude/projects/<slug>/<id>.jsonl with one user line for <task>.

  claude agents --json --all
      Print the registered agents as a JSON array of
      {"name", "sessionId"} objects (the shape rbg's parser expects).

  claude -p "<task>" --resume <id> --output-format stream-json
      Append a user line and a canned assistant line to that session's
      transcript, then print the assistant line as stream-json. This is the
      headless "send" path; it APPENDS to the existing transcript (it does not
      fork), mirroring HLD Option B.

  claude --resume <id>
      Interactive attach. We can't drive a real TTY in tests, so this just
      prints a marker and exits 0.

Everything is rooted at $HOME so the test harness can sandbox it to a throwaway
directory and never touch a real ~/.claude.

NOTE: this is a SIMULATION of rbg's assumptions about claude, not the real
contract. A green run proves rbg's mechanics, not that the real `claude`
emits this exact JSON shape — that remains a one-time manual check (plan Task 2).
"""
import json
import os
import sys

HOME = os.environ["HOME"]
CLAUDE_DIR = os.path.join(HOME, ".claude")
AGENTS_FILE = os.path.join(CLAUDE_DIR, "agents.json")
PROJECTS_DIR = os.path.join(CLAUDE_DIR, "projects")
# A single fixed project slug keeps the transcript glob (projects/*/<id>.jsonl)
# honest while staying simple.
SLUG = "sim-project"

# Flags that consume the following token as their value (so it isn't mistaken
# for the trailing positional task argument).
FLAGS_WITH_VALUES = {"-n", "--name", "-p", "--print", "--resume",
                     "--output-format", "--model"}


def _load_agents():
    try:
        with open(AGENTS_FILE) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return []


def _save_agents(agents):
    os.makedirs(CLAUDE_DIR, exist_ok=True)
    with open(AGENTS_FILE, "w") as f:
        json.dump(agents, f)


def _transcript_path(session_id):
    return os.path.join(PROJECTS_DIR, SLUG, f"{session_id}.jsonl")


def _append_message(session_id, role, text):
    path = _transcript_path(session_id)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    record = {
        "type": role,
        "sessionId": session_id,
        "message": {"role": role, "content": text},
    }
    with open(path, "a") as f:
        f.write(json.dumps(record) + "\n")
    return record


def _next_session_id(agents):
    # Deterministic, glob-safe id (matches rbg's ^[A-Za-z0-9-]+$ guard).
    return f"sid-{len(agents) + 1}"


def _opt_value(args, flag):
    """Return the value following `flag`, or None."""
    if flag in args:
        i = args.index(flag)
        if i + 1 < len(args):
            return args[i + 1]
    return None


def _last_positional(args):
    """The trailing non-flag argument (the task string)."""
    skip_next = False
    positional = None
    for a in args:
        if skip_next:
            skip_next = False
            continue
        if a in FLAGS_WITH_VALUES:
            skip_next = True
            continue
        if a.startswith("-"):
            continue
        positional = a
    return positional or ""


def cmd_bg(name, task):
    agents = _load_agents()
    session_id = _next_session_id(agents)
    agents.append({"name": name, "sessionId": session_id})
    _save_agents(agents)
    _append_message(session_id, "user", task)
    print(f"Launched background agent '{name}' ({session_id})")
    return 0


def cmd_agents_json():
    print(json.dumps(_load_agents()))
    return 0


def cmd_resume_headless(session_id, task):
    _append_message(session_id, "user", task)
    record = _append_message(session_id, "assistant", f"ack: {task}")
    # stream-json output is one JSON object per line.
    print(json.dumps(record))
    return 0


def cmd_resume_interactive(session_id):
    print(f"[fake-claude] interactive attach to {session_id}")
    return 0


def main(argv):
    args = argv[1:]
    if not args:
        print("fake-claude: no command", file=sys.stderr)
        return 2

    # claude agents --json --all
    if args[0] == "agents":
        if "--json" in args:
            return cmd_agents_json()
        print("fake-claude: only --json agents supported", file=sys.stderr)
        return 2

    # claude --bg -n <name> <task>
    if "--bg" in args:
        name = _opt_value(args, "-n") or _opt_value(args, "--name") or "agent"
        return cmd_bg(name, _last_positional(args))

    # claude -p <task> --resume <id> [--output-format stream-json]
    if "-p" in args or "--print" in args:
        session_id = _opt_value(args, "--resume")
        task = _opt_value(args, "-p") or _opt_value(args, "--print")
        if not session_id:
            print("fake-claude: -p requires --resume <id>", file=sys.stderr)
            return 2
        return cmd_resume_headless(session_id, task)

    # claude --resume <id>   (interactive)
    if "--resume" in args:
        return cmd_resume_interactive(_opt_value(args, "--resume"))

    print(f"fake-claude: unrecognized: {args}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
