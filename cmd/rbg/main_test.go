package main

import (
	"strings"
	"testing"
)

func TestIsScriptableClassifiesVerbs(t *testing.T) {
	for _, v := range []string{"ls", "create", "run", "send", "read", "kill", "adopt"} {
		if !isScriptable(v) {
			t.Errorf("verb %q should route to the scriptable CLI", v)
		}
	}
	for _, v := range []string{"dash", "deploy", "ping", "attach", "help", "frob"} {
		if isScriptable(v) {
			t.Errorf("verb %q should NOT be scriptable", v)
		}
	}
}

func TestMigrationHint(t *testing.T) {
	if h := migrationHint("launch"); h == "" {
		t.Errorf("launch should have a migration hint to create+run")
	} else if !strings.Contains(h, "create") || !strings.Contains(h, "run") {
		t.Errorf("launch hint should mention create and run: %q", h)
	}
	if migrationHint("frob") != "" {
		t.Errorf("frob should have no migration hint")
	}
}

func TestClaudeSessionIDFor(t *testing.T) {
	ls := []byte(`[{"name":"alpha","claudeSessionId":"sid-a"},{"name":"beta","claudeSessionId":"sid-b"}]`)
	if got := claudeSessionIDFor(ls, "beta"); got != "sid-b" {
		t.Errorf("got %q want sid-b", got)
	}
	if got := claudeSessionIDFor(ls, "ghost"); got != "" {
		t.Errorf("ghost should be empty, got %q", got)
	}
}
