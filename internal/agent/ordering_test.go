package agent

import (
	"testing"

	"github.com/divkov575/rbg/internal/session"
)

func names(list []session.Session) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dir A {a1 older, a2 newer}, dir B {b1 newest overall}.
// Expected: b1 (B group newest), then a2, a1 (within A newest-first).
func TestSortSessions_GroupsByDirNewestFirst(t *testing.T) {
	list := []session.Session{
		{Name: "a1", Dir: "/A", StartedAt: "2026-06-28T01:00:00Z"},
		{Name: "a2", Dir: "/A", StartedAt: "2026-06-28T02:00:00Z"},
		{Name: "b1", Dir: "/B", StartedAt: "2026-06-28T03:00:00Z"},
	}
	sortSessions(list)
	want := []string{"b1", "a2", "a1"}
	if !eq(names(list), want) {
		t.Fatalf("got %v want %v", names(list), want)
	}
}

// Equal StartedAt within same dir falls back to Name ascending.
func TestSortSessions_EqualStartedAtNameAsc(t *testing.T) {
	list := []session.Session{
		{Name: "zeta", Dir: "/A", StartedAt: "2026-06-28T01:00:00Z"},
		{Name: "alpha", Dir: "/A", StartedAt: "2026-06-28T01:00:00Z"},
	}
	sortSessions(list)
	want := []string{"alpha", "zeta"}
	if !eq(names(list), want) {
		t.Fatalf("got %v want %v", names(list), want)
	}
}

// Empty/unparseable StartedAt sorts as oldest (zero time).
func TestSortSessions_EmptyStartedAtSortsOldest(t *testing.T) {
	list := []session.Session{
		{Name: "empty", Dir: "/A", StartedAt: ""},
		{Name: "bad", Dir: "/A", StartedAt: "not-a-time"},
		{Name: "real", Dir: "/A", StartedAt: "2026-06-28T01:00:00Z"},
	}
	sortSessions(list)
	// real is newest; empty and bad are both zero time -> Name asc (bad < empty).
	want := []string{"real", "bad", "empty"}
	if !eq(names(list), want) {
		t.Fatalf("got %v want %v", names(list), want)
	}
}

func TestSortSessions_SubSecondNewestFirst(t *testing.T) {
	// Same dir, same second, differing only by nanoseconds — newest must win
	// (previously these tied on RFC3339 second-precision and fell back to name).
	list := []session.Session{
		{Name: "aaa", Dir: "/d", StartedAt: "2026-06-30T12:00:00.100000000Z"},
		{Name: "ccc", Dir: "/d", StartedAt: "2026-06-30T12:00:00.900000000Z"},
	}
	sortSessions(list)
	if list[0].Name != "ccc" || list[1].Name != "aaa" {
		t.Fatalf("want ccc,aaa (newest first), got %s,%s", list[0].Name, list[1].Name)
	}
}
