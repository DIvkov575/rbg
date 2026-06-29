# Design: rbg UX upgrade — auto-names + dashboard TUI

**Date:** 2026-06-29 · **Status:** Approved

## Goal
Stop forcing the user to type/choose agent names, and let them arrow through
agents in an interactive dashboard instead of memorizing names.

## Two features

### 1. Auto-generated names
- `--name` becomes optional on `launch` (CLI: `rbg launch "<task>"` with no name).
- When omitted, the **agent** (desktop side) derives a slug from the task and
  dedups against its own session store:
  - slug rule: lowercase; keep `[a-z0-9]` runs; drop stopwords
    (the, a, an, to, of, in, on, for, and, is, it); join with `-`;
    cap at 4 words / 40 chars; empty → `agent`.
  - dedup: if the slug exists in the store, append `-2`, `-3`, … until free.
- Dedup lives on the agent because the name set lives there
  (`~/.rbg-agent/sessions.json`); this stays correct under concurrent launches.
- Client just forwards `--task` (and `--name` only if the user gave one) and
  prints the `id` the agent returns.

### 2. Dashboard TUI (`rbg` with no args, or `rbg dash`)
- Built with **stdlib only** (no Bubble Tea): raw terminal mode via `syscall`
  termios ioctls + hand-rolled ANSI escapes. The public Go module proxy is
  unreachable in this environment, so external TUI deps are not an option; the
  project's zero-dep property is preserved. Raw-mode is build-tagged per OS
  (darwin/linux) since the client may run on either.
- Two panes: left = agent list (from ls), right = selected agent's rendered
  transcript.
- Keys: `↑/↓` move · `⏎`/`v` load transcript · `a` attach · `r` refresh · `q` quit.
- **View + attach** only. Launch/send remain CLI verbs (they need free-text
  input; keeping them on the CLI avoids modal text widgets in the TUI).
- **Attach**: restore cooked terminal mode, run `ssh -t … claude --resume <id>`
  wired to the real stdio (reuse existing attach path), then re-enter raw mode
  and refresh the list on return.
- **Refresh model:** no background polling. List fetched on open and on `r`;
  transcript fetched on select and on `r`. Zero idle SSH traffic; user-driven.

## Architecture / isolation
- New `internal/slug` — pure task→slug function (no deps, table-tested).
- Agent dedup added in `internal/agent` (uses slug + its store).
- New `internal/tui` — a small model split into pure logic and an I/O loop:
  - `internal/tui/model.go`: pure state machine — `Model` + `Update(Model, Key)
   (Model, Action)` and `View(Model) string`. No terminal, fully unit-tested.
  - `internal/tui/term.go` (+ `term_darwin.go`/`term_linux.go` build-tagged):
   raw-mode enter/exit, key reader (arrow-escape parsing), render loop.
  The loop is a thin consumer of client fetchers and the pure model.
- New client fetchers returning **structured data** (the TUI can't use the
  current stdout-writing `Ls`/`Read`):
  - `FetchSessions(cfg, r) ([]session.Session, error)`
  - `FetchTranscript(cfg, r, name) (string, error)` (rendered text)
  The existing `Ls`/`Read` stdout verbs are refactored to call these.
- `cmd/rbg`: `--name` optional on launch; new `dash` verb + no-args default
  launches the TUI.
- **Unchanged:** sshx, render, session, config, run (consumed, not modified).

## Testing
- `internal/slug`: table-driven unit tests (slugging + edge cases).
- agent dedup: unit tests with a seeded store (concurrent-name collisions).
- client fetchers: unit tests with the `run.Recording` stub (parse ls JSON →
  []Session; read bytes → rendered string).
- TUI: drive the pure `Update(Model, Key)` with key and data inputs; assert
  selection movement, pane loading, and that `a` yields an Attach action. No TTY.
  The raw-mode/term layer is thin and exercised via the integration harness.
- Attach + live SSH: covered by the integration harness.

## Out of scope
- In-TUI launch/send (text-input modals).
- Background auto-polling / live tail.
- Random-slug or timestamp naming (chose task-derived).
