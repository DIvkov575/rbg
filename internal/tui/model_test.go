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

func TestViewLoadsTranscriptIntoPane(t *testing.T) {
	m := sample()
	m, act := Update(m, KeyView)
	if act != ActionLoadTranscript {
		t.Fatalf("KeyView action = %v, want ActionLoadTranscript", act)
	}
	// the loop fulfills the action by calling SetTranscript:
	m = m.SetTranscript("user: hi\nassistant: yo\n")
	if !strings.Contains(View(m), "assistant: yo") {
		t.Fatalf("transcript not rendered in view:\n%s", View(m))
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
	m, act := Update(m, KeyView) // nothing to load
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
