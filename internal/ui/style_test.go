package ui

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestTruncPadExactWidth(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},   // pad
		{"abcdef", 4, "abc…"}, // truncate with ellipsis
		{"abc", 3, "abc"},     // exact
		{"abcdef", 1, "…"},    // width 1 → just ellipsis
		{"anything", 0, ""},   // zero width
	}
	for _, c := range cases {
		got := truncPad(c.in, c.w)
		if got != c.want {
			t.Errorf("truncPad(%q,%d) = %q, want %q", c.in, c.w, got, c.want)
		}
		// Every non-zero-width result must be exactly w display cells.
		if c.w > 0 && len([]rune(got)) != c.w {
			t.Errorf("truncPad(%q,%d) width = %d, want %d", c.in, c.w, len([]rune(got)), c.w)
		}
	}
}

func TestStyledToggleControlsEscapes(t *testing.T) {
	// Off (default in tests): no ANSI escapes, plain text.
	Styled = false
	if got := fg(colGreen, "run"); got != "run" {
		t.Errorf("styling off should be plain, got %q", got)
	}
	if got := bold("x"); got != "x" {
		t.Errorf("bold off should be plain, got %q", got)
	}

	// On: wraps with an escape and resets.
	Styled = true
	defer func() { Styled = false }()
	got := fg(colGreen, "run")
	if !strings.Contains(got, "\x1b[38;5;") || !strings.HasSuffix(got, ansiReset) {
		t.Errorf("styling on should wrap with SGR + reset, got %q", got)
	}
	if !strings.Contains(got, "run") {
		t.Errorf("styled text should still contain the payload, got %q", got)
	}
}

func TestRenderListPlainWhenStyledOff(t *testing.T) {
	// A rendered list with styling off must contain no ESC bytes, so scripts /
	// tests see clean text.
	Styled = false
	out := renderList(viewModel(ViewCombined))
	if strings.Contains(out, "\x1b") {
		t.Errorf("renderList with styling off must be escape-free:\n%q", out)
	}
}

func TestRenderRowsTruncatesLongNameToWidth(t *testing.T) {
	Styled = false
	m := viewModel(ViewRemote)
	m.Agents = []core.Agent{
		{Name: "a-really-long-agent-name-that-overflows-the-column", Where: core.Remote, State: core.Running},
	}
	m.W, m.H = 80, 24
	out := renderList(m)
	// the long name must be truncated with an ellipsis, not printed in full.
	if strings.Contains(out, "overflows-the-column") {
		t.Errorf("long name should be truncated:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("truncated name should carry an ellipsis:\n%s", out)
	}
}
