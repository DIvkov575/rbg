package agent

import (
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/session"
)

func storeWith(t *testing.T, names ...string) (*session.Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sessions.json")
	s, _ := session.Load(p)
	for _, n := range names {
		s.Add(session.Session{Name: n})
	}
	return s, p
}

func TestResolveName_UsesExplicitName(t *testing.T) {
	s, _ := storeWith(t)
	if got := resolveName(s, "explicit", "some task"); got != "explicit" {
		t.Errorf("got %q, want explicit", got)
	}
}

func TestResolveName_DerivesFromTaskWhenEmpty(t *testing.T) {
	s, _ := storeWith(t)
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test" {
		t.Errorf("got %q, want fix-flaky-test", got)
	}
}

func TestResolveName_DedupsAgainstStore(t *testing.T) {
	s, _ := storeWith(t, "fix-flaky-test")
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test-2" {
		t.Errorf("got %q, want fix-flaky-test-2", got)
	}
	s.Add(session.Session{Name: "fix-flaky-test-2"})
	if got := resolveName(s, "", "fix the flaky test"); got != "fix-flaky-test-3" {
		t.Errorf("got %q, want fix-flaky-test-3", got)
	}
}

func TestResolveName_DedupsExplicitNameToo(t *testing.T) {
	s, _ := storeWith(t, "explicit")
	if got := resolveName(s, "explicit", "task"); got != "explicit-2" {
		t.Errorf("got %q, want explicit-2", got)
	}
}
