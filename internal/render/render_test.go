package render

import (
	"bytes"
	"testing"
)

func TestLine_StringContent(t *testing.T) {
	got, ok := Line(`{"type":"user","message":{"role":"user","content":"hello"}}`)
	if !ok || got != "user: hello" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_TextBlocks(t *testing.T) {
	in := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}`
	got, ok := Line(in)
	if !ok || got != "assistant: hi there" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_ToolUse(t *testing.T) {
	in := `{"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`
	got, ok := Line(in)
	if !ok || got != "assistant: [tool: Bash]" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_SkipsBlankBadEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "{bad json", `{"type":"system","message":{"content":[]}}`} {
		if got, ok := Line(in); ok {
			t.Fatalf("expected skip for %q, got %q", in, got)
		}
	}
}

func TestLine_ToleratesUnknownKeys(t *testing.T) {
	in := `{"type":"assistant","weird":1,"message":{"role":"assistant","content":"ok"}}`
	got, ok := Line(in)
	if !ok || got != "assistant: ok" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestStream_PrintsOnlyRenderable(t *testing.T) {
	lines := []string{
		`{"message":{"role":"user","content":"q"}}`,
		"garbage",
		`{"message":{"role":"assistant","content":"a"}}`,
	}
	var buf bytes.Buffer
	Stream(lines, &buf)
	if buf.String() != "user: q\nassistant: a\n" {
		t.Fatalf("got %q", buf.String())
	}
}
