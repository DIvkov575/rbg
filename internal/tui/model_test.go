package tui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/session"
)

func sample() Model {
	return New([]session.Session{
		{Name: "alpha", ClaudeSessionID: "sid-1"},
		{Name: "beta", ClaudeSessionID: "sid-2"},
		{Name: "gamma", ClaudeSessionID: "sid-3"},
	})
}

func TestDownUpMovesSelection(t *testing.T) {
	m := sample()
	if m.Selected != 0 {
		t.Fatalf("start selected = %d", m.Selected)
	}
	m, _ = Update(m, KeyDown)
	m, _ = Update(m, KeyDown)
	if m.Selected != 2 {
		t.Fatalf("after 2 downs selected = %d", m.Selected)
	}
	m, _ = Update(m, KeyDown) // clamp at bottom
	if m.Selected != 2 {
		t.Fatalf("should clamp at last, got %d", m.Selected)
	}
	m, _ = Update(m, KeyUp)
	if m.Selected != 1 {
		t.Fatalf("after up selected = %d", m.Selected)
	}
}

func TestMovementLoadsTranscriptIntoPane(t *testing.T) {
	m := sample()
	// moving the selection emits ActionLoadTranscript so the loop fetches the
	// now-selected agent's transcript.
	m, act := Update(m, KeyDown)
	if act != ActionLoadTranscript {
		t.Fatalf("KeyDown action = %v, want ActionLoadTranscript", act)
	}
	if m.Selected != 1 {
		t.Fatalf("KeyDown should move selection, got %d", m.Selected)
	}
	m, act = Update(m, KeyUp)
	if act != ActionLoadTranscript {
		t.Fatalf("KeyUp action = %v, want ActionLoadTranscript", act)
	}
	if m.Selected != 0 {
		t.Fatalf("KeyUp should move selection, got %d", m.Selected)
	}
	// the loop fulfills the action by calling SetTranscript:
	m = m.SetTranscript("user: hi\nassistant: yo\n")
	if !strings.Contains(View(m), "assistant: yo") {
		t.Fatalf("transcript not rendered in view:\n%s", View(m))
	}
}

func TestMovementOnEmptyListIsNoop(t *testing.T) {
	m := New(nil)
	_, act := Update(m, KeyDown)
	if act != ActionNone {
		t.Fatalf("KeyDown on empty list action = %v, want ActionNone", act)
	}
}

func TestAttachKeyYieldsAttachAction(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyDown) // select beta
	_, act := Update(m, KeyAttach)
	if act != ActionAttach {
		t.Fatalf("KeyAttach action = %v, want ActionAttach", act)
	}
}

func TestRefreshAndQuitActions(t *testing.T) {
	m := sample()
	if _, act := Update(m, KeyRefresh); act != ActionRefresh {
		t.Fatalf("refresh action = %v", act)
	}
	if _, act := Update(m, KeyQuit); act != ActionQuit {
		t.Fatalf("quit action = %v", act)
	}
}

func TestSelectedName(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyDown)
	if m.SelectedName() != "beta" {
		t.Fatalf("SelectedName = %q", m.SelectedName())
	}
}

func TestEmptyModelIsSafe(t *testing.T) {
	m := New(nil)
	m, act := Update(m, KeyDown) // nothing to load
	if act != ActionNone {
		t.Fatalf("empty view action = %v, want ActionNone", act)
	}
	if m.SelectedName() != "" {
		t.Fatalf("empty SelectedName = %q", m.SelectedName())
	}
	_ = View(m) // must not panic
}

func TestFormatAge(t *testing.T) {
	now := "2026-06-30T12:00:00Z"
	cases := map[string]string{
		"2026-06-30T11:59:30Z": "30s",
		"2026-06-30T11:58:00Z": "2m",
		"2026-06-30T09:00:00Z": "3h",
		"2026-06-28T12:00:00Z": "2d",
		"":                     "-", // unknown
		"garbage":              "-", // unparseable
	}
	for started, want := range cases {
		if got := formatAge(started, now); got != want {
			t.Errorf("formatAge(%q) = %q, want %q", started, got, want)
		}
	}
}

func TestBoxedViewStructure(t *testing.T) {
	m := sample().SetSize(80, 24)
	m.Now = "2026-06-30T12:00:00Z"
	m.Sessions[0].StartedAt = "2026-06-30T11:58:00Z"
	m = m.SetTranscript("user: hi\nassistant: yo\n")
	v := View(m)
	// box-drawing frame present
	if !strings.ContainsAny(v, "┌┐└┘│─") {
		t.Fatalf("expected box-drawing borders, got:\n%s", v)
	}
	// every agent name in the left column
	for _, s := range m.Sessions {
		if !strings.Contains(v, s.Name) {
			t.Errorf("agent %q missing from view", s.Name)
		}
	}
	// selected marker, age, transcript content, and key hints
	if !strings.Contains(v, "›") && !strings.Contains(v, ">") {
		t.Error("no selection marker")
	}
	if !strings.Contains(v, "2m") {
		t.Error("age not shown")
	}
	if !strings.Contains(v, "assistant: yo") {
		t.Error("transcript not shown")
	}
	// no rendered line exceeds the width
	for _, ln := range strings.Split(v, "\n") {
		if w := displayWidth(ln); w > 80 {
			t.Errorf("line exceeds width 80 (%d): %q", w, ln)
		}
	}
}

func TestViewFallsBackWhenNoSize(t *testing.T) {
	m := sample() // Width/Height zero
	if v := View(m); v == "" {
		t.Fatal("View must render with a fallback size")
	}
}

// enterInput drives the two-phase new-agent flow up to task-input mode: open
// the browser ('n'), then choose the current dir ('c'). After Task D, KeyNew no
// longer enters input mode directly.
func enterInput(m Model) Model {
	m, _ = Update(m, KeyNew)     // phase 1: browser
	m = m.SetBrowse("", "", nil) // loop would populate; empty is fine
	m, _ = Update(m, KeyChoose)  // phase 2: input mode
	return m
}

func TestNewKeyEntersInputMode(t *testing.T) {
	m := enterInput(sample())
	if !m.Input {
		t.Fatal("choosing a dir should enter input mode")
	}
}

func TestInputModeTypingAndSubmit(t *testing.T) {
	m := enterInput(sample())
	for _, r := range "fix bug" {
		m = m.InputRune(r)
	}
	if m.Buffer != "fix bug" {
		t.Fatalf("buffer = %q", m.Buffer)
	}
	m, act := Update(m, KeyEnter)
	if act != ActionLaunch {
		t.Fatalf("Enter in input mode should ActionLaunch, got %v", act)
	}
	if m.Input {
		t.Fatal("submit should exit input mode")
	}
	if m.LaunchTask() != "fix bug" {
		t.Fatalf("LaunchTask = %q", m.LaunchTask())
	}
}

func TestInputModeBackspaceAndEscape(t *testing.T) {
	m := enterInput(sample())
	m = m.InputRune('a').InputRune('b')
	m = m.Backspace()
	if m.Buffer != "a" {
		t.Fatalf("buffer after backspace = %q", m.Buffer)
	}
	m, act := Update(m, KeyEsc)
	if act != ActionNone || m.Input {
		t.Fatalf("Esc should cancel input mode (act=%v input=%v)", act, m.Input)
	}
	if m.Buffer != "" {
		t.Fatalf("cancel should clear buffer, got %q", m.Buffer)
	}
}

func TestEmptySubmitDoesNotLaunch(t *testing.T) {
	m := enterInput(sample())
	m, act := Update(m, KeyEnter) // empty buffer
	if act == ActionLaunch {
		t.Fatal("empty task must not launch")
	}
}

func TestKillKeyYieldsKillAction(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyDown)
	_, act := Update(m, KeyKill)
	if act != ActionKill {
		t.Fatalf("KeyKill action = %v, want ActionKill", act)
	}
}

func TestKillEmptyListNoop(t *testing.T) {
	m := New(nil)
	_, act := Update(m, KeyKill)
	if act != ActionNone {
		t.Fatalf("kill on empty list should be noop, got %v", act)
	}
}

func TestInputModeIgnoresNavKeys(t *testing.T) {
	m := enterInput(sample())
	before := m.Selected
	m, _ = Update(m, KeyDown) // in input mode, nav must not move selection
	if m.Selected != before {
		t.Fatal("nav keys must be inert in input mode")
	}
}

func TestViewShowsInputPrompt(t *testing.T) {
	m := enterInput(sample().SetSize(80, 24))
	m = m.InputRune('h').InputRune('i')
	v := View(m)
	if !strings.Contains(v, "new task:") || !strings.Contains(v, "hi") {
		t.Fatalf("input prompt/buffer not shown:\n%s", v)
	}
}

func TestViewKeyHintsIncludeNewAndKill(t *testing.T) {
	m := sample().SetSize(80, 24)
	v := View(m)
	if !strings.Contains(v, "n new") || !strings.Contains(v, "k kill") {
		t.Fatalf("key hints missing n/k:\n%s", v)
	}
}

// --- Task D: directory browser before task input ---

func TestNewKeyEntersBrowsing(t *testing.T) {
	m := sample()
	m, act := Update(m, KeyNew)
	if !m.Browsing {
		t.Fatal("KeyNew should enter browsing mode")
	}
	if m.Input {
		t.Fatal("KeyNew should NOT enter input mode directly")
	}
	if act != ActionBrowse {
		t.Fatalf("KeyNew action = %v, want ActionBrowse", act)
	}
}

func TestSetBrowsePopulatesEntries(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/home/u", "/home", []DirItem{
		{Name: "proj", Path: "/home/u/proj"},
		{Name: "docs", Path: "/home/u/docs"},
	})
	if m.BrowseDir != "/home/u" || m.BrowseParent != "/home" {
		t.Fatalf("dir/parent = %q/%q", m.BrowseDir, m.BrowseParent)
	}
	if len(m.BrowseEntries) != 2 || m.BrowseEntries[0].Name != "proj" {
		t.Fatalf("entries = %+v", m.BrowseEntries)
	}
	if m.BrowseSel != 0 {
		t.Fatalf("BrowseSel should reset to 0, got %d", m.BrowseSel)
	}
}

func TestBrowseUpDownClamp(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d", "/", []DirItem{
		{Name: "a", Path: "/d/a"},
		{Name: "b", Path: "/d/b"},
	})
	m, _ = Update(m, KeyUp) // clamp at top
	if m.BrowseSel != 0 {
		t.Fatalf("up at top should clamp, got %d", m.BrowseSel)
	}
	m, _ = Update(m, KeyDown)
	if m.BrowseSel != 1 {
		t.Fatalf("down should move, got %d", m.BrowseSel)
	}
	m, _ = Update(m, KeyDown) // clamp at bottom
	if m.BrowseSel != 1 {
		t.Fatalf("down at bottom should clamp, got %d", m.BrowseSel)
	}
}

func TestBrowseEnterDescends(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d", "/", []DirItem{
		{Name: "a", Path: "/d/a"},
		{Name: "b", Path: "/d/b"},
	})
	m, _ = Update(m, KeyDown) // select b
	m, act := Update(m, KeyEnter)
	if act != ActionBrowse {
		t.Fatalf("Enter on entry action = %v, want ActionBrowse", act)
	}
	if m.BrowseDir != "/d/b" {
		t.Fatalf("descend should set BrowseDir to entry path, got %q", m.BrowseDir)
	}
	if !m.Browsing {
		t.Fatal("still browsing after descend")
	}
}

func TestBrowseEnterEmptyListNoop(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d", "/", nil)
	_, act := Update(m, KeyEnter)
	if act != ActionNone {
		t.Fatalf("Enter on empty browse list = %v, want ActionNone", act)
	}
}

func TestBrowseParentGoesUp(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d/sub", "/d", []DirItem{{Name: "x", Path: "/d/sub/x"}})
	m, act := Update(m, KeyParent)
	if act != ActionBrowse {
		t.Fatalf("KeyParent action = %v, want ActionBrowse", act)
	}
	if m.BrowseDir != "/d" {
		t.Fatalf("parent should set BrowseDir to parent, got %q", m.BrowseDir)
	}
}

func TestBrowseChooseEntersInput(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d/sub", "/d", []DirItem{{Name: "x", Path: "/d/sub/x"}})
	m, act := Update(m, KeyChoose)
	if act != ActionNone {
		t.Fatalf("KeyChoose action = %v, want ActionNone", act)
	}
	if m.Browsing {
		t.Fatal("choose should exit browsing")
	}
	if !m.Input {
		t.Fatal("choose should enter input mode")
	}
	if m.ChosenDir() != "/d/sub" {
		t.Fatalf("ChosenDir = %q, want /d/sub", m.ChosenDir())
	}
	if m.Buffer != "" {
		t.Fatalf("buffer should be empty on entering input, got %q", m.Buffer)
	}
}

func TestBrowseEscCancels(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/d", "/", []DirItem{{Name: "x", Path: "/d/x"}})
	m, act := Update(m, KeyEsc)
	if act != ActionNone {
		t.Fatalf("Esc in browse action = %v, want ActionNone", act)
	}
	if m.Browsing || m.Input {
		t.Fatal("Esc should cancel the whole flow")
	}
}

func TestChooseThenTypeThenLaunch(t *testing.T) {
	m := sample()
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/work/proj", "/work", nil)
	m, _ = Update(m, KeyChoose)
	for _, r := range "do it" {
		m = m.InputRune(r)
	}
	m, act := Update(m, KeyEnter)
	if act != ActionLaunch {
		t.Fatalf("Enter action = %v, want ActionLaunch", act)
	}
	if m.LaunchTask() != "do it" {
		t.Fatalf("LaunchTask = %q", m.LaunchTask())
	}
	if m.ChosenDir() != "/work/proj" {
		t.Fatalf("ChosenDir = %q, want /work/proj", m.ChosenDir())
	}
}

func TestViewShowsBrowser(t *testing.T) {
	m := sample().SetSize(80, 24)
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/home/u", "/home", []DirItem{
		{Name: "proj", Path: "/home/u/proj"},
	})
	v := View(m)
	if !strings.Contains(v, "proj") {
		t.Fatalf("browser entry not shown:\n%s", v)
	}
	if !strings.Contains(v, "/home/u") {
		t.Fatalf("browse dir title not shown:\n%s", v)
	}
	if !strings.Contains(v, "choose") {
		t.Fatalf("browser footer hint not shown:\n%s", v)
	}
}

func TestViewShowsChosenDirInInputPrompt(t *testing.T) {
	m := sample().SetSize(80, 24)
	m, _ = Update(m, KeyNew)
	m = m.SetBrowse("/work/proj", "/work", nil)
	m, _ = Update(m, KeyChoose)
	m = m.InputRune('h').InputRune('i')
	v := View(m)
	if !strings.Contains(v, "/work/proj") {
		t.Fatalf("chosen dir not shown in input prompt:\n%s", v)
	}
	if !strings.Contains(v, "new task:") || !strings.Contains(v, "hi") {
		t.Fatalf("input prompt missing:\n%s", v)
	}
}
