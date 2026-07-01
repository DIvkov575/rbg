package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentJSONRoundTrip(t *testing.T) {
	a := Agent{
		Name:    "fix-auth",
		Repo:    "git@github.com:me/app.git",
		Dir:     "/home/me/workplace/app",
		Task:    "fix the login bug",
		Session: "55a63641-2b5e-413e-bd07-00a74bbc1dfc",
		Where:   Remote,
		State:   Running,
		Origin:  Managed,
		Sync:    Behind,
		RunAt:   "2026-06-30T12:00:00Z",
		Pid:     4321,
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Agent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != a {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, a)
	}
}

func TestIsHeld(t *testing.T) {
	held := Agent{State: Held}
	if !held.IsHeld() {
		t.Errorf("State=Held: IsHeld()=false, want true")
	}
	run := Agent{State: Running}
	if run.IsHeld() {
		t.Errorf("State=Running: IsHeld()=true, want false")
	}
}

func TestIsForeign(t *testing.T) {
	f := Agent{Origin: Foreign}
	if !f.IsForeign() {
		t.Errorf("Origin=Foreign: IsForeign()=false, want true")
	}
	m := Agent{Origin: Managed}
	if m.IsForeign() {
		t.Errorf("Origin=Managed: IsForeign()=true, want false")
	}
}

func TestNewSessionIDShapeAndUniqueness(t *testing.T) {
	a := NewSessionID()
	b := NewSessionID()
	if a == b {
		t.Errorf("two ids collided: %q", a)
	}
	// v4-ish UUID: 36 chars, 5 dash-separated groups of 8-4-4-4-12.
	if len(a) != 36 {
		t.Fatalf("len(%q) = %d, want 36", a, len(a))
	}
	groups := strings.Split(a, "-")
	wantLens := []int{8, 4, 4, 4, 12}
	if len(groups) != 5 {
		t.Fatalf("got %d groups in %q, want 5", len(groups), a)
	}
	for i, g := range groups {
		if len(g) != wantLens[i] {
			t.Errorf("group %d = %q (len %d), want len %d", i, g, len(g), wantLens[i])
		}
		for _, r := range g {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Errorf("group %d %q has non-hex rune %q", i, g, r)
			}
		}
	}
	// v4-ness: the 13th char (group[2][0]) must be '4' (version), and
	// group[3][0] must be one of 8,9,a,b (variant) — the two nibbles the
	// bit-masking sets. Guards against a regression dropping the masks.
	if groups[2][0] != '4' {
		t.Errorf("version nibble = %q, want '4' (not a v4 UUID)", groups[2][0])
	}
	switch groups[3][0] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("variant nibble = %q, want one of 8/9/a/b", groups[3][0])
	}
}

func TestDeriveSync(t *testing.T) {
	cases := []struct {
		name        string
		hasUpstream bool
		behind      int
		ahead       int
		dirty       bool
		want        Sync
	}{
		{"clean aligned", true, 0, 0, false, Aligned},
		{"behind", true, 3, 0, false, Behind},
		{"ahead", true, 0, 2, false, Ahead},
		{"dirty beats behind", true, 5, 0, true, Dirty},
		{"dirty beats ahead", true, 0, 5, true, Dirty},
		{"behind beats ahead when diverged", true, 1, 1, false, Behind},
		{"no upstream clean is unknown", false, 0, 0, false, SyncUnknown},
		{"no upstream but dirty is dirty", false, 0, 0, true, Dirty},
	}
	for _, c := range cases {
		got := DeriveSync(c.hasUpstream, c.behind, c.ahead, c.dirty)
		if got != c.want {
			t.Errorf("%s: DeriveSync(%v,%d,%d,%v) = %q, want %q",
				c.name, c.hasUpstream, c.behind, c.ahead, c.dirty, got, c.want)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	valid := []string{
		"55a63641-2b5e-413e-bd07-00a74bbc1dfc",
		"abc123",
		"A-B-c-9",
	}
	for _, id := range valid {
		if !ValidSessionID(id) {
			t.Errorf("ValidSessionID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"",            // empty
		"has space",   // space
		"semi;colon",  // shell metachar
		"glob*star",   // glob metachar
		"dot.dot",     // path char
		"slash/slash", // path separator
		"tilde~home",  // tilde
		"quote'quote", // single quote (shell breakout attempt)
		"dollar$var",  // expansion
		"-rf",         // leading dash (flag-injection into cat/ls)
		"--all",       // leading double-dash
		"back`tick",   // command substitution
		"pipe|pipe",   // pipe
		"amp&amp",     // background/chain
	}
	for _, id := range invalid {
		if ValidSessionID(id) {
			t.Errorf("ValidSessionID(%q) = true, want false", id)
		}
	}
}
