// Package slug derives short, filesystem- and shell-safe agent names from a
// task string. Output matches ^[a-z0-9-]+$ and is never empty (falls back to
// "agent"), so it is always a valid rbg agent id.
package slug

import "strings"

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "and": true, "is": true, "it": true,
}

const (
	maxWords = 4
	maxLen   = 40
)

// FromTask converts a free-text task into a slug: lowercase, alnum runs only,
// stopwords dropped, joined by '-', capped at maxWords / maxLen. Empty results
// fall back to "agent".
func FromTask(task string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		cur.Reset()
		if !stopwords[w] {
			words = append(words, w)
		}
	}
	for _, r := range strings.ToLower(task) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	if len(words) > maxWords {
		words = words[:maxWords]
	}
	out := strings.Join(words, "-")
	if len(out) > maxLen {
		out = out[:maxLen]
		out = strings.TrimRight(out, "-")
	}
	if out == "" {
		return "agent"
	}
	return out
}
