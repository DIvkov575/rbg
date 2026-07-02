package render

import (
	"strings"
	"testing"
)

const fixture = `{"type":"user","message":{"role":"user","content":"add a test"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"sure"},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok\nPASS\nmore\nlines\nhere"}]}}
garbage-not-json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`

func TestRenderAllTurns(t *testing.T) {
	lines := Render([]byte(fixture), Options{TruncateResult: 2})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "add a test") {
		t.Error("missing user prompt")
	}
	if !strings.Contains(joined, "Bash") || !strings.Contains(joined, "go test") {
		t.Error("tool_use should show name + input summary")
	}
	if !strings.Contains(joined, "+3 more") {
		t.Errorf("tool_result should truncate to 2 lines with marker, got:\n%s", joined)
	}
	if strings.Contains(joined, "garbage-not-json") {
		t.Error("malformed line must be skipped")
	}
	if !strings.Contains(joined, "done") {
		t.Error("final assistant turn missing")
	}
}

func TestRenderTailLimitsTurns(t *testing.T) {
	lines := Render([]byte(fixture), Options{Tail: 1})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "done") {
		t.Error("tail should include the last turn")
	}
	if strings.Contains(joined, "add a test") {
		t.Error("tail=1 should exclude earlier turns")
	}
}

func TestRenderEmpty(t *testing.T) {
	lines := Render(nil, Options{})
	if len(lines) != 1 || !strings.Contains(lines[0], "no conversation") {
		t.Errorf("empty transcript should yield a placeholder, got %v", lines)
	}
}
