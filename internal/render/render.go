// Package render turns claude transcript JSONL lines into human text. It
// tolerates unknown keys and malformed lines (returns ok=false to skip).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type record struct {
	Type    string  `json:"type"`
	Message message `json:"message"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// Line renders one JSONL line to "role: text", or ok=false to skip it.
func Line(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	var rec record
	if err := json.Unmarshal([]byte(s), &rec); err != nil {
		return "", false
	}
	var parts []string
	if len(rec.Message.Content) > 0 {
		// content may be a string or an array of blocks.
		var str string
		if json.Unmarshal(rec.Message.Content, &str) == nil {
			if str != "" {
				parts = append(parts, str)
			}
		} else {
			var blocks []block
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					switch b.Type {
					case "text":
						if b.Text != "" {
							parts = append(parts, b.Text)
						}
					case "tool_use":
						name := b.Name
						if name == "" {
							name = "?"
						}
						parts = append(parts, fmt.Sprintf("[tool: %s]", name))
					case "tool_result":
						parts = append(parts, "[tool result]")
					}
				}
			}
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		return "", false
	}
	role := rec.Message.Role
	if role == "" {
		role = rec.Type
	}
	if role == "" {
		role = "?"
	}
	return role + ": " + text, true
}

// Stream renders each line and writes renderable ones to w.
func Stream(lines []string, w io.Writer) {
	for _, ln := range lines {
		if out, ok := Line(ln); ok {
			fmt.Fprintln(w, out)
		}
	}
}
