package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/render"
)

// pagerModel is the remote session view: a scrollable rendered transcript plus
// a prompt bar for sending follow-ups, a status line, and a processing spinner.
// It opens instantly (loading=true) and fills when the transcript arrives, so
// pressing enter feels immediate instead of blocking on the SSH read.
type pagerModel struct {
	title   string
	agent   string   // agent SESSION id (unique) — the read/send key; names aren't unique
	lines   []string // rendered transcript lines
	offset  int
	loading bool   // fetching the transcript
	sending bool   // a follow-up send is in flight
	status  string // one-line status (errors, "sent", etc.)
	prompt  string // the prompt-bar buffer
	spin    int    // spinner frame index
}

// spinnerFrames is a small braille spinner shown while loading/sending.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// newSessionView opens the view for an agent immediately, before the transcript
// is fetched (loading=true). setTranscript fills it when the data arrives.
func newSessionView(title, agent string) pagerModel {
	return pagerModel{title: title, agent: agent, loading: true}
}

// setTranscript replaces the rendered lines from raw JSONL and clears loading.
func (p pagerModel) setTranscript(data []byte) pagerModel {
	p.lines = renderTranscript(data)
	p.loading = false
	// keep the view pinned to the bottom (latest turns) on (re)load.
	p.offset = -1 // sentinel: viewPager clamps -1 to the last page
	return p
}

// renderTranscript turns raw claude JSONL into readable "role: text" lines.
func renderTranscript(data []byte) []string {
	norm := strings.ReplaceAll(string(data), "\r\n", "\n")
	var lines []string
	for _, raw := range strings.Split(norm, "\n") {
		if out, ok := render.Line(raw); ok {
			lines = append(lines, strings.Split(out, "\n")...)
		}
	}
	if len(lines) == 0 {
		lines = []string{"(no conversation content yet)"}
	}
	return lines
}

// window is how many transcript lines fit below the chrome (title + status +
// prompt bar + hints).
func (p pagerModel) window(h int) int {
	n := h - 6
	if n < 1 {
		n = 1
	}
	return n
}

func (p pagerModel) maxOffset(h int) int {
	max := len(p.lines) - p.window(h)
	if max < 0 {
		return 0
	}
	return max
}

// resolveOffset turns the -1 "pin to bottom" sentinel into a concrete offset.
func (p pagerModel) resolveOffset(h int) int {
	if p.offset < 0 {
		return p.maxOffset(h)
	}
	return p.offset
}

// pagerAction is what a keystroke asks the enclosing model to do.
type pagerAction int

const (
	pagerNone  pagerAction = iota
	pagerClose             // esc — leave the session view
	pagerSend              // enter with a non-empty prompt — send p.prompt
)

// key handles a keystroke in the session view: arrows scroll, printable runes
// type into the prompt bar, enter sends (if the prompt is non-empty), esc
// closes. Returns the updated model and the action for the caller to fulfill.
func (p pagerModel) key(s string, r rune, h int) (pagerModel, pagerAction) {
	switch s {
	case "up":
		o := p.resolveOffset(h)
		if o > 0 {
			p.offset = o - 1
		}
	case "down":
		o := p.resolveOffset(h)
		if o < p.maxOffset(h) {
			p.offset = o + 1
		}
	case "esc":
		return p, pagerClose
	case "enter":
		if strings.TrimSpace(p.prompt) != "" {
			return p, pagerSend
		}
	case "backspace":
		if n := len(p.prompt); n > 0 {
			p.prompt = p.prompt[:n-1]
		}
	case "rune":
		p.prompt += string(r)
	}
	return p, pagerNone
}
