#!/usr/bin/env python3
"""A fake `claude` CLI for rbg integration tests.

Mirrors the headless contract rbg actually drives, as verified against the real
ASBX claude (v2.1.187) on a dev desktop:

  claude -p "<task>" --session-id <uuid>
      Launch. Creates the session transcript at
      $HOME/.claude/projects/<cwd-slug>/<uuid>.jsonl with a user line for <task>
      and a canned assistant reply. Leaves NO resident process.

  claude -p "<task>" --resume <uuid>
      Send. APPENDS a user line + canned assistant reply to that session's
      existing transcript (does not fork).

  claude --resume <uuid>   (no -p)
      Interactive attach. No TTY in tests, so it just prints a marker.

The project directory is derived from the current working directory (matching
real claude, e.g. cwd /local/home/divkov -> projects/-local-home-divkov/), so
rbg's Read — which globs ~/.claude/projects/*/<uuid>.jsonl by session id —
exercises the real lookup path rather than a fixed slug.

Rooted at $HOME so the harness can sandbox it and never touch a real ~/.claude.
This SIMULATES the contract; the real-claude verification is done on a live host.
"""
import json
import os
import sys

HOME = os.environ["HOME"]
PROJECTS_DIR = os.path.join(HOME, ".claude", "projects")


def _cwd_slug():
    # Real claude slugifies the cwd: replace os.sep runs with '-'.
    cwd = os.getcwd()
    return cwd.replace(os.sep, "-")


def _transcript_path(session_id):
    return os.path.join(PROJECTS_DIR, _cwd_slug(), f"{session_id}.jsonl")


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


def _opt_value(args, flag):
    if flag in args:
        i = args.index(flag)
        if i + 1 < len(args):
            return args[i + 1]
    return None


def _headless(session_id, task):
    """Run a headless -p turn against session_id: append user + assistant."""
    _append_message(session_id, "user", task)
    _append_message(session_id, "assistant", f"ack: {task}")
    # Real claude prints the reply text to stdout; rbg ignores it (reads the
    # transcript), but we echo it for realism.
    print(f"ack: {task}")
    return 0


def main(argv):
    args = argv[1:]
    if not args:
        print("fake-claude: no command", file=sys.stderr)
        return 2

    is_headless = "-p" in args or "--print" in args
    task = _opt_value(args, "-p") or _opt_value(args, "--print") or ""
    session_id = _opt_value(args, "--session-id") or _opt_value(args, "--resume")

    if is_headless:
        if not session_id:
            print("fake-claude: -p requires --session-id or --resume", file=sys.stderr)
            return 2
        return _headless(session_id, task)

    # claude --resume <id>  (interactive attach)
    if "--resume" in args:
        print(f"[fake-claude] interactive attach to {session_id}")
        return 0

    print(f"fake-claude: unrecognized: {args}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
