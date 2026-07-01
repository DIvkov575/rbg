# rbg Phase 4 Slice 2 — Wire the pure UI to the engine, retire old packages

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the shipped `rbg` dashboard run on the new pure `internal/ui` (screen-stack + four lens views) driven by `*engine.Engine`, and delete the old `internal/tui`, `internal/client`, and `internal/queue` packages.

**Architecture:** `internal/ui` gets (a) a read-only pager screen to fulfill `ActRead`, (b) relocated raw-terminal primitives (moved out of `internal/tui`), and (c) a `Run` loop that fetches the reconciled inventory from an `Ops` interface (satisfied by `*engine.Engine`), draws the top screen, decodes one key at a time, and fulfills each returned `Action` against the engine. `cmd/rbg` swaps `dash` onto `ui.Run`, rebases the `ping` verb onto `sshx.Reachable`, and the three old packages are removed.

**Tech Stack:** Go 1.26, stdlib only. Existing packages: `internal/ui` (pure), `internal/engine` (`List/Create/Run/Send/Read/Kill/Adopt`), `internal/core`, `internal/sshx`, `internal/config`, `internal/run`.

**Deliberate scope boundary (functional reduction):** the OLD dashboard's in-dash directory browser, task queue, in-dash config editor, and attach-from-dashboard are old-architecture features with no equivalent Screen in the new UI. They are dropped by this slice (the `rbg attach`/`deploy`/`ping` CLI verbs remain). Re-adding any as a new Screen is future work, not part of finishing the rewrite.

---

## File Structure

- **Create** `internal/ui/pager.go` — `pagerScreen` (read-only, scrollable transcript view; fulfills `ActRead`).
- **Create** `internal/ui/pager_test.go` — pager unit tests.
- **Create** `internal/ui/term.go` — `Stdio` struct + `readRaw` (moved from `internal/tui/term.go`, trimmed to what the new loop needs).
- **Create** `internal/ui/term_darwin.go` — `rawMode`/`termSize`/`ioctl`/`winsize` (moved verbatim from `internal/tui/term_darwin.go`, `package ui`).
- **Create** `internal/ui/term_linux.go` — same for linux (moved verbatim).
- **Create** `internal/ui/loop.go` — `Ops` interface, `applyAction` (pure-ish, testable), `Run` (raw-terminal driver).
- **Create** `internal/ui/loop_test.go` — `applyAction` tests with a fake `Ops`.
- **Modify** `internal/ui/model.go` — add `SetAgents`; `NewModel` pushes the base `listScreen`.
- **Modify** `internal/ui/model_test.go` — cover `SetAgents` + initial screen.
- **Rewrite** `cmd/rbg/dash.go` — build an engine, call `ui.Run`; drop all `client`/`queue`/`tui` usage and the `dispatchLocal`/`localRepoDir`/`queuePath`/`confPath` helpers that only served the old dash.
- **Modify** `cmd/rbg/main.go` — replace `client.Ping` with an inline `sshx.Reachable` ping; drop the `client` import.
- **Delete** `internal/tui/`, `internal/client/`, `internal/queue/` (whole directories).

---

## Task 1: Model — SetAgents + initial screen

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/model_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
func TestNewModelPushesListScreen(t *testing.T) {
	m := NewModel(nil)
	if m.Top() == nil {
		t.Fatal("NewModel should push the base list screen so the loop has a screen")
	}
	if _, ok := m.Top().(*listScreen); !ok {
		t.Errorf("top screen = %T, want *listScreen", m.Top())
	}
}

func TestSetAgentsReplacesInventoryAndClampsCursor(t *testing.T) {
	m := NewModel([]core.Agent{
		{Name: "a", Where: core.Remote}, {Name: "b", Where: core.Remote},
	})
	m.View = ViewRemote
	m.Cursor = 1
	m.SetAgents([]core.Agent{{Name: "only", Where: core.Remote}})
	if len(m.Agents) != 1 {
		t.Fatalf("Agents not replaced: %d", len(m.Agents))
	}
	if m.Cursor != 0 {
		t.Errorf("Cursor should clamp to 0 after shrink, got %d", m.Cursor)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run 'TestNewModelPushesListScreen|TestSetAgents' -v`
Expected: FAIL (compile error: `SetAgents` undefined, and `Top()` nil since `NewModel` pushes nothing).

- [ ] **Step 3: Implement**

In `internal/ui/model.go`, change `NewModel` to push the base screen, and add `SetAgents`:

```go
// NewModel builds a model over the given inventory and pushes the base list
// screen, so the loop always has a Top() to drive.
func NewModel(agents []core.Agent) *Model {
	m := &Model{Agents: agents, View: ViewRemote}
	m.push(&listScreen{})
	return m
}

// SetAgents replaces the inventory (e.g. after a refresh) and re-clamps the
// cursor to the new visible bounds.
func (m *Model) SetAgents(agents []core.Agent) {
	m.Agents = agents
	m.clampCursor()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/ui/ -run 'TestNewModelPushesListScreen|TestSetAgents' -v`
Expected: PASS. Then `go test ./internal/ui/` — all existing ui tests still PASS (they build a `*Model` directly and may push their own screens; `NewModel` pushing a listScreen must not break them — verify).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): NewModel pushes base list screen; add SetAgents"
```

---

## Task 2: pagerScreen — read-only transcript view

**Files:**
- Create: `internal/ui/pager.go`
- Test: `internal/ui/pager_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/pager_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func TestPagerScrollsAndClampsToBounds(t *testing.T) {
	m := NewModel(nil)
	m.H = 6 // small window
	p := newPagerScreen("transcript", []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7"})
	m.push(p)

	// scrolling up at the top is a no-op (offset stays 0)
	p.Update(m, KeyUp, 0)
	if p.offset != 0 {
		t.Errorf("offset = %d, want 0 (already at top)", p.offset)
	}
	// down advances the offset
	p.Update(m, KeyDown, 0)
	if p.offset != 1 {
		t.Errorf("offset after one down = %d, want 1", p.offset)
	}
	// 'j'/'k' behave like down/up
	p.Update(m, KeyRune, 'j')
	if p.offset != 2 {
		t.Errorf("offset after j = %d, want 2", p.offset)
	}
	p.Update(m, KeyRune, 'k')
	if p.offset != 1 {
		t.Errorf("offset after k = %d, want 1", p.offset)
	}
}

func TestPagerEscPops(t *testing.T) {
	m := NewModel(nil)
	base := m.Top()
	p := newPagerScreen("t", []string{"x"})
	m.push(p)
	if m.Top() != p {
		t.Fatal("pager should be on top after push")
	}
	act := p.Update(m, KeyEsc, 0)
	if act.Kind != ActNone {
		t.Errorf("Esc should return ActNone, got %v", act.Kind)
	}
	if m.Top() != base {
		t.Errorf("Esc should pop back to the base screen")
	}
}

func TestPagerViewShowsTitleAndLines(t *testing.T) {
	m := NewModel(nil)
	m.H = 10
	p := newPagerScreen("my transcript", []string{"hello", "world"})
	out := p.View(m)
	if !strings.Contains(out, "my transcript") || !strings.Contains(out, "hello") {
		t.Errorf("pager view missing title/content: %q", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run TestPager -v`
Expected: FAIL (compile error: `newPagerScreen` undefined).

- [ ] **Step 3: Implement**

Create `internal/ui/pager.go`:

```go
package ui

import (
	"fmt"
	"strings"
)

// pagerScreen is a read-only, scrollable text view used to display a transcript
// (fulfilling ActRead). It owns its own lines and scroll offset; Esc/q pops it
// off the stack back to the list. It performs no I/O — the loop hands it the
// already-fetched text.
type pagerScreen struct {
	title  string
	lines  []string
	offset int
}

func newPagerScreen(title string, lines []string) *pagerScreen {
	return &pagerScreen{title: title, lines: lines}
}

// window is how many text lines fit below the title/hints chrome.
func (s *pagerScreen) window(m *Model) int {
	h := m.H - 4 // title line + blank + hints + margin
	if h < 1 {
		h = 1
	}
	return h
}

// maxOffset is the largest first-line index that still shows a full window
// (or 0 when everything fits).
func (s *pagerScreen) maxOffset(m *Model) int {
	max := len(s.lines) - s.window(m)
	if max < 0 {
		return 0
	}
	return max
}

func (s *pagerScreen) Update(m *Model, k Key, r rune) Action {
	switch {
	case k == KeyUp || (k == KeyRune && r == 'k'):
		if s.offset > 0 {
			s.offset--
		}
	case k == KeyDown || (k == KeyRune && r == 'j'):
		if s.offset < s.maxOffset(m) {
			s.offset++
		}
	case k == KeyEsc || k == KeyQuit || (k == KeyRune && r == 'q'):
		m.pop()
	}
	return Action{}
}

func (s *pagerScreen) View(m *Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", s.title)
	win := s.window(m)
	end := s.offset + win
	if end > len(s.lines) {
		end = len(s.lines)
	}
	for i := s.offset; i < end; i++ {
		b.WriteString(s.lines[i])
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%s\n", s.Hints())
	return b.String()
}

func (s *pagerScreen) Hints() string { return "j/k scroll · esc back · q back" }

var _ Screen = (*pagerScreen)(nil)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/ui/ -run TestPager -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/pager.go internal/ui/pager_test.go
git commit -m "feat(ui): read-only pager screen for transcripts (fulfills ActRead)"
```

---

## Task 3: Relocate terminal primitives into internal/ui

**Files:**
- Create: `internal/ui/term.go`, `internal/ui/term_darwin.go`, `internal/ui/term_linux.go`

These are moved copies of the `internal/tui` primitives (the originals are deleted in Task 7). No behavior change — just relocation into `package ui` so the loop can use them without importing the doomed `tui` package.

- [ ] **Step 1: Create `internal/ui/term.go`**

```go
package ui

import (
	"io"
	"os"
)

// Stdio bundles the loop's I/O endpoints (injectable for testing).
type Stdio struct {
	In  io.Reader
	Out io.Writer
}

// DefaultStdio uses the process terminal.
func DefaultStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout} }

// readRaw reads one input chunk; nil/empty means EOF (treated as quit).
func readRaw(r io.Reader) []byte {
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}
```

- [ ] **Step 2: Create `internal/ui/term_darwin.go`** (verbatim from `internal/tui/term_darwin.go`, only the package line differs)

```go
//go:build darwin

package ui

import (
	"syscall"
	"unsafe"
)

// rawMode puts fd into raw mode and returns a restore func. Darwin uses
// TIOCGETA/TIOCSETA for the termios get/set ioctls.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TIOCSETA, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TIOCSETA, &old) }, nil
}

func ioctl(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

// winsize mirrors struct winsize for the TIOCGWINSZ ioctl.
type winsize struct {
	rows, cols, xpix, ypix uint16
}

// termSize returns the terminal (cols, rows) for fd, or (0,0) if unavailable.
func termSize(fd uintptr) (int, int) {
	var ws winsize
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if e != 0 {
		return 0, 0
	}
	return int(ws.cols), int(ws.rows)
}
```

- [ ] **Step 3: Create `internal/ui/term_linux.go`** (verbatim from `internal/tui/term_linux.go`, package line differs)

```go
//go:build linux

package ui

import (
	"syscall"
	"unsafe"
)

// rawMode puts fd into raw mode and returns a restore func. Linux uses
// TCGETS/TCSETS for the termios get/set ioctls.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, &old) }, nil
}

func ioctl(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

// winsize mirrors struct winsize for the TIOCGWINSZ ioctl.
type winsize struct {
	rows, cols, xpix, ypix uint16
}

// termSize returns the terminal (cols, rows) for fd, or (0,0) if unavailable.
func termSize(fd uintptr) (int, int) {
	var ws winsize
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if e != 0 {
		return 0, 0
	}
	return int(ws.cols), int(ws.rows)
}
```

- [ ] **Step 4: Verify it builds** (both files reference `readRaw`/`Stdio` only via the loop later; for now just compile)

Run: `go build ./internal/ui/`
Expected: builds clean (the new term files are self-contained; `readRaw`/`Stdio` unused until Task 4 — Go allows unused funcs, only unused imports/locals fail, so this is fine).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/term.go internal/ui/term_darwin.go internal/ui/term_linux.go
git commit -m "feat(ui): relocate raw-terminal primitives into internal/ui"
```

---

## Task 4: The loop — Ops interface, applyAction, Run

**Files:**
- Create: `internal/ui/loop.go`
- Test: `internal/ui/loop_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/loop_test.go`:

```go
package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// fakeOps records calls and returns canned data.
type fakeOps struct {
	agents   []core.Agent
	listErr  error
	ran      string
	sent     [2]string
	killed   string
	adopted  string
	created  core.Agent
	readName string
	readData []byte
}

func (f *fakeOps) List() ([]core.Agent, error)          { return f.agents, f.listErr }
func (f *fakeOps) Create(a core.Agent) (core.Agent, error) { f.created = a; return a, nil }
func (f *fakeOps) Run(name string) error                { f.ran = name; return nil }
func (f *fakeOps) Send(name, task string) error         { f.sent = [2]string{name, task}; return nil }
func (f *fakeOps) Read(name string) ([]byte, error)     { f.readName = name; return f.readData, nil }
func (f *fakeOps) Kill(name string) error               { f.killed = name; return nil }
func (f *fakeOps) Adopt(name string) error              { f.adopted = name; return nil }

func TestApplyActionQuitReturnsTrue(t *testing.T) {
	m := NewModel(nil)
	if quit := applyAction(m, &fakeOps{}, Action{Kind: ActQuit}); !quit {
		t.Error("ActQuit should signal quit")
	}
}

func TestApplyActionRunCallsEngineAndRefreshes(t *testing.T) {
	ops := &fakeOps{agents: []core.Agent{{Name: "after", Where: core.Remote}}}
	m := NewModel([]core.Agent{{Name: "before", Where: core.Remote}})
	quit := applyAction(m, ops, Action{Kind: ActRun, Name: "job"})
	if quit {
		t.Error("ActRun should not quit")
	}
	if ops.ran != "job" {
		t.Errorf("Run called with %q, want job", ops.ran)
	}
	// After a mutating action the inventory is refreshed from List().
	if len(m.Agents) != 1 || m.Agents[0].Name != "after" {
		t.Errorf("inventory not refreshed after Run: %+v", m.Agents)
	}
}

func TestApplyActionReadPushesPager(t *testing.T) {
	ops := &fakeOps{readData: []byte("line1\nline2")}
	m := NewModel(nil)
	applyAction(m, ops, Action{Kind: ActRead, Name: "foo"})
	if ops.readName != "foo" {
		t.Errorf("Read called with %q, want foo", ops.readName)
	}
	if _, ok := m.Top().(*pagerScreen); !ok {
		t.Errorf("ActRead should push a pager, top is %T", m.Top())
	}
}

func TestApplyActionSendKillAdoptCreate(t *testing.T) {
	ops := &fakeOps{}
	m := NewModel(nil)
	applyAction(m, ops, Action{Kind: ActSend, Name: "n", Task: "t"})
	if ops.sent != [2]string{"n", "t"} {
		t.Errorf("Send got %v", ops.sent)
	}
	applyAction(m, ops, Action{Kind: ActKill, Name: "k"})
	if ops.killed != "k" {
		t.Errorf("Kill got %q", ops.killed)
	}
	applyAction(m, ops, Action{Kind: ActAdopt, Name: "a"})
	if ops.adopted != "a" {
		t.Errorf("Adopt got %q", ops.adopted)
	}
	applyAction(m, ops, Action{Kind: ActCreate, Spec: core.Agent{Task: "do"}})
	if ops.created.Task != "do" {
		t.Errorf("Create got %+v", ops.created)
	}
}

func TestApplyActionSetsErrorStatusOnFailure(t *testing.T) {
	ops := &fakeOps{}
	// Read of a name the fake errors on: make Read fail by overriding via a
	// closure fake is overkill; instead assert Run error path with a failing ops.
	failing := &failOps{}
	m := NewModel(nil)
	applyAction(m, failing, Action{Kind: ActRun, Name: "x"})
	if m.Status == "" {
		t.Error("a failed action should set a status message")
	}
}

// failOps returns an error from every mutating call.
type failOps struct{ fakeOps }

func (f *failOps) Run(name string) error { return errBoom }
```

Add at the bottom of the test file:

```go
var errBoom = errStr("boom")

type errStr string

func (e errStr) Error() string { return string(e) }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run TestApplyAction -v`
Expected: FAIL (compile error: `applyAction` undefined).

- [ ] **Step 3: Implement**

Create `internal/ui/loop.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// Ops is the engine surface the dashboard drives. *engine.Engine satisfies it
// (same method set as the CLI's Ops), so the loop needs no real SSH in tests.
type Ops interface {
	List() ([]core.Agent, error)
	Create(spec core.Agent) (core.Agent, error)
	Run(name string) error
	Send(name, task string) error
	Read(name string) ([]byte, error)
	Kill(name string) error
	Adopt(name string) error
}

// refresh re-pulls the reconciled inventory into the model, surfacing a
// degradation error as a status line but still showing whatever came back.
func refresh(m *Model, ops Ops) {
	agents, err := ops.List()
	m.SetAgents(agents)
	if err != nil {
		m.Status = "inventory may be incomplete: " + err.Error()
	}
}

// applyAction fulfills one Action against the engine and returns true when the
// loop should exit. Mutating actions refresh the inventory so the list reflects
// the new state; ActRead pushes a pager over the fetched transcript. Errors go
// to the status line rather than aborting the dashboard.
func applyAction(m *Model, ops Ops, act Action) bool {
	switch act.Kind {
	case ActQuit:
		return true
	case ActRefresh:
		refresh(m, ops)
	case ActRun:
		if err := ops.Run(act.Name); err != nil {
			m.Status = "run failed: " + err.Error()
		} else {
			m.Status = "ran " + act.Name
		}
		refresh(m, ops)
	case ActSend:
		if err := ops.Send(act.Name, act.Task); err != nil {
			m.Status = "send failed: " + err.Error()
		} else {
			m.Status = "sent to " + act.Name
		}
		refresh(m, ops)
	case ActKill:
		if err := ops.Kill(act.Name); err != nil {
			m.Status = "kill failed: " + err.Error()
		} else {
			m.Status = "killed " + act.Name
		}
		refresh(m, ops)
	case ActAdopt:
		if err := ops.Adopt(act.Name); err != nil {
			m.Status = "adopt failed: " + err.Error()
		} else {
			m.Status = "adopted " + act.Name
		}
		refresh(m, ops)
	case ActCreate:
		if _, err := ops.Create(act.Spec); err != nil {
			m.Status = "create failed: " + err.Error()
		} else {
			m.Status = "created a held agent"
		}
		refresh(m, ops)
	case ActRead:
		data, err := ops.Read(act.Name)
		if err != nil {
			m.Status = "read failed: " + err.Error()
			return false
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		m.push(newPagerScreen("transcript: "+act.Name, lines))
	}
	return false
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/ui/ -run TestApplyAction -v`
Expected: PASS.

- [ ] **Step 5: Add the raw-terminal `Run` driver (no unit test — it needs a real tty; covered by the smoke test in Task 8)**

Append to `internal/ui/loop.go`:

```go
import (
	"os"          // add to the import block
)

// Run drives the dashboard until the user quits. It pulls the initial
// inventory, enters raw mode, and on each key: decodes it, hands it to the top
// screen's Update, fulfills the returned Action, and redraws. EOF quits.
func Run(ops Ops, io Stdio) error {
	m := NewModel(nil)
	refresh(m, ops)
	w, h := termSize(os.Stdin.Fd())
	m.W, m.H = w, h

	restore, err := rawMode(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer restore()

	draw(io.Out, m)
	for {
		raw := readRaw(io.In)
		if raw == nil {
			return nil // EOF → quit
		}
		top := m.Top()
		if top == nil {
			return nil
		}
		k, r := DecodeKey(raw)
		act := top.Update(m, k, r)
		if applyAction(m, ops, act) {
			return nil
		}
		w, h := termSize(os.Stdin.Fd())
		m.W, m.H = w, h
		draw(io.Out, m)
	}
}

const clearScreen = "\x1b[2J\x1b[H"

// draw clears the screen and renders the top screen.
func draw(out interface{ Write([]byte) (int, error) }, m *Model) {
	top := m.Top()
	if top == nil {
		return
	}
	fmt.Fprint(out, clearScreen)
	fmt.Fprint(out, top.View(m))
}
```

Note: fold the `os` import into the existing import block (don't write two `import` statements). The `fmt` import is already present from Step 3.

- [ ] **Step 6: Verify build + full ui tests**

Run: `go build ./internal/ui/ && go test ./internal/ui/`
Expected: builds clean, all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/loop.go internal/ui/loop_test.go
git commit -m "feat(ui): engine-driven Run loop + testable applyAction"
```

---

## Task 5: Swap cmd/rbg dash onto ui.Run

**Files:**
- Rewrite: `cmd/rbg/dash.go`

- [ ] **Step 1: Replace the whole file**

Replace `cmd/rbg/dash.go` with:

```go
package main

import (
	"fmt"
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/ui"
)

// dash launches the interactive dashboard over the engine-backed UI. It builds
// the same *engine.Engine the scriptable CLI uses, so the dashboard manages the
// exact same reconciled inventory (create/run/send/read/kill/adopt across the
// four lens views).
func dash(cfg *config.Config, r run.Runner) int {
	e, err := buildEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		return 2
	}
	if err := ui.Run(e, ui.DefaultStdio()); err != nil {
		fmt.Fprintf(os.Stderr, "rbg: dashboard: %v\n", err)
		return 1
	}
	return 0
}
```

`buildEngine()` already exists in `cmd/rbg/main.go` and returns `(*engine.Engine, error)`. `*engine.Engine` satisfies `ui.Ops` (same method set as `cli.Ops`).

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/rbg/`
Expected: FAIL — `dash.go` no longer references `client`/`queue`/`tui`, but `main.go` still imports `client` (for Ping) and the deleted helpers may still be referenced. This is expected; Task 6 fixes `main.go`. If the only errors are about unused imports in `main.go` or the now-removed `dispatchLocal`/`localRepoDir`, proceed. (If `dash.go` itself has errors, fix them before moving on.)

- [ ] **Step 3: Commit (after Task 6 makes it build — see note)**

Do not commit yet; `cmd/rbg` won't build until Task 6. Proceed to Task 6, then commit both together.

---

## Task 6: Rebase ping onto sshx; drop client from main.go

**Files:**
- Modify: `cmd/rbg/main.go`

- [ ] **Step 1: Replace the `client.Ping` call**

In `cmd/rbg/main.go`, the `runLegacy` function has:

```go
	case "ping":
		return client.Ping(cfg, r, os.Stdout)
```

Replace with an inline ping (add a small helper at the bottom of `main.go`):

```go
	case "ping":
		return doPing(cfg, r)
```

Add this function to `cmd/rbg/main.go`:

```go
// doPing reports whether the desktop is reachable, replacing the retired
// client.Ping (a thin wrapper over sshx.Reachable).
func doPing(cfg *config.Config, r run.Runner) int {
	if sshx.Reachable(cfg, r) {
		fmt.Fprintf(os.Stdout, "%s: reachable\n", cfg.Host)
		return 0
	}
	fmt.Fprintf(os.Stderr, "cannot reach '%s' — disconnected\n", cfg.Host)
	return 1
}
```

- [ ] **Step 2: Remove the `client` import**

In `cmd/rbg/main.go`, delete the line:

```go
	"github.com/divkov575/rbg/internal/client"
```

`sshx` is already imported in `main.go` (used by `attach`). Confirm with `grep -n sshx cmd/rbg/main.go`.

- [ ] **Step 3: Verify it builds**

Run: `go build ./cmd/rbg/`
Expected: builds clean now (dash.go from Task 5 + main.go here). If there are leftover references to removed helpers (`dispatchLocal`, `localRepoDir`, `queuePath`, `confPath`), they lived in the OLD `dash.go` which Task 5 replaced — so they're gone. `go vet ./cmd/rbg/` should be clean.

- [ ] **Step 4: Run existing cmd/rbg tests**

Run: `go test ./cmd/rbg/`
Expected: PASS. (`main_test.go` tests `isScriptable`, `migrationHint`, `claudeSessionIDFor`, `localRepoDir`.) **`localRepoDir` was deleted** with the old dash.go, so `TestLocalRepoDir` will fail to compile. Fix: delete `TestLocalRepoDir` from `cmd/rbg/main_test.go` (it tested a helper that only served the retired local-dispatch path).

- [ ] **Step 5: Re-run tests**

Run: `go test ./cmd/rbg/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/rbg/dash.go cmd/rbg/main.go cmd/rbg/main_test.go
git commit -m "feat(cmd): dashboard on engine-backed ui.Run; ping via sshx; drop client dep"
```

---

## Task 7: Delete the retired packages

**Files:**
- Delete: `internal/tui/`, `internal/client/`, `internal/queue/`

- [ ] **Step 1: Confirm nothing references them**

Run: `grep -rn "internal/tui\|internal/client\|internal/queue" --include='*.go' .`
Expected: NO matches (Task 5 and 6 removed the last references). If any remain, fix before deleting.

- [ ] **Step 2: Delete the directories**

```bash
git rm -r internal/tui internal/client internal/queue
```

- [ ] **Step 3: Verify the whole module builds and tests**

Run: `go build ./... && go test ./...`
Expected: builds clean; all packages PASS.

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: retire internal/tui, internal/client, internal/queue (superseded by ui+engine)"
```

---

## Task 8: Whole-slice verification

**Files:** none (verification only)

- [ ] **Step 1: Full gate**

Run: `go vet ./... && go test -race ./...`
Expected: vet clean; all packages PASS under the race detector.

- [ ] **Step 2: Confirm the dashboard binary wiring**

Run: `go build -o /tmp/rbg-verify ./cmd/rbg && /tmp/rbg-verify help`
Expected: help text prints (confirms the binary links with the new dash path). Do NOT launch the interactive dash in a non-tty (it needs raw mode); the help path exercises linking.

- [ ] **Step 3: Grep for orphans**

Run: `grep -rn "tui\.\|client\.\|queue\." --include='*.go' cmd/ internal/ | grep -v internal/ui | grep -v _test`
Expected: no references to the retired packages.

- [ ] **Step 4: Final commit (if any stray fixes were needed)**

```bash
git add -A && git commit -m "test: whole-slice verification for the UI wiring" || echo "nothing to commit"
```

---

## Self-Review

**Spec coverage:** wire ui→engine (Tasks 1–4) ✓; pager for ActRead (Task 2) ✓; relocate term primitives (Task 3) ✓; swap dash (Task 5) ✓; ping without client (Task 6) ✓; retire tui/client/queue (Task 7) ✓; verification (Task 8) ✓.

**Type consistency:** `Ops` method set matches `*engine.Engine` (`List/Create/Run/Send/Read/Kill/Adopt`) and the existing `cli.Ops`. `newPagerScreen(title, lines)`, `applyAction(m, ops, act) bool`, `refresh(m, ops)`, `Run(ops, io)`, `SetAgents(agents)` used consistently across tasks.

**Placeholders:** none — every code step is complete.

**Known functional reduction (flagged, intended):** dir browser / queue / in-dash config editor / dashboard-attach are dropped with the old packages; CLI verbs `attach`/`deploy`/`ping` remain.
