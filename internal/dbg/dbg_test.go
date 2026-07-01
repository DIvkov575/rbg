package dbg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisabledByDefault(t *testing.T) {
	reset()
	t.Setenv("RBG_DEBUG", "")
	if Enabled() {
		t.Error("debug should be off when RBG_DEBUG is empty")
	}
	// Logf must be a silent no-op when disabled.
	Logf("should not appear")
}

func TestEnabledWritesToExplicitPath(t *testing.T) {
	reset()
	p := filepath.Join(t.TempDir(), "debug.log")
	t.Setenv("RBG_DEBUG", p)
	if !Enabled() {
		t.Fatal("debug should be on with RBG_DEBUG set to a path")
	}
	if Path() != p {
		t.Errorf("Path() = %q, want %q", Path(), p)
	}
	Logf("hello %d", 42)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello 42") {
		t.Errorf("log missing message: %q", data)
	}
}
