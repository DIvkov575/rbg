// Package render turns claude transcript JSONL into a rich, human-readable
// reconstruction of the conversation: user/assistant turns with full text, tool
// calls (name + input summary), truncated tool results, and thinking. It
// tolerates unknown keys and malformed lines (skipped).
package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Options tunes a render. Tail>0 renders only the last N turns (0 = all).
// TruncateResult caps tool-result lines shown (0 = a sensible default of 8).
type Options struct {
	Tail           int
	TruncateResult int
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type record struct {
	Type    string  `json:"type"`
	Message message `json:"message"`
}
type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

// turn is one rendered role turn (its lines, sans separators).
type turn struct{ lines []string }

// Render reconstructs the conversation from raw JSONL bytes.
func Render(data []byte, opts Options) []string {
	if opts.TruncateResult <= 0 {
		opts.TruncateResult = 8
	}
	var turns []turn
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if t, ok := renderRecord(raw, opts); ok {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return []string{"(no conversation content yet)"}
	}
	if opts.Tail > 0 && opts.Tail < len(turns) {
		turns = turns[len(turns)-opts.Tail:]
	}
	var out []string
	for i, t := range turns {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, t.lines...)
	}
	return out
}

func renderRecord(raw string, opts Options) (turn, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return turn{}, false
	}
	var rec record
	if json.Unmarshal([]byte(raw), &rec) != nil {
		return turn{}, false
	}
	role := rec.Message.Role
	if role == "" {
		role = rec.Type
	}
	var body []string
	// content is a bare string or an array of blocks.
	var str string
	if json.Unmarshal(rec.Message.Content, &str) == nil {
		if str != "" {
			body = append(body, str)
		}
	} else {
		var blocks []block
		if json.Unmarshal(rec.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				body = append(body, renderBlock(b, opts)...)
			}
		}
	}
	if len(body) == 0 {
		return turn{}, false
	}
	header := roleHeader(role)
	return turn{lines: append([]string{header}, indent(body)...)}, true
}

func renderBlock(b block, opts Options) []string {
	switch b.Type {
	case "text":
		if b.Text == "" {
			return nil
		}
		return strings.Split(b.Text, "\n")
	case "thinking":
		if b.Text == "" {
			return nil
		}
		return []string{"(thinking) " + firstLine(b.Text)}
	case "tool_use":
		name := b.Name
		if name == "" {
			name = "?"
		}
		return []string{fmt.Sprintf("⚙ %s(%s)", name, inputSummary(b.Input))}
	case "tool_result":
		return truncateResult(b.Content, opts.TruncateResult)
	}
	return nil
}

// inputSummary renders a tool's input JSON to a short one-liner.
func inputSummary(in json.RawMessage) string {
	if len(in) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(in, &m) != nil {
		return ""
	}
	// prefer common single-value keys.
	for _, k := range []string{"command", "file_path", "path", "pattern", "query", "url"} {
		if v, ok := m[k]; ok {
			return firstLine(fmt.Sprintf("%v", v))
		}
	}
	// else list keys.
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

// truncateResult renders a tool_result's content (string or blocks) to at most
// n lines, appending a "+M more" marker when clipped.
func truncateResult(content json.RawMessage, n int) []string {
	text := resultText(content)
	if text == "" {
		return []string{"[tool result]"}
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return append([]string{"[tool result]"}, lines...)
	}
	shown := append([]string{"[tool result]"}, lines[:n]...)
	return append(shown, fmt.Sprintf("… +%d more", len(lines)-n))
}

func resultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(content, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func roleHeader(role string) string {
	switch role {
	case "user":
		return "▸ you"
	case "assistant":
		return "▸ claude"
	case "":
		return "▸ ?"
	default:
		return "▸ " + role
	}
}

func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
