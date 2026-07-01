# rbg Dashboard — Pure UI Core (Screen Stack + Lens Views) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure presentation core of rbg's new dashboard: a `Screen` interface + a screen stack (replacing the old TUI's boolean mode-flag sprawl), a `Model` over the reconciled `[]core.Agent`, the four lens views (remote / local / combined / by-project) with `ctrl-s` cycling, and a text-input screen — all pure (no I/O), fully table-testable. This is Phase 4, slice 1.

**Architecture:** The front-end of `docs/HLD-rbg-clean-architecture.md` (§4.4 Screen stack, §4.3 views as lenses). The old `internal/tui` is a ~900-line monolith with ~7 boolean mode-flags and 5 key-decoders in one `Update`/`View` — the exact rot the HLD replaces. This slice builds a NEW package `internal/ui` with the clean design; it does NOT touch the old `tui` or the shipped dashboard (that swap is slice 2). Everything here is a pure function of `(Model, Key)` → `(Model, Action)` and `Model` → `string`; the engine wiring and the raw-terminal loop are slice 2. Views are pure lenses over `[]core.Agent` (reusing `core.OnMachine`/`core.GroupByRepo`).

**Scope split:** Phase 4 has two slices. **Slice 1 (this plan):** the pure `ui` core — Screen/stack/Model/views/input, no I/O. **Slice 2 (next):** wire `ui` to `*engine.Engine`, reuse the proven raw-terminal machinery (`rawMode`/`termSize`/`readRaw` — copied from `tui`), route dashboard actions to engine ops, and swap `cmd/rbg`'s `dash` onto it, then retire `internal/tui`/`client`/`queue`. Splitting keeps the pure logic reviewable in isolation from terminal I/O.

**Tech Stack:** Go 1.26 (module `github.com/divkov575/rbg`), stdlib only. Reuses `internal/core` (`Agent`, `Location`, `Lifecycle`, `Sync`, `OnMachine`, `GroupByRepo`, `RepoGroup`). No I/O, no other internal deps. No third-party deps.

**Scope of this plan (HLD §4.3/§4.4/§5.6):**
- The **Screen stack** navigation model (esc pops) — replaces boolean mode-flags.
- The four **views as lenses**: Remote, Local, Combined (sectioned by machine), Project (grouped by repo, with a sync badge). `ctrl-s` cycles them.
- Cursor navigation and selected-agent resolution across views.
- An **Action** vocabulary the loop (slice 2) will fulfill: Run/Send/Kill/Read/Adopt/Create/Refresh/Quit.
- A **text-input screen** (for Create's task and Send's follow-up) that exercises the stack (push on open, pop on esc/enter).
- **Not in scope:** ANY I/O (SSH/engine/terminal/file); the raw-terminal loop; config/dir-browse screens (add later if wanted); the `cmd/rbg` swap and retiring `tui`/`client`/`queue` (slice 2).

**Verified facts (grounded 2026-07-01):**
- `core.OnMachine(agents []core.Agent, where core.Location) []core.Agent` and `core.GroupByRepo(agents []core.Agent) []core.RepoGroup` (`RepoGroup{Repo string; Agents []core.Agent}`) exist and are pure.
- `core.Agent` fields: Name/Repo/Dir/Task/Session/Where(`Local`/`Remote`)/State(`Held`/`Running`/`Done`)/Origin(`Managed`/`Foreign`)/Sync(`SyncUnknown`/`Aligned`/`Ahead`/`Behind`/`Dirty`)/RunAt/Pid; predicates `IsHeld()`, `IsForeign()`.
- The old `tui` pins `ctrl-s` = byte `0x13` as the view-cycle key (reused here). Its `rawMode`/`termSize`/`readRaw` machinery is proven and will be COPIED into `ui` in slice 2 (not imported — `tui` is being retired).
- The old `tui.Update`/`View` are pure; this slice keeps that discipline but replaces the flag-dispatch with a Screen stack.

---

## File Structure

New package `internal/ui`:

- Create: `internal/ui/key.go` — `Key` enum + `DecodeKey([]byte) (Key, rune)` (pure byte→key).
- Create: `internal/ui/key_test.go`
- Create: `internal/ui/model.go` — `Model`, `ViewMode` (+cycle), `Action`/`ActionKind`, `Screen` interface, stack ops (`Top`/`push`/`pop`), selected-agent resolution.
- Create: `internal/ui/model_test.go`
- Create: `internal/ui/list.go` — `listScreen`: `Update` (nav / ctrl-s cycle / action keys / open-input) and `View` (renders the current lens) + `Hints`.
- Create: `internal/ui/list_test.go`
- Create: `internal/ui/views.go` — pure renderers: `renderRemote`/`renderLocal`/`renderCombined`/`renderProject`, `syncBadge`, row formatting.
- Create: `internal/ui/views_test.go`
- Create: `internal/ui/input.go` — `inputScreen`: text entry; `Update` (runes/backspace/enter/esc) + `View` + `Hints`.
- Create: `internal/ui/input_test.go`

Each file has one responsibility; screens are separate files implementing one interface. No file holds a second concern (unlike the old monolith).

---

## Task 1: Key decoding

**Files:**
- Create: `internal/ui/key.go`
- Test: `internal/ui/key_test.go`

One pure decoder maps a raw input chunk to a `Key` (+ a rune for printable input). No per-mode decoders — screens interpret the same `Key` differently.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/key_test.go`:

```go
package ui

import "testing"

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Key
	}{
		{"up arrow", []byte{0x1b, '[', 'A'}, KeyUp},
		{"down arrow", []byte{0x1b, '[', 'B'}, KeyDown},
		{"enter", []byte{'\r'}, KeyEnter},
		{"enter-nl", []byte{'\n'}, KeyEnter},
		{"esc", []byte{0x1b}, KeyEsc},
		{"ctrl-s cycles view", []byte{0x13}, KeyCycleView},
		{"ctrl-c quits", []byte{0x03}, KeyQuit},
		{"j down", []byte{'j'}, KeyDown},
		{"k up", []byte{'k'}, KeyUp},
		{"q quit", []byte{'q'}, KeyQuit},
		{"r refresh", []byte{'r'}, KeyRefresh},
		{"empty", []byte{}, KeyNone},
	}
	for _, c := range cases {
		got, _ := DecodeKey(c.in)
		if got != c.want {
			t.Errorf("%s: DecodeKey(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestDecodeKeyRune(t *testing.T) {
	// A printable byte returns KeyRune and the rune itself (for text input).
	k, r := DecodeKey([]byte{'x'})
	if k != KeyRune || r != 'x' {
		t.Errorf("DecodeKey('x') = (%v,%q), want (KeyRune,'x')", k, r)
	}
	// Backspace (DEL 0x7f or BS 0x08) maps to KeyBackspace.
	if k, _ := DecodeKey([]byte{0x7f}); k != KeyBackspace {
		t.Errorf("0x7f should be KeyBackspace, got %v", k)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -v`
Expected: FAIL — `undefined: DecodeKey` / `undefined: Key` (build error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/key.go`:

```go
// Package ui is rbg's pure dashboard presentation layer: a Screen interface and
// a screen stack over a Model of the reconciled agent inventory, with the four
// lens views. Everything here is a pure function of (Model, Key) — no I/O. The
// engine wiring and the raw-terminal loop live in a separate layer.
package ui

// Key is a decoded input event. Screens interpret the same Key by context
// (there are no per-mode decoders); printable input arrives as KeyRune + a rune.
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyEsc
	KeyCycleView // ctrl-s (0x13): cycle the list view
	KeyRefresh   // r
	KeyRune      // a printable rune (carried alongside)
	KeyBackspace
	KeyQuit // q or ctrl-c
)

// DecodeKey maps one raw input chunk to a Key (and, for KeyRune, the rune). A
// nil/empty chunk is KeyNone. This is the single decoder; screens decide meaning.
func DecodeKey(b []byte) (Key, rune) {
	if len(b) == 0 {
		return KeyNone, 0
	}
	// arrow escape sequences: ESC [ A/B
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return KeyUp, 0
		case 'B':
			return KeyDown, 0
		}
		return KeyNone, 0
	}
	if len(b) == 1 {
		switch b[0] {
		case '\r', '\n':
			return KeyEnter, 0
		case 0x1b:
			return KeyEsc, 0
		case 0x13: // ctrl-s
			return KeyCycleView, 0
		case 0x03: // ctrl-c
			return KeyQuit, 0
		case 0x7f, 0x08: // DEL / BS
			return KeyBackspace, 0
		}
	}
	// single printable byte → rune (used for text input and letter shortcuts)
	if len(b) == 1 && b[0] >= 0x20 && b[0] < 0x7f {
		return KeyRune, rune(b[0])
	}
	return KeyNone, 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: FAIL still — `TestDecodeKey` expects `'j'`→`KeyDown`, `'q'`→`KeyQuit`, etc., but the decoder above returns `KeyRune` for those. This is intentional: **letter shortcuts are a screen concern, not the decoder's.** Fix the TEST to match: the decoder returns `KeyRune`+rune for all printable bytes, and the *listScreen* (Task 3) maps `j`/`k`/`q`/`r` to actions. Update `key_test.go`'s `TestDecodeKey` to remove the `j`/`k`/`q`/`r` cases (they are KeyRune now) and keep only the control-key cases (arrows, enter, esc, ctrl-s, ctrl-c, empty). Keep `TestDecodeKeyRune` as-is (it already expects KeyRune for `x`).

After fixing the test, run again:
Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/key.go internal/ui/key_test.go
git commit -m "feat(ui): single Key decoder (control keys; printables as KeyRune)"
```

---

## Task 2: Model, ViewMode, Action, Screen interface, stack

**Files:**
- Create: `internal/ui/model.go`
- Test: `internal/ui/model_test.go`

The `Model` holds the inventory, the current view, the cursor, size, a status line, an input buffer, and the screen stack. `Screen` is the interface every screen implements. The stack (`push`/`pop`/`Top`) replaces the old boolean mode-flags: navigating into a modal pushes a screen; esc pops.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/model_test.go`:

```go
package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestViewModeCycle(t *testing.T) {
	// ctrl-s cycles Remote → Local → Combined → Project → Remote.
	order := []ViewMode{ViewRemote, ViewLocal, ViewCombined, ViewProject}
	v := ViewRemote
	for i := 1; i <= 4; i++ {
		v = v.Next()
		want := order[i%4]
		if v != want {
			t.Errorf("after %d cycles: got %v, want %v", i, v, want)
		}
	}
}

func TestStackPushPopTop(t *testing.T) {
	base := &listScreen{}
	m := NewModel(nil)
	m.push(base)
	if m.Top() != base {
		t.Fatalf("Top after push should be base")
	}
	child := &inputScreen{}
	m.push(child)
	if m.Top() != child {
		t.Errorf("Top after second push should be child")
	}
	m.pop()
	if m.Top() != base {
		t.Errorf("Top after pop should be base again")
	}
	// popping the last screen leaves Top nil (loop treats nil as quit).
	m.pop()
	if m.Top() != nil {
		t.Errorf("Top after popping all should be nil")
	}
}

func TestSelectedAgent(t *testing.T) {
	agents := []core.Agent{
		{Name: "a", Where: core.Remote},
		{Name: "b", Where: core.Remote},
	}
	m := NewModel(agents)
	m.View = ViewRemote
	m.Cursor = 1
	sel, ok := m.Selected()
	if !ok || sel.Name != "b" {
		t.Errorf("Selected at cursor 1 = %+v ok=%v, want b", sel, ok)
	}
	// Out-of-range cursor → no selection (defensive).
	m.Cursor = 99
	if _, ok := m.Selected(); ok {
		t.Errorf("out-of-range cursor should yield no selection")
	}
	// Empty inventory → no selection.
	if _, ok := NewModel(nil).Selected(); ok {
		t.Errorf("empty inventory should yield no selection")
	}
}

func TestVisibleAgentsPerView(t *testing.T) {
	agents := []core.Agent{
		{Name: "loc", Where: core.Local},
		{Name: "rem", Where: core.Remote},
	}
	m := NewModel(agents)
	m.View = ViewLocal
	if v := m.Visible(); len(v) != 1 || v[0].Name != "loc" {
		t.Errorf("Local view = %+v, want [loc]", v)
	}
	m.View = ViewRemote
	if v := m.Visible(); len(v) != 1 || v[0].Name != "rem" {
		t.Errorf("Remote view = %+v, want [rem]", v)
	}
	m.View = ViewCombined
	if v := m.Visible(); len(v) != 2 {
		t.Errorf("Combined view should show both, got %d", len(v))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run "TestViewMode|TestStack|TestSelected|TestVisible" -v`
Expected: FAIL — `undefined: ViewMode`, `NewModel`, `listScreen`, `inputScreen`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/model.go`:

```go
package ui

import "github.com/divkov575/rbg/internal/core"

// ViewMode is which lens the list screen shows. ctrl-s cycles through them.
type ViewMode int

const (
	ViewRemote ViewMode = iota
	ViewLocal
	ViewCombined
	ViewProject
)

// Next returns the next view in the cycle (wraps Project → Remote).
func (v ViewMode) Next() ViewMode { return (v + 1) % 4 }

// String names the view for the header/hints.
func (v ViewMode) String() string {
	switch v {
	case ViewRemote:
		return "remote"
	case ViewLocal:
		return "local"
	case ViewCombined:
		return "combined"
	case ViewProject:
		return "project"
	}
	return "?"
}

// ActionKind is an intent the I/O loop fulfills (slice 2). The pure UI never
// performs I/O; it returns an Action describing what the user asked for.
type ActionKind int

const (
	ActNone ActionKind = iota
	ActQuit
	ActRefresh
	ActRun    // launch/run the selected agent
	ActSend   // send Task to the selected agent
	ActKill   // stop the selected agent
	ActRead   // read the selected agent's transcript
	ActAdopt  // adopt the selected (foreign) agent
	ActCreate // create a new held agent from Spec
)

// Action is what an Update returns for the loop to fulfill. Name/Task/Spec carry
// the operands the chosen ActionKind needs.
type Action struct {
	Kind ActionKind
	Name string
	Task string
	Spec core.Agent
}

// Screen is one interactive view. Update mutates the model and returns an Action
// for the loop; View renders the whole screen; Hints is the key-legend footer.
// Screens push/pop children via the Model's stack — no boolean mode-flags.
type Screen interface {
	Update(m *Model, k Key, r rune) Action
	View(m *Model) string
	Hints() string
}

// Model is the whole dashboard state: the reconciled inventory, the current
// view, cursor, terminal size, a status line, a text buffer (for input
// screens), and the screen stack.
type Model struct {
	Agents []core.Agent
	View   ViewMode
	Cursor int
	W, H   int
	Status string
	Buffer string
	stack  []Screen
}

// NewModel builds a model over the given inventory (no screens pushed yet).
func NewModel(agents []core.Agent) *Model {
	return &Model{Agents: agents, View: ViewRemote}
}

// Top returns the current (top-of-stack) screen, or nil if the stack is empty
// (the loop treats a nil Top as "quit").
func (m *Model) Top() Screen {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

func (m *Model) push(s Screen) { m.stack = append(m.stack, s) }

func (m *Model) pop() {
	if len(m.stack) > 0 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// Visible returns the agents shown by the current view — the pure lens applied
// to the inventory. Project/Combined show all (grouping/sectioning is a render
// concern); Local/Remote filter by machine.
func (m *Model) Visible() []core.Agent {
	switch m.View {
	case ViewLocal:
		return core.OnMachine(m.Agents, core.Local)
	case ViewRemote:
		return core.OnMachine(m.Agents, core.Remote)
	default: // Combined, Project
		return m.Agents
	}
}

// Selected returns the agent under the cursor within the current view, or
// (zero,false) if the cursor is out of range or the view is empty.
func (m *Model) Selected() (core.Agent, bool) {
	vis := m.Visible()
	if m.Cursor < 0 || m.Cursor >= len(vis) {
		return core.Agent{}, false
	}
	return vis[m.Cursor], true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: FAIL still — `listScreen`/`inputScreen` are referenced by the test but defined in Tasks 3/5. To let Task 2 compile & pass in isolation, add MINIMAL stub types at the end of `model.go` now (they get their real methods in Tasks 3/5):

```go
// listScreen and inputScreen are the two screens; their behavior is added in
// later tasks. Declared here so the stack (which holds Screens) compiles.
type listScreen struct{}
type inputScreen struct{}
```

But a `Screen` is an interface — the stubs must satisfy it to be pushed. Give them trivial method sets now (replaced in Tasks 3/5):

```go
func (s *listScreen) Update(m *Model, k Key, r rune) Action { return Action{} }
func (s *listScreen) View(m *Model) string                  { return "" }
func (s *listScreen) Hints() string                         { return "" }

func (s *inputScreen) Update(m *Model, k Key, r rune) Action { return Action{} }
func (s *inputScreen) View(m *Model) string                  { return "" }
func (s *inputScreen) Hints() string                         { return "" }
```

Tasks 3 and 5 will MOVE these into `list.go`/`input.go` with real bodies (and fields), deleting the stubs from `model.go`. Run again:
Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): Model, ViewMode cycle, Action, Screen interface + stack"
```

---

## Task 3: listScreen.Update — navigation, view cycling, action dispatch

**Files:**
- Create: `internal/ui/list.go` (move `listScreen` here from model.go, add fields + real Update/Hints)
- Modify: `internal/ui/model.go` (remove the `listScreen` stub + its stub methods)
- Test: `internal/ui/list_test.go`

`listScreen.Update` interprets keys: up/down (and j/k) move the cursor within the current view; ctrl-s cycles the view (and clamps the cursor); q/ctrl-c → ActQuit; r → ActRefresh; enter → ActRead (view the selected transcript); k(kill)/... letter shortcuts → the matching Action on the selected agent; n → push an inputScreen for Create. View/rendering is Task 4.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/list_test.go`:

```go
package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func listModel() *Model {
	m := NewModel([]core.Agent{
		{Name: "r1", Where: core.Remote, State: core.Running, Origin: core.Managed},
		{Name: "r2", Where: core.Remote, State: core.Done, Origin: core.Foreign},
	})
	m.push(&listScreen{})
	return m
}

func TestListNavigation(t *testing.T) {
	m := listModel()
	s := m.Top()
	s.Update(m, KeyDown, 0)
	if m.Cursor != 1 {
		t.Errorf("down → cursor 1, got %d", m.Cursor)
	}
	s.Update(m, KeyDown, 0) // clamp at last
	if m.Cursor != 1 {
		t.Errorf("down at end should clamp at 1, got %d", m.Cursor)
	}
	s.Update(m, KeyUp, 0)
	if m.Cursor != 0 {
		t.Errorf("up → cursor 0, got %d", m.Cursor)
	}
	s.Update(m, KeyUp, 0) // clamp at 0
	if m.Cursor != 0 {
		t.Errorf("up at top should clamp at 0, got %d", m.Cursor)
	}
	// j/k are down/up too (KeyRune 'j'/'k').
	s.Update(m, KeyRune, 'j')
	if m.Cursor != 1 {
		t.Errorf("j → cursor 1, got %d", m.Cursor)
	}
	s.Update(m, KeyRune, 'k')
	if m.Cursor != 0 {
		t.Errorf("k → cursor 0, got %d", m.Cursor)
	}
}

func TestListCycleViewResetsCursor(t *testing.T) {
	m := listModel()
	m.Cursor = 1
	m.Top().Update(m, KeyCycleView, 0)
	if m.View != ViewLocal {
		t.Errorf("ctrl-s should advance to Local, got %v", m.View)
	}
	// Local view is empty here; cursor must clamp to 0 (not dangle at 1).
	if m.Cursor != 0 {
		t.Errorf("cycling to an empty view should reset cursor to 0, got %d", m.Cursor)
	}
}

func TestListActionKeys(t *testing.T) {
	m := listModel() // cursor 0 → r1 (managed, running)
	cases := []struct {
		key  Key
		rune rune
		want ActionKind
	}{
		{KeyQuit, 0, ActQuit},
		{KeyRefresh, 0, ActRefresh},
		{KeyEnter, 0, ActRead},
		{KeyRune, 'k', ActKill}, // wait: k is up-nav above — see note
	}
	_ = cases
	// Quit
	if a := m.Top().Update(m, KeyQuit, 0); a.Kind != ActQuit {
		t.Errorf("q → ActQuit, got %v", a.Kind)
	}
	// Refresh
	if a := m.Top().Update(m, KeyRefresh, 0); a.Kind != ActRefresh {
		t.Errorf("r → ActRefresh, got %v", a.Kind)
	}
	// Enter → Read the selected agent
	m.Cursor = 0
	if a := m.Top().Update(m, KeyEnter, 0); a.Kind != ActRead || a.Name != "r1" {
		t.Errorf("enter → ActRead(r1), got %+v", a)
	}
	// 'x' → Kill the selected agent (x avoids the k/j nav collision)
	if a := m.Top().Update(m, KeyRune, 'x'); a.Kind != ActKill || a.Name != "r1" {
		t.Errorf("x → ActKill(r1), got %+v", a)
	}
	// 'g' → Run the selected agent (go)
	if a := m.Top().Update(m, KeyRune, 'g'); a.Kind != ActRun || a.Name != "r1" {
		t.Errorf("g → ActRun(r1), got %+v", a)
	}
}

func TestListAdoptOnlyForForeign(t *testing.T) {
	m := listModel()
	m.Cursor = 0 // r1 is managed
	if a := m.Top().Update(m, KeyRune, 'A'); a.Kind != ActNone {
		t.Errorf("adopt on a managed agent should be a no-op, got %v", a.Kind)
	}
	m.Cursor = 1 // r2 is foreign
	if a := m.Top().Update(m, KeyRune, 'A'); a.Kind != ActAdopt || a.Name != "r2" {
		t.Errorf("A on a foreign agent → ActAdopt(r2), got %+v", a)
	}
}

func TestListNewPushesInputScreen(t *testing.T) {
	m := listModel()
	before := m.Top()
	m.Top().Update(m, KeyRune, 'n')
	if m.Top() == before {
		t.Errorf("n should push a new screen (input), top unchanged")
	}
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("n should push an *inputScreen, got %T", m.Top())
	}
}
```

Note on key choices (documented, deliberate): `j`/`k` are nav (down/up), so kill uses `x`, run uses `g` (go), send uses `s`, adopt uses `A`, new uses `n`. These are the listScreen's interpretation of `KeyRune`; the decoder stays generic.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestList -v`
Expected: FAIL — `listScreen` has only the stub Update from Task 2.

- [ ] **Step 3: Write minimal implementation**

Remove the `listScreen` stub type and its three stub methods from `model.go`. Create `internal/ui/list.go`:

```go
package ui

import "github.com/divkov575/rbg/internal/core"

// listScreen is the base screen: the agent list under the current view lens,
// with cursor navigation, ctrl-s view cycling, and per-agent action keys.
type listScreen struct{}

// Update interprets keys for the list. Navigation/cycle mutate the model and
// return ActNone; action keys return an Action for the loop, carrying the
// selected agent's name. `n` pushes an input screen to compose a new task.
func (s *listScreen) Update(m *Model, k Key, r rune) Action {
	switch k {
	case KeyQuit:
		return Action{Kind: ActQuit}
	case KeyRefresh:
		return Action{Kind: ActRefresh}
	case KeyCycleView:
		m.View = m.View.Next()
		m.clampCursor()
		return Action{}
	case KeyUp:
		m.moveCursor(-1)
		return Action{}
	case KeyDown:
		m.moveCursor(1)
		return Action{}
	case KeyEnter:
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActRead, Name: a.Name}
		}
		return Action{}
	case KeyRune:
		return s.rune(m, r)
	}
	return Action{}
}

// rune interprets a printable key for the list (nav letters + action letters).
func (s *listScreen) rune(m *Model, r rune) Action {
	switch r {
	case 'j':
		m.moveCursor(1)
	case 'k':
		m.moveCursor(-1)
	case 'q':
		return Action{Kind: ActQuit}
	case 'r':
		return Action{Kind: ActRefresh}
	case 'g': // go: run the selected agent
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActRun, Name: a.Name}
		}
	case 'x': // kill
		if a, ok := m.Selected(); ok {
			return Action{Kind: ActKill, Name: a.Name}
		}
	case 's': // send: compose a follow-up (push input in send mode)
		if a, ok := m.Selected(); ok {
			m.push(newInputScreen(sendMode, a.Name))
		}
	case 'A': // adopt (foreign only)
		if a, ok := m.Selected(); ok && a.IsForeign() {
			return Action{Kind: ActAdopt, Name: a.Name}
		}
	case 'n': // new: compose a task to create
		m.push(newInputScreen(createMode, ""))
	}
	return Action{}
}

func (s *listScreen) Hints() string {
	return "j/k move · ctrl-s view · enter read · g run · s send · x kill · A adopt · n new · r refresh · q quit"
}

// moveCursor moves by delta and clamps to the current view's bounds.
func (m *Model) moveCursor(delta int) {
	m.Cursor += delta
	m.clampCursor()
}

// clampCursor keeps the cursor within [0, len(visible)-1], or 0 when empty.
func (m *Model) clampCursor() {
	n := len(m.Visible())
	if n == 0 {
		m.Cursor = 0
		return
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= n {
		m.Cursor = n - 1
	}
}

var _ Screen = (*listScreen)(nil)
var _ = core.Local // keep the core import if unused after edits (remove if not needed)
```

(Drop the `var _ = core.Local` line if `core` is otherwise referenced; it's only there to avoid an unused-import error if the file ends up not naming `core`. In practice `core` is used via `m.Selected()` returning `core.Agent`? No — that's in model.go. If `list.go` does not reference `core`, remove the import entirely.)

The `newInputScreen(mode, name)`, `sendMode`, `createMode` symbols are defined in Task 5 (`input.go`). To keep Task 3 compiling before Task 5, add temporary definitions at the bottom of `list.go` and MOVE them to `input.go` in Task 5:

```go
// (temporary until Task 5 defines the real input screen)
type inputMode int

const (
	createMode inputMode = iota
	sendMode
)

func newInputScreen(mode inputMode, target string) *inputScreen { return &inputScreen{} }
```

But `inputScreen` still has its Task-2 stub methods in model.go — remove the `inputScreen` stub from model.go too and define a minimal `inputScreen{}` with stub methods here in list.go temporarily, then Task 5 replaces it. To avoid churn, simplest: in Task 3 keep the `inputScreen` stub (with fields it will need) in `model.go`; Task 5 fills its methods. Concretely, in Task 3 change the `inputScreen` stub in model.go to carry fields:

```go
type inputScreen struct {
	mode   inputMode
	target string
}
```
and keep its stub methods until Task 5. And put `inputMode`/`createMode`/`sendMode`/`newInputScreen` in `list.go` as above.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestList -v && go test ./internal/ui/`
Expected: PASS (list nav/cycle/action/adopt/new tests; whole package green).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/list.go internal/ui/model.go internal/ui/list_test.go
git commit -m "feat(ui): listScreen — nav, ctrl-s view cycle, action-key dispatch"
```

---

## Task 4: views — the four lens renderers

**Files:**
- Create: `internal/ui/views.go`
- Modify: `internal/ui/list.go` (implement `listScreen.View` calling the renderers)
- Test: `internal/ui/views_test.go`

Pure renderers turn the model into text per view. Remote/Local are flat agent tables; Combined sections by machine; Project groups by repo (via `core.GroupByRepo`) with a sync badge per group. The cursor row is marked.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/views_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func viewModel(view ViewMode) *Model {
	m := NewModel([]core.Agent{
		{Name: "rem-run", Repo: "app", Where: core.Remote, State: core.Running, Origin: core.Managed, Sync: core.Behind},
		{Name: "loc-held", Repo: "app", Where: core.Local, State: core.Held, Origin: core.Managed},
		{Name: "wild", Repo: "lib", Where: core.Remote, State: core.Done, Origin: core.Foreign, Sync: core.Aligned},
	})
	m.View = view
	m.W, m.H = 80, 24
	return m
}

func TestRenderRemoteListsOnlyRemote(t *testing.T) {
	out := renderList(viewModel(ViewRemote))
	if !strings.Contains(out, "rem-run") || !strings.Contains(out, "wild") {
		t.Errorf("remote view should list remote agents:\n%s", out)
	}
	if strings.Contains(out, "loc-held") {
		t.Errorf("remote view should NOT list the local agent:\n%s", out)
	}
}

func TestRenderCombinedSectionsByMachine(t *testing.T) {
	out := renderList(viewModel(ViewCombined))
	if !strings.Contains(out, "LOCAL") || !strings.Contains(out, "REMOTE") {
		t.Errorf("combined view should have machine sections:\n%s", out)
	}
	for _, n := range []string{"rem-run", "loc-held", "wild"} {
		if !strings.Contains(out, n) {
			t.Errorf("combined view missing %q:\n%s", n, out)
		}
	}
}

func TestRenderProjectGroupsByRepoWithSyncBadge(t *testing.T) {
	out := renderList(viewModel(ViewProject))
	// repo names appear as group headers
	if !strings.Contains(out, "app") || !strings.Contains(out, "lib") {
		t.Errorf("project view should group by repo:\n%s", out)
	}
	// a sync badge for a behind repo is shown somewhere
	if !strings.Contains(strings.ToLower(out), "behind") {
		t.Errorf("project view should show a sync badge:\n%s", out)
	}
}

func TestRenderMarksCursorRow(t *testing.T) {
	m := viewModel(ViewRemote)
	m.Cursor = 1
	out := renderList(m)
	// the cursor marker (">") precedes the selected row's name
	lines := strings.Split(out, "\n")
	var marked string
	for _, ln := range lines {
		if strings.Contains(ln, ">") && (strings.Contains(ln, "rem-run") || strings.Contains(ln, "wild")) {
			marked = ln
		}
	}
	if !strings.Contains(marked, "wild") {
		t.Errorf("cursor at 1 should mark the second remote row (wild):\n%s", out)
	}
}

func TestSyncBadge(t *testing.T) {
	cases := map[core.Sync]string{
		core.Aligned:     "ok",
		core.Behind:      "behind",
		core.Ahead:       "ahead",
		core.Dirty:       "dirty",
		core.SyncUnknown: "",
	}
	for s, want := range cases {
		got := syncBadge(s)
		if want == "" {
			if got != "" {
				t.Errorf("syncBadge(%q) = %q, want empty", s, got)
			}
		} else if !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("syncBadge(%q) = %q, want to contain %q", s, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run "TestRender|TestSyncBadge" -v`
Expected: FAIL — `undefined: renderList`, `syncBadge`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/views.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// renderList renders the current view's body (the header/hints are added by the
// screen's View). It dispatches on m.View to the matching lens renderer.
func renderList(m *Model) string {
	switch m.View {
	case ViewCombined:
		return renderCombined(m)
	case ViewProject:
		return renderProject(m)
	default: // Remote, Local — a flat table of the visible agents
		return renderRows(m, m.Visible(), 0)
	}
}

// renderRows renders agents as aligned rows, marking the row at m.Cursor
// (cursor is an index into the current view's Visible() list; base is the
// starting global index of this block so multi-section views mark correctly).
func renderRows(m *Model, agents []core.Agent, base int) string {
	var b strings.Builder
	for i, a := range agents {
		marker := "  "
		if base+i == m.Cursor {
			marker = "> "
		}
		badge := syncBadge(a.Sync)
		fmt.Fprintf(&b, "%s%-20s  %-7s  %-8s  %-8s  %-10s  %s\n",
			marker, a.Name, a.Where, a.State, a.Origin, badge, a.Repo)
	}
	if len(agents) == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

// renderCombined sections the inventory by machine (local then remote). The
// cursor indexes the concatenation local++remote, matching Visible() order for
// Combined (which returns all agents in inventory order) — so this renderer
// re-derives sections but marks by global cursor over the SAME order Visible
// uses. To keep cursor semantics simple, Combined marks over m.Agents order.
func renderCombined(m *Model) string {
	var b strings.Builder
	b.WriteString("LOCAL\n")
	b.WriteString(renderRowsFiltered(m, core.Local))
	b.WriteString("REMOTE\n")
	b.WriteString(renderRowsFiltered(m, core.Remote))
	return b.String()
}

// renderRowsFiltered renders one machine's agents, marking the cursor by the
// agent's index within m.Visible() (Combined's Visible == m.Agents).
func renderRowsFiltered(m *Model, where core.Location) string {
	var b strings.Builder
	any := false
	for i, a := range m.Agents {
		if a.Where != where {
			continue
		}
		any = true
		marker := "  "
		if i == m.Cursor {
			marker = "> "
		}
		fmt.Fprintf(&b, "%s%-20s  %-8s  %-8s  %-10s  %s\n",
			marker, a.Name, a.State, a.Origin, syncBadge(a.Sync), a.Repo)
	}
	if !any {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

// renderProject groups agents by repo (core.GroupByRepo) with a per-group sync
// badge (taken from the group's first agent that has a known sync state).
func renderProject(m *Model) string {
	var b strings.Builder
	for _, g := range core.GroupByRepo(m.Agents) {
		repo := g.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		fmt.Fprintf(&b, "%s  %s\n", repo, groupSyncBadge(g))
		for _, a := range g.Agents {
			fmt.Fprintf(&b, "    %-20s  %-7s  %-8s  %-8s\n", a.Name, a.Where, a.State, a.Origin)
		}
	}
	return b.String()
}

// groupSyncBadge returns the badge for a repo group: the first non-unknown sync
// state among its agents (they share a checkout, so it's representative).
func groupSyncBadge(g core.RepoGroup) string {
	for _, a := range g.Agents {
		if b := syncBadge(a.Sync); b != "" {
			return b
		}
	}
	return ""
}

// syncBadge is a short human tag for a Sync state ("" for unknown).
func syncBadge(s core.Sync) string {
	switch s {
	case core.Aligned:
		return "[ok]"
	case core.Ahead:
		return "[ahead]"
	case core.Behind:
		return "[behind]"
	case core.Dirty:
		return "[dirty]"
	}
	return ""
}
```

In `internal/ui/list.go`, implement `View`:

```go
// View renders the list screen: a header (view name + counts), the body (the
// current lens), a status line, and the hints footer.
func (s *listScreen) View(m *Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "rbg — %s view  (%d agents)\n\n", m.View, len(m.Agents))
	b.WriteString(renderList(m))
	if m.Status != "" {
		fmt.Fprintf(&b, "\n%s\n", m.Status)
	}
	fmt.Fprintf(&b, "\n%s\n", s.Hints())
	return b.String()
}
```

Add `"fmt"` and `"strings"` imports to `list.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS (render + sync-badge tests; whole package green).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/views.go internal/ui/list.go internal/ui/views_test.go
git commit -m "feat(ui): four lens views (remote/local/combined/project) + sync badges"
```

---

## Task 5: inputScreen — text entry that exercises the stack

**Files:**
- Create: `internal/ui/input.go` (real `inputScreen` + `inputMode`/`newInputScreen`; remove the temporaries from `list.go`/`model.go`)
- Modify: `internal/ui/list.go` (remove the temporary `inputMode`/`newInputScreen`), `internal/ui/model.go` (remove the `inputScreen` stub)
- Test: `internal/ui/input_test.go`

The input screen collects a task string. In create mode, Enter returns `ActCreate` with a `Spec` (the composed agent); in send mode, Enter returns `ActSend{Name,Task}`. Esc pops back to the list with no action. Typing accumulates into `m.Buffer`; backspace deletes.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/input_test.go`:

```go
package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestInputTypingAndBackspace(t *testing.T) {
	m := NewModel(nil)
	s := newInputScreen(createMode, "")
	m.push(s)
	for _, r := range "hi" {
		s.Update(m, KeyRune, r)
	}
	if m.Buffer != "hi" {
		t.Errorf("buffer = %q, want hi", m.Buffer)
	}
	s.Update(m, KeyBackspace, 0)
	if m.Buffer != "h" {
		t.Errorf("after backspace, buffer = %q, want h", m.Buffer)
	}
}

func TestInputEscPopsNoAction(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	m.Buffer = "abandon me"
	a := s.Update(m, KeyEsc, 0)
	if a.Kind != ActNone {
		t.Errorf("esc should return ActNone, got %v", a.Kind)
	}
	if _, ok := m.Top().(*listScreen); !ok {
		t.Errorf("esc should pop back to the list, top is %T", m.Top())
	}
	if m.Buffer != "" {
		t.Errorf("esc should clear the buffer, got %q", m.Buffer)
	}
}

func TestInputCreateEnterReturnsSpec(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	for _, r := range "do the thing" {
		s.Update(m, KeyRune, r)
	}
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActCreate {
		t.Fatalf("create-mode enter → ActCreate, got %v", a.Kind)
	}
	if a.Spec.Task != "do the thing" {
		t.Errorf("spec task = %q, want 'do the thing'", a.Spec.Task)
	}
	// enter also pops back to the list and clears the buffer.
	if _, ok := m.Top().(*listScreen); !ok {
		t.Errorf("enter should pop back to the list, top is %T", m.Top())
	}
	if m.Buffer != "" {
		t.Errorf("enter should clear the buffer, got %q", m.Buffer)
	}
}

func TestInputSendEnterReturnsNameAndTask(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(sendMode, "target-agent")
	m.push(s)
	for _, r := range "next step" {
		s.Update(m, KeyRune, r)
	}
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActSend || a.Name != "target-agent" || a.Task != "next step" {
		t.Errorf("send-mode enter → ActSend(target-agent,next step), got %+v", a)
	}
}

func TestInputEmptyEnterDoesNothing(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	a := s.Update(m, KeyEnter, 0) // empty buffer
	if a.Kind != ActNone {
		t.Errorf("enter on empty buffer should be ActNone, got %v", a.Kind)
	}
	// stays on the input screen (didn't pop) so the user can type or esc.
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("empty enter should stay on input, top is %T", m.Top())
	}
	_ = core.Agent{}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestInput -v`
Expected: FAIL — the real `inputScreen` behavior/`newInputScreen` don't exist yet (stubs return empty).

- [ ] **Step 3: Write minimal implementation**

Remove the temporary `inputMode`/`createMode`/`sendMode`/`newInputScreen` from `list.go` and the `inputScreen` stub (type + methods) from `model.go`. Create `internal/ui/input.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// inputMode is what the input screen composes.
type inputMode int

const (
	createMode inputMode = iota // compose a task for a new held agent
	sendMode                    // compose a follow-up for a running agent
)

// inputScreen collects a task string, then returns ActCreate (create mode) or
// ActSend (send mode) on Enter, popping back to the list. Esc cancels. It
// exercises the screen stack — no boolean "inputting" flag on the Model.
type inputScreen struct {
	mode   inputMode
	target string // the agent name (send mode)
}

func newInputScreen(mode inputMode, target string) *inputScreen {
	return &inputScreen{mode: mode, target: target}
}

func (s *inputScreen) Update(m *Model, k Key, r rune) Action {
	switch k {
	case KeyEsc:
		m.Buffer = ""
		m.pop()
		return Action{}
	case KeyBackspace:
		if n := len(m.Buffer); n > 0 {
			m.Buffer = m.Buffer[:n-1]
		}
		return Action{}
	case KeyRune:
		m.Buffer += string(r)
		return Action{}
	case KeyEnter:
		task := strings.TrimSpace(m.Buffer)
		if task == "" {
			return Action{} // stay; nothing to submit
		}
		m.Buffer = ""
		m.pop()
		if s.mode == sendMode {
			return Action{Kind: ActSend, Name: s.target, Task: task}
		}
		return Action{Kind: ActCreate, Spec: core.Agent{Task: task}}
	}
	return Action{}
}

func (s *inputScreen) View(m *Model) string {
	title := "New task (will be created as a held agent)"
	if s.mode == sendMode {
		title = fmt.Sprintf("Follow-up to %q", s.target)
	}
	return fmt.Sprintf("%s\n\n> %s\n\n%s\n", title, m.Buffer, s.Hints())
}

func (s *inputScreen) Hints() string { return "type · enter submit · esc cancel" }

var _ Screen = (*inputScreen)(nil)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS (all input tests + the whole package: key/model/list/views/input).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/input.go internal/ui/list.go internal/ui/model.go internal/ui/input_test.go
git commit -m "feat(ui): inputScreen — task/follow-up entry via the screen stack"
```

---

## Task 6: Whole-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run the ui suite**

Run: `go test ./internal/ui/ -v`
Expected: PASS — key/model/list/views/input.

- [ ] **Step 2: Whole module build + test**

Run: `go build ./... && go test ./...`
Expected: PASS — new package compiles, nothing regressed (the old `tui` is untouched and still passes).

- [ ] **Step 3: Vet and format**

Run: `go vet ./internal/ui/ && gofmt -l internal/ui/`
Expected: vet clean; gofmt lists nothing. If gofmt lists a file, `gofmt -w` it and include below.

- [ ] **Step 4: Commit any fixups** (skip if none)

```bash
git add internal/ui/
git commit -m "test(ui): whole-package verification fixups"
```

---

## Self-Review Notes (traceability to the HLD)

- **§4.4 Screen stack replaces mode-flags:** `Screen` interface + `Model.stack` (push/pop/Top); the input screen is pushed/popped rather than gated by a boolean. Two real screens exercise the stack (not scaffolding). ✅
- **§4.3 Views as lenses:** `Visible()` uses `core.OnMachine`; Project uses `core.GroupByRepo`; `ctrl-s`/`Next()` cycles Remote→Local→Combined→Project. ✅
- **§5.6 Action vocabulary:** `ActRun/ActSend/ActKill/ActRead/ActAdopt/ActCreate/ActRefresh/ActQuit`, gated by the selected agent (adopt only for foreign). The loop (slice 2) fulfills them. ✅
- **Purity (NFR):** every `Update`/`View`/renderer is a pure function of `(Model, Key)` / `Model` — zero I/O; all tests are table tests with no terminal/SSH/engine. ✅
- **One decoder, not five:** `DecodeKey` is the single decoder; screens interpret `KeyRune` themselves — the old 5-decoder sprawl is gone. ✅

**Design decisions (documented, deliberate):**
- Cursor indexes the current view's `Visible()` list; cycling clamps it. Combined view marks the cursor over `m.Agents` order (its `Visible()` returns all agents in inventory order), so the section renderers mark by global index — kept simple, verified by `TestRenderMarksCursorRow`.
- Action keys avoid the `j`/`k` nav collision: `g`=run(go), `x`=kill, `s`=send, `A`=adopt, `n`=new, enter=read. Documented in `Hints()`.
- `inputScreen` returns `ActCreate` with a `Spec` carrying only `Task`; the loop/engine fill the rest (name derivation, repo, where) — the UI doesn't invent a name. (Repo/where selection is a later refinement — a create flow could push a second input; out of scope here.)
- Create currently composes only a task (no repo/where picker) — the engine's `Create` requires a name+task, so slice 2's loop will derive a name (e.g. via slug) or push a follow-up prompt. Flagged for slice 2.

**Deferred to slice 2 (not gaps here):** wiring `Action`s to `*engine.Engine` ops; the raw-terminal loop (copy `rawMode`/`termSize`/`readRaw` from `tui`); transcript display (ActRead → a read/preview screen showing `engine.Read` output); config/dir-browse screens if wanted; the `cmd/rbg` `dash` swap; retiring `internal/tui`/`client`/`queue`. Also: a name/repo/where picker for Create.

**Type/name consistency:** `Key`/`DecodeKey`, `Model`/`NewModel`/`Visible`/`Selected`/`Top`/`push`/`pop`/`moveCursor`/`clampCursor`, `ViewMode`/`Next`, `Action`/`ActionKind`, `Screen`, `listScreen`, `inputScreen`/`newInputScreen`/`inputMode`/`createMode`/`sendMode`, `renderList`/`renderRows`/`renderCombined`/`renderProject`/`syncBadge`/`groupSyncBadge` — used identically across tasks and matching the verified `core` signatures. ✅
