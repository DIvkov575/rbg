// Package tui holds the rbg dashboard. model.go is the PURE state machine —
// no terminal, no I/O — so it is fully unit-testable. term*.go drives it.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/divkov575/rbg/internal/session"
)

// Key is an abstract key event fed to Update (decoded from raw bytes by the
// terminal layer).
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyView      // ⏎ or v: load selected transcript
	KeyAttach    // a
	KeyRefresh   // r
	KeyQuit      // q
	KeyNew       // n: start a new agent (enter input mode)
	KeyKill      // k: kill the selected agent
	KeyEnter     // ↵ in input mode: submit
	KeyEsc       // esc in input mode: cancel
	KeyBackspace // backspace in input mode
)

// Action is what the terminal loop must do after an Update (the model itself
// performs no I/O).
type Action int

const (
	ActionNone Action = iota
	ActionLoadTranscript
	ActionAttach
	ActionRefresh
	ActionQuit
	ActionLaunch // launch m.LaunchTask()
	ActionKill   // kill m.SelectedName()
)

const (
	fallbackWidth  = 80
	fallbackHeight = 24
	minListWidth   = 18
	maxListWidth   = 32
)

// Model is the dashboard state. Width/Height are the terminal dimensions (0
// until SetSize); Now is an RFC3339 timestamp the loop stamps so age rendering
// stays pure (no clock in the model).
type Model struct {
	Sessions   []session.Session
	Selected   int
	Transcript string
	Width      int
	Height     int
	Now        string
	Input      bool   // in task-input (new-agent) mode
	Buffer     string // task text being typed
	launchTask string // task captured at submit, read by the loop
}

// New builds a model from a session list.
func New(sessions []session.Session) Model {
	return Model{Sessions: sessions}
}

// SelectedName returns the highlighted agent's name, or "" if none.
func (m Model) SelectedName() string {
	if len(m.Sessions) == 0 || m.Selected < 0 || m.Selected >= len(m.Sessions) {
		return ""
	}
	return m.Sessions[m.Selected].Name
}

// SetSessions replaces the list (after a refresh), clamping Selected.
func (m Model) SetSessions(s []session.Session) Model {
	m.Sessions = s
	if m.Selected >= len(s) {
		m.Selected = len(s) - 1
	}
	if m.Selected < 0 {
		m.Selected = 0
	}
	return m
}

// SetTranscript stores rendered transcript text for the right pane.
func (m Model) SetTranscript(text string) Model {
	m.Transcript = text
	return m
}

// withNow sets the timestamp used for age rendering (loop-injected, keeps the
// model clock-free).
func (m Model) withNow(now string) Model {
	m.Now = now
	return m
}

// SetSize records the terminal dimensions for layout.
func (m Model) SetSize(w, h int) Model {
	m.Width, m.Height = w, h
	return m
}

// InputRune appends a typed rune to the task buffer (only meaningful in input
// mode; the loop gates on m.Input).
func (m Model) InputRune(r rune) Model {
	m.Buffer += string(r)
	return m
}

// Backspace removes the last rune from the task buffer.
func (m Model) Backspace() Model {
	r := []rune(m.Buffer)
	if len(r) > 0 {
		m.Buffer = string(r[:len(r)-1])
	}
	return m
}

// LaunchTask returns the task captured by the most recent submit.
func (m Model) LaunchTask() string { return m.launchTask }

// Update applies a key, returning the new model and an Action for the loop.
func Update(m Model, k Key) (Model, Action) {
	if m.Input {
		switch k {
		case KeyEnter:
			m.Input = false
			task := strings.TrimSpace(m.Buffer)
			m.Buffer = ""
			if task == "" {
				return m, ActionNone // empty submit cancels
			}
			m.launchTask = task
			return m, ActionLaunch
		case KeyEsc:
			m.Input = false
			m.Buffer = ""
			return m, ActionNone
		}
		// other keys (incl. nav) are inert here; printable runes arrive via
		// InputRune / Backspace, which the loop calls directly.
		return m, ActionNone
	}
	switch k {
	case KeyNew:
		m.Input = true
		m.Buffer = ""
		return m, ActionNone
	case KeyKill:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionKill
	case KeyUp:
		if m.Selected > 0 {
			m.Selected--
		}
	case KeyDown:
		if m.Selected < len(m.Sessions)-1 {
			m.Selected++
		}
	case KeyView:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionLoadTranscript
	case KeyAttach:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionAttach
	case KeyRefresh:
		return m, ActionRefresh
	case KeyQuit:
		return m, ActionQuit
	}
	return m, ActionNone
}

// formatAge renders the gap between an RFC3339 startedAt and now as a compact
// "30s"/"2m"/"3h"/"2d". Unknown/unparseable → "-".
func formatAge(startedAt, now string) string {
	if startedAt == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return "-"
	}
	n, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return "-"
	}
	d := n.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// displayWidth is the rune count of s (our glyphs are width-1; box chars and
// ASCII alike), used to keep rendered lines within the terminal width.
func displayWidth(s string) int {
	return len([]rune(s))
}

// padTo right-pads s with spaces to exactly w runes (truncating with an ellipsis
// if longer).
func padTo(s string, w int) string {
	r := []rune(s)
	if len(r) == w {
		return s
	}
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// View renders the dashboard: a bordered two-pane layout (agent list | selected
// transcript) à la `claude agents`, sized to the terminal.
func View(m Model) string {
	w, h := m.Width, m.Height
	if w <= 0 {
		w = fallbackWidth
	}
	if h <= 0 {
		h = fallbackHeight
	}

	// Layout: outer frame is w wide. Two panes separated by a vertical rule.
	// listW chosen from the longest "name age" plus padding, clamped.
	listW := minListWidth
	for _, s := range m.Sessions {
		if l := displayWidth(s.Name) + 6; l > listW {
			listW = l
		}
	}
	if listW > maxListWidth {
		listW = maxListWidth
	}
	if listW > w-12 { // always leave room for the transcript pane
		listW = w - 12
	}
	transW := w - listW - 3 // 3 = two outer borders + one separator
	if transW < 1 {
		transW = 1
	}

	bodyH := h - 4 // top title, header rule, key-hint row, bottom border
	if bodyH < 1 {
		bodyH = 1
	}

	var b strings.Builder
	// top border with titles
	b.WriteString("┌" + labelRule("agents", listW) + "┬" + labelRule("transcript", transW) + "┐\n")

	left := listLines(m)
	right := strings.Split(m.Transcript, "\n")
	if m.Transcript == "" {
		right = []string{"(↵ to load)"}
	}
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		b.WriteString("│" + padTo(l, listW) + "│" + padTo(r, transW) + "│\n")
	}

	// footer row spanning the full inner width: the task-input prompt while in
	// input mode, otherwise the key hints.
	inner := listW + transW + 1
	var hints string
	if m.Input {
		hints = " new task: " + m.Buffer + "█"
	} else {
		hints = " ↑/↓ move  ↵/v view  n new  k kill  a attach  r refresh  q quit"
	}
	b.WriteString("│" + padTo(hints, inner) + "│\n")
	b.WriteString("└" + strings.Repeat("─", inner) + "┘")
	return b.String()
}

// labelRule renders a "─ label ───" segment exactly w runes wide.
func labelRule(label string, w int) string {
	s := "─ " + label + " "
	if displayWidth(s) >= w {
		return strings.Repeat("─", w)
	}
	return s + strings.Repeat("─", w-displayWidth(s))
}

// listLines renders the agent column: "› name        2m".
func listLines(m Model) []string {
	out := make([]string, 0, len(m.Sessions))
	for i, s := range m.Sessions {
		marker := "  "
		if i == m.Selected {
			marker = "› "
		}
		age := formatAge(s.StartedAt, m.Now)
		out = append(out, fmt.Sprintf("%s%s  %s", marker, s.Name, age))
	}
	if len(out) == 0 {
		out = []string{"  (no agents)"}
	}
	return out
}
