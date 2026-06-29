package run

import (
	"strings"
	"testing"
)

func TestRecordingRunner_RecordsAndReturnsDefault(t *testing.T) {
	r := &Recording{Default: Result{Stdout: []byte("ok"), Code: 0}}
	out, code, err := r.Run("ssh", []string{"host", "true"}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if string(out) != "ok" || code != 0 {
		t.Fatalf("got out=%q code=%d", out, code)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("calls not recorded: %+v", r.Calls)
	}
}

func TestRecordingRunner_MatchesBySubstring(t *testing.T) {
	r := &Recording{
		BySubstring: map[string]Result{"agents": {Stdout: []byte("[]")}},
		Default:     Result{Stdout: []byte("")},
	}
	out, _, _ := r.Run("ssh", []string{"host", "rbg-agent agents"}, nil)
	if string(out) != "[]" {
		t.Fatalf("substring match failed: got %q", out)
	}
	out2, _, _ := r.Run("ssh", []string{"host", "true"}, nil)
	if string(out2) != "" {
		t.Fatalf("default fallthrough failed: got %q", out2)
	}
}

func TestLastArgJoin(t *testing.T) {
	// helper used by tests to assert on the joined remote command
	got := joinArgs([]string{"host", "rbg-agent", "ls"})
	if !strings.Contains(got, "rbg-agent ls") {
		t.Fatalf("joinArgs = %q", got)
	}
}
