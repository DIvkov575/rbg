# First-Class Projects + Own-Render Transcript — Design

**Date:** 2026-07-02 · **Status:** Design (approved section-by-section)

**Goal:** Make a *project* a first-class, directory-anchored thing that owns agents and
auto-detects its git repo (and can be mirrored to the desktop on demand), and replace the
three incoherent "open an agent" paths with one: our own faithful, tailing render of the
transcript — with follow-ups delivered by resume (done) or daemon-socket injection (live).

---

## 1. Background & Problem

Today a "project" is *derived and ephemeral*: `core.ProjectKey(agent)` groups agents whose
working-dir **leaf** matches, and `core.Project{Label,Repo,Origin}` is only a picker
*suggestion*. Nothing about a project is stored; a project cannot exist without an agent.

"Opening" an agent is three inconsistent mechanisms glued to one keypress:
1. **remote** agent → `render.Line` reconstructs the JSONL into lossy `role: text` lines
   (tool calls collapse to `[tool: X]`, formatting/thinking dropped) — **the "strange,
   unreasonable answers"** the user reported. It is not the session; it is our crude
   summary of it.
2. **local** agent → `claude --resume` via `tea.ExecProcess` — which **fails on a live bg
   session** (a process already owns it).
3. the **pty bridge** built previously (the real live terminal) — **wired to nothing.**

This design fixes both: projects become explicit and directory-anchored, and "open" becomes
one faithful transcript render for every agent, with a coherent follow-up path.

## 2. Goals / Non-Goals

**Goals**
- Project = a **local directory**; identity is the dir path; display name defaults to the
  dir leaf and is **renameable**; a **single** git repo is **auto-detected** (origin URL).
- Projects are **persisted** and can exist without agents; agents **explicitly link** to a
  project. Projects appear via **explicit add** *and* **auto-adopt** (any dir with agents).
- Remote **mirrors the project structure** (dir + clone) **lazily**, on first remote spawn
  or explicit sync. Agent *placement* need not mirror.
- Attachment is **always our own render** of the transcript: **rich & faithful**, **live
  tail** for running agents, **last-turn-first** instant open.
- Follow-ups: **done** → `claude --resume`; **live** → **inject text over the daemon
  socket** (no terminal attach).

**Non-Goals**
- No terminal attach / true `claude` TUI (pty bridge is removed except a codec slice).
- No background/eager remote sync, no automatic pull/push. Mirror = "dir + clone exist."
- No multiple repos per project (explicitly walked back to single, auto-detected).

## 3. Core model changes (`internal/core`)

### 3.1 `Project` becomes first-class

Rename the current picker-suggestion type `core.Project{Label,Repo,Origin}` → **`Suggestion`**
(same fields, same `MergeProjects`→`MergeSuggestions`), freeing the name. Introduce:

```go
// Project is a first-class unit of work: a local directory that owns agents and
// has (at most) one auto-detected git repo. Identity is Dir; it is the map key
// in the ProjectStore. It exists independently of any agent.
type Project struct {
    Dir    string `json:"dir"`    // absolute LOCAL dir — identity / store key
    Name   string `json:"name"`   // display name; defaults to leaf(Dir), renameable
    Repo   string `json:"repo"`   // origin URL, auto-detected; "" if not a git repo
    Remote string `json:"remote"` // mirrored desktop dir; "" until mirrored
}
```

### 3.2 Agent gains an explicit project link

`core.Agent` gains `ProjectDir string` (`json:"projectDir"`) — the owning project's `Dir`.
This replaces fuzzy leaf-matching for membership. Back-compat: a record with an empty
`ProjectDir` falls back to its own `Dir` (so existing agents.json rows still group).

### 3.3 Grouping rebuilt on the link

- `ProjectKey(a)` → returns `a.ProjectDir` if set, else `a.Dir`, else `a.Repo`-leaf, else `""`.
- `GroupByProject(agents, projects []Project)` now takes the project list so a project with
  **zero** agents still renders (explicit-add-then-empty). Groups are keyed by project `Dir`;
  the display name comes from the matching `Project.Name` (fallback `leaf(dir)`); the
  unlinked `""` bucket sorts last. Agents within a group sort by Name.
- `leaf()` stays as-is (shared path/URL leaf extractor).

### 3.4 Project store (`internal/core/projectstore.go`)

Mirror `Store` exactly (atomic temp-file + rename, corrupt→empty, keyed map):

```go
type ProjectStore struct { path string; projects map[string]Project } // key = Dir
func LoadProjectStore(path string) (*ProjectStore, error)
func (s *ProjectStore) Add(p Project)                 // insert/update by Dir
func (s *ProjectStore) Get(dir string) (Project, bool)
func (s *ProjectStore) Rename(dir, name string) bool  // set Name; false if absent
func (s *ProjectStore) Delete(dir string)
func (s *ProjectStore) Records() []Project            // sorted by Name
func (s *ProjectStore) Save() error
```

Stored at `~/.rbg/projects.json` as `{"projects": {"<dir>": {…}}}`.

### 3.5 Auto-adopt reconcile

`core.ReconcileProjects(stored []Project, agents []Agent) []Project` (pure): starts from
`stored`, then for every agent whose `ProjectDir` (or `Dir`) is non-empty and absent from
`stored`, synthesize a `Project{Dir: d, Name: leaf(d)}` (Repo left "" — detection is I/O,
done by the engine layer, not here). Result sorted by Name, deduped by Dir. This is what the
UI/engine lists, so dirs-with-agents always show up even before an explicit add.

## 4. Engine surface (`internal/engine`)

- Add a `projectStore *core.ProjectStore` to `Engine`; load it in `New` beside the agent store.
- **`Projects() []core.Project`** — real list now: `ReconcileProjects(projectStore.Records(),
  List()-agents)`, then fill each `Repo` via `detectOrigin(dir)` for rows that lack one
  (cached back into the store best-effort). Replaces the old suggestion-only `Projects()`.
  (The picker's *suggestions* — local/remote/github dirs to pick when adding — move to a new
  `Suggestions() []core.Suggestion`.)
- **`AddProject(dir string) (core.Project, error)`** — abs-resolve `dir`, detect origin
  (`git -C <dir> remote get-url origin`, trimmed; missing → ""), `Add` + `Save`, return it.
- **`RenameProject(dir, name string) error`** — `Rename` + `Save`.
- **`MirrorProject(dir string) (core.Project, error)`** — compute remote path from the
  local↔remote base convention (§5), ssh `mkdir -p`, clone origin if `Repo!=""` and no
  remote `.git`, stamp `Remote`, `Save`. Idempotent.
- **Spawn wiring:** `Create`/spawn stamps the new agent's `ProjectDir` from the selected
  project. A **remote** spawn calls `MirrorProject` first so the dir+clone exist; the agent's
  `Dir` becomes the project's `Remote` path.
- `find(ref)` already resolves by session-id-then-name (kept from the prior fix).

## 5. Remote mirror mechanics (`internal/host`)

- **Path convention (already present):** local base `~/workplace`, remote base
  `<RBG_CWD>/workplace`. Local `…/workplace/<leaf>` ↔ remote `<remoteBase>/<leaf>`. A project
  dir outside `~/workplace` mirrors to `<remoteBase>/<leaf>` by leaf (documented limitation).
- **Reuse the existing `rbg-agent clone` verb**, generalized: today it clones into
  `~/rbg-repos/<leaf>`; add an optional `--dir <abs>` so the engine can target the mirrored
  project path instead. `mkdir -p` + reuse-if-`.git`-present is already in `Clone`.
- Mirror is **lazy and idempotent**; no pull/push, no watch.

## 6. Own-render transcript (`internal/render` rewrite + `internal/uitea/pager.go`)

### 6.1 Faithful reconstruction
Rewrite `render` from `Line(one-line)→"role: text"` to a **turn-oriented** renderer:
`Render(jsonl []byte, opts) []string` producing styled lines:
- **user** turn: `▸ you` header + full prompt text.
- **assistant** turn: `▸ claude` header + full text; **tool_use** → `⚙ <ToolName>(<short
  input summary>)`; **tool_result** → indented, **truncated** to first N lines with
  `… +M more` marker; **thinking** → dim, shown by default (toggleable later, YAGNI now).
- Turn separators (blank line) and lipgloss role colors (reuse `view.go` palette).
- Tolerates unknown record types / malformed lines (skip), like today.

### 6.2 Instant open = last turn first
Open must not block on SSH, and must show the latest turn immediately. `Render` gains a
`Tail int` option: `Tail>0` renders only the last N turns (N=1 on first open), `Tail=0`
renders all. `newSessionView` opens in loading state and issues a read; the first
`transcriptMsg` renders with `Tail:1` (instant, latest turn, pinned to bottom), and a
follow-up full read (`Tail:0`) fills the scrollback in place. The tail refresh (§6.3) always
uses `Tail:0` so scrollback stays complete while pinned to the newest turn.

### 6.3 Live tail
When the opened agent's `State == Running`, schedule a `tea.Tick` (~1.5s) that re-issues the
read; on each `transcriptMsg` the pager re-renders and stays pinned to bottom. Tailing stops
when the agent is `done`, on error, or when the view closes (guarded by
`mode==modePager && pager.agent==session`). Reuses `CachingTranscripts` so repeat reads are
cheap and offline-tolerant.

## 7. Follow-up send (done vs live)

The pager prompt bar submits a follow-up; the engine routes by lifecycle:
- **done** → existing `Send` path: `claude -p --resume <id> <text>` on the agent's machine.
- **live** → **`ptybridge.Inject(home, session, text)`**: connect the worker's `ptySock`
  (from roster), read the `hello` frame, write `text + "\r"` as a `KindData` stdin frame,
  close. No terminal, no raw mode. (Prior probing showed input is ungated on fleet workers;
  implementation re-verifies a real keystroke lands before relying on it. Remote agents inject
  desktop-side via `rbg-agent inject --id <session> --text <text>`.)
- After send, the tail (or a one-shot re-read for done agents) surfaces the new turn.

## 8. What is deleted vs kept

**Deleted**
- `internal/ptybridge/bridge.go`, `attach.go` (terminal attach loop + raw mode).
- `uitea.openClientCmd` (`claude --resume` via `ExecProcess`) and the local-vs-remote open
  fork in `openSelected` — one render path for all.
- `rbg attach` (cmd/rbg) and `rbg-agent attach` (cmd/rbg-agent) terminal verbs.
- Old `render.Line`/`Stream` shape (replaced by `Render`).

**Kept / repurposed**
- `internal/ptybridge/frame.go` (frame codec) + `roster.go` (worker lookup) + **new
  `inject.go`** (`Inject`).
- `rbg-agent inject --id --text` verb (replaces `attach`); `rbg-agent clone --dir` addition.
- `pagerModel` (fed by the new renderer + tail; prompt bar routes done/live send).

## 9. UI & keys (`internal/uitea`)

- **Project view is primary** (default lens), backed by real `ProjectStore`: each project is
  a section — `Name`, a repo badge (origin leaf or "no repo"), a "mirrored" badge when
  `Remote!=""`; agents nested beneath. Remote/local machine lenses remain via tab.
- **Keys:** `^n` → **add project** (dir browser → auto-detect origin → row); `^e` →
  **rename** selected project; spawn prompt bar spawns into the selected project (its
  dir/repo, machine from the lens; remote spawn mirrors first); `enter` on an agent → the
  render view; existing `^x` kill / `^a` adopt / `^r` refresh / `^p` repair / `^d` debug.

## 10. Testing

- **core:** `ProjectStore` round-trip/rename/delete; `ReconcileProjects` auto-adopt (stored
  ∪ agent dirs, dedupe, empty-project survives); `ProjectKey`/`GroupByProject` on the explicit
  link incl. zero-agent project and unlinked bucket.
- **render:** golden tests over a fixture JSONL — user/assistant/tool_use/tool_result/thinking
  → expected styled lines; truncation marker; malformed line skipped; empty → "(no content)".
- **engine:** `AddProject` detects origin (fake runner returns a URL / errors→""),
  `RenameProject` persists, `MirrorProject` idempotent (no re-clone when `.git` present),
  remote spawn stamps `ProjectDir` + mirrors. `Projects()` = reconcile+detect.
- **ptybridge:** `Inject` writes hello-then-stdin framing to a `net.Pipe` fake server
  (asserts a `KindData` frame carrying `text\r`).
- **uitea:** project view renders sections incl. empty project; `^n`/`^e` flows; open→render
  keys on session id (kept); live agent → tail tick scheduled; done agent → no tick; send
  routes done vs live.

## 11. Risks

| Risk | Mitigation |
|---|---|
| Daemon socket protocol is undocumented, version-pinned (2.1.197) | Codec has a `maxFrame` guard; `Inject` fails loudly, follow-up reports error; done-path unaffected. Re-verify a live keystroke lands during impl. |
| Live inject races the agent mid-turn | Best-effort; the tail shows whatever the agent does. Not a correctness guarantee — documented. |
| Project dir outside `~/workplace` mirrors only by leaf | Documented limitation; leaf-collision across two out-of-tree dirs is possible but rare. |
| Two stores (agents.json + projects.json) drift | `ReconcileProjects` is the single source of truth for the displayed list; auto-adopt heals missing rows each load. |
