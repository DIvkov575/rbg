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
	KeyAttach    // a
	KeyRefresh   // r
	KeyQuit      // q
	KeyNew       // n: start a new agent (enter input mode)
	KeyKill      // k: kill the selected agent
	KeyEnter     // ↵ in input mode: submit
	KeyEsc       // esc in input mode: cancel
	KeyBackspace // backspace in input mode
	KeyParent    // h in browse mode: go to parent dir
	KeyChoose    // c in browse mode: choose current dir
	KeyMkdir     // m in browse mode: make a new directory
	KeyConfig    // C: open the config screen
	KeySave      // s: save the config screen
	KeyQueue     // Q: open the queue screen
	KeyDispatch  // d (in queue): dispatch the selected item
	KeyRemove    // x (in queue): remove the selected item
	KeyQueueAdd  // a (in queue): add a new item
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
	ActionLaunch      // launch m.LaunchTask() in m.ChosenDir()
	ActionKill        // kill m.SelectedName()
	ActionBrowse      // (re)list m.BrowseDir into the model via SetBrowse
	ActionMkdir       // create BrowseDir/DirName(), then descend + re-list
	ActionLoadConfig  // load ~/.rbg.conf fields into the config screen via SetConfig
	ActionSaveConfig  // persist m.ConfigValues() to ~/.rbg.conf
	ActionLoadQueue   // load the staged queue items via SetQueue
	ActionDispatch    // dispatch m.DispatchItem() (clone repo + launch)
	ActionQueueRemove // remove the item at m.QueueSel from the queue store
	ActionQueueAdd    // add m.PendingItem() to the queue store
)

// DirItem is one browsable subdirectory. It mirrors client.DirEntry but lives in
// package tui so the pure model never imports the client package; the loop
// converts client.DirEntry → DirItem.
type DirItem struct {
	Name string
	Path string
}

// ConfigField is one editable ~/.rbg.conf key/value in the config screen.
type ConfigField struct {
	Key   string
	Value string
}

// QueueItem is one staged task shown in the queue screen.
type QueueItem struct {
	Prompt string
	Repo   string
}

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

	// Directory-browser (phase 1 of the new-agent flow) state.
	Browsing      bool      // in directory-browser mode
	BrowseDir     string    // the directory currently listed
	BrowseParent  string    // its parent (for navigating up)
	BrowseEntries []DirItem // visible subdirectories
	BrowseSel     int       // highlighted entry
	chosenDir     string    // dir chosen for the launch (read by the loop)
	MakingDir     bool      // name-entry sub-mode within browsing (typing a new dir name)

	// Config-screen state (a separate, mutually-exclusive screen).
	ConfigOpen    bool
	ConfigFields  []ConfigField
	ConfigSel     int
	ConfigEditing bool

	// Queue-screen state (a separate, mutually-exclusive screen).
	QueueOpen   bool
	QueueItems  []QueueItem
	QueueSel    int
	QueueAdding bool      // entering a new item
	addStage    int       // 0 = typing prompt, 1 = typing repo
	pendingItem QueueItem // accumulates the new item
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

// DirName returns the directory name typed in the making-dir sub-mode (the
// shared Buffer, trimmed of surrounding whitespace).
func (m Model) DirName() string { return strings.TrimSpace(m.Buffer) }

// EnteredDir descends the browser into path (a just-created directory) and
// exits the making-dir sub-mode, clearing the name buffer. The loop then
// re-lists BrowseDir via ActionBrowse semantics.
func (m Model) EnteredDir(path string) Model {
	m.BrowseDir = path
	m.MakingDir = false
	m.Buffer = ""
	return m
}

// ChosenDir returns the directory chosen in the browser for the next launch
// ("" means the agent picks its default).
func (m Model) ChosenDir() string { return m.chosenDir }

// SetBrowse records a directory listing fetched by the loop and resets the
// selection to the top.
func (m Model) SetBrowse(dir, parent string, items []DirItem) Model {
	m.BrowseDir = dir
	m.BrowseParent = parent
	m.BrowseEntries = items
	m.BrowseSel = 0
	return m
}

// SetConfig loads the editable fields into the config screen.
func (m Model) SetConfig(fields []ConfigField) Model {
	m.ConfigFields = fields
	if m.ConfigSel >= len(fields) {
		m.ConfigSel = len(fields) - 1
	}
	if m.ConfigSel < 0 {
		m.ConfigSel = 0
	}
	return m
}

// ConfigValues returns the current key→value map from the config screen.
func (m Model) ConfigValues() map[string]string {
	out := make(map[string]string, len(m.ConfigFields))
	for _, f := range m.ConfigFields {
		out[f.Key] = f.Value
	}
	return out
}

// SetQueue loads the staged items into the queue screen, clamping QueueSel.
func (m Model) SetQueue(items []QueueItem) Model {
	m.QueueItems = items
	if m.QueueSel >= len(items) {
		m.QueueSel = len(items) - 1
	}
	if m.QueueSel < 0 {
		m.QueueSel = 0
	}
	return m
}

// DispatchItem returns the highlighted queue item (zero value if none).
func (m Model) DispatchItem() QueueItem {
	if m.QueueSel < 0 || m.QueueSel >= len(m.QueueItems) {
		return QueueItem{}
	}
	return m.QueueItems[m.QueueSel]
}

// PendingItem returns the item accumulated by the in-progress add flow.
func (m Model) PendingItem() QueueItem { return m.pendingItem }

// Update applies a key, returning the new model and an Action for the loop.
func Update(m Model, k Key) (Model, Action) {
	if m.QueueOpen {
		if m.QueueAdding {
			// Two-stage add: stage 0 captures the prompt, stage 1 the repo.
			// Printable runes/backspace are applied by the loop via
			// InputRune/Backspace; only Enter/Esc reach Update here.
			switch k {
			case KeyEnter:
				if m.addStage == 0 {
					m.pendingItem.Prompt = m.Buffer
					m.Buffer = ""
					m.addStage = 1
					return m, ActionNone
				}
				m.pendingItem.Repo = m.Buffer
				m.Buffer = ""
				m.QueueAdding = false
				m.addStage = 0
				return m, ActionQueueAdd
			case KeyEsc:
				m.QueueAdding = false
				m.addStage = 0
				m.Buffer = ""
				m.pendingItem = QueueItem{}
				return m, ActionNone
			}
			return m, ActionNone
		}
		switch k {
		case KeyUp:
			if m.QueueSel > 0 {
				m.QueueSel--
			}
			return m, ActionNone
		case KeyDown:
			if m.QueueSel < len(m.QueueItems)-1 {
				m.QueueSel++
			}
			return m, ActionNone
		case KeyDispatch:
			if len(m.QueueItems) == 0 {
				return m, ActionNone
			}
			return m, ActionDispatch
		case KeyRemove:
			if len(m.QueueItems) == 0 {
				return m, ActionNone
			}
			return m, ActionQueueRemove
		case KeyQueueAdd:
			m.QueueAdding = true
			m.addStage = 0
			m.Buffer = ""
			m.pendingItem = QueueItem{}
			return m, ActionNone
		case KeyEsc:
			m.QueueOpen = false
			return m, ActionNone
		}
		return m, ActionNone
	}
	if m.ConfigOpen {
		if m.ConfigEditing {
			// Field-edit sub-mode. Printable runes/backspace are applied by the
			// loop via InputRune/Backspace; only Enter/Esc reach Update here.
			switch k {
			case KeyEnter:
				m.ConfigEditing = false
				if m.ConfigSel >= 0 && m.ConfigSel < len(m.ConfigFields) {
					m.ConfigFields[m.ConfigSel].Value = m.Buffer
				}
				m.Buffer = ""
				return m, ActionNone
			case KeyEsc:
				m.ConfigEditing = false
				m.Buffer = ""
				return m, ActionNone
			}
			return m, ActionNone
		}
		switch k {
		case KeyUp:
			if m.ConfigSel > 0 {
				m.ConfigSel--
			}
			return m, ActionNone
		case KeyDown:
			if m.ConfigSel < len(m.ConfigFields)-1 {
				m.ConfigSel++
			}
			return m, ActionNone
		case KeyEnter:
			if m.ConfigSel < 0 || m.ConfigSel >= len(m.ConfigFields) {
				return m, ActionNone
			}
			m.ConfigEditing = true
			// Start from an empty buffer; the typed text replaces the field value
			// on commit (the plan's TestConfigNavAndEdit asserts a fresh edit).
			m.Buffer = ""
			return m, ActionNone
		case KeySave:
			return m, ActionSaveConfig
		case KeyEsc:
			m.ConfigOpen = false
			return m, ActionNone
		}
		return m, ActionNone
	}
	if m.Browsing {
		if m.MakingDir {
			// Name-entry sub-mode. Printable runes/backspace are applied by the
			// loop via InputRune/Backspace; only Enter/Esc reach Update here.
			switch k {
			case KeyEnter:
				if m.DirName() == "" {
					m.MakingDir = false
					m.Buffer = ""
					return m, ActionNone // empty name cancels
				}
				return m, ActionMkdir
			case KeyEsc:
				m.MakingDir = false
				m.Buffer = ""
				return m, ActionNone
			}
			return m, ActionNone
		}
		switch k {
		case KeyMkdir:
			m.MakingDir = true
			m.Buffer = ""
			return m, ActionNone
		case KeyUp:
			if m.BrowseSel > 0 {
				m.BrowseSel--
			}
			return m, ActionNone
		case KeyDown:
			if m.BrowseSel < len(m.BrowseEntries)-1 {
				m.BrowseSel++
			}
			return m, ActionNone
		case KeyEnter:
			if len(m.BrowseEntries) == 0 ||
				m.BrowseSel < 0 || m.BrowseSel >= len(m.BrowseEntries) {
				return m, ActionNone
			}
			m.BrowseDir = m.BrowseEntries[m.BrowseSel].Path
			return m, ActionBrowse // loop re-lists the descended dir
		case KeyParent:
			m.BrowseDir = m.BrowseParent
			return m, ActionBrowse
		case KeyChoose:
			m.Browsing = false
			m.chosenDir = m.BrowseDir
			m.Input = true
			m.Buffer = ""
			return m, ActionNone
		case KeyEsc:
			m.Browsing = false
			return m, ActionNone
		}
		return m, ActionNone
	}
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
		// phase 1: open the directory browser starting from the agent's
		// default dir (empty BrowseDir → loop asks the agent to resolve).
		m.Browsing = true
		m.BrowseDir = ""
		m.BrowseSel = 0
		m.chosenDir = ""
		return m, ActionBrowse
	case KeyKill:
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionKill
	case KeyUp:
		if m.Selected > 0 {
			m.Selected--
		}
		if m.SelectedName() == "" {
			return m, ActionNone
		}
		return m, ActionLoadTranscript
	case KeyDown:
		if m.Selected < len(m.Sessions)-1 {
			m.Selected++
		}
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
	case KeyConfig:
		m.ConfigOpen = true
		return m, ActionLoadConfig
	case KeyQueue:
		m.QueueOpen = true
		return m, ActionLoadQueue
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

	if m.QueueOpen {
		return queueView(m, w, h)
	}

	if m.ConfigOpen {
		return configView(m, w, h)
	}

	if m.Browsing {
		return browseView(m, w, h)
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
		right = []string{"(no transcript)"}
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
		if m.chosenDir != "" {
			hints = " in " + m.chosenDir + " — new task: " + m.Buffer + "█"
		} else {
			hints = " new task: " + m.Buffer + "█"
		}
	} else {
		hints = " ↑/↓ select  n new  k kill  a attach  r refresh  q quit"
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

// configView renders the config screen: a framed list of ~/.rbg.conf key/value
// fields with the ›-marker on ConfigSel, the selected value shown with a cursor
// while editing, and a footer of config keybindings.
func configView(m Model, w, h int) string {
	inner := w - 2
	if inner < 1 {
		inner = 1
	}
	bodyH := h - 4 // title rule, footer row, two borders
	if bodyH < 1 {
		bodyH = 1
	}

	lines := make([]string, 0, len(m.ConfigFields))
	for i, f := range m.ConfigFields {
		marker := "  "
		if i == m.ConfigSel {
			marker = "› "
		}
		val := f.Value
		if i == m.ConfigSel && m.ConfigEditing {
			val = m.Buffer + "█"
		}
		lines = append(lines, fmt.Sprintf("%s%s: %s", marker, f.Key, val))
	}
	if len(lines) == 0 {
		lines = []string{"  (no config fields)"}
	}

	var b strings.Builder
	b.WriteString("┌" + labelRule("config", inner) + "┐\n")
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		b.WriteString("│" + padTo(l, inner) + "│\n")
	}
	hints := " ↑/↓ select  ↵ edit  s save  esc close"
	b.WriteString("│" + padTo(hints, inner) + "│\n")
	b.WriteString("└" + strings.Repeat("─", inner) + "┘")
	return b.String()
}

// queueView renders the queue screen: a framed list of staged items
// (`"<prompt>"  @<repo>`) with the ›-marker on QueueSel. While adding, it shows
// the current field entry (prompt or repo) with a cursor, and a footer of
// queue keybindings.
func queueView(m Model, w, h int) string {
	inner := w - 2
	if inner < 1 {
		inner = 1
	}
	bodyH := h - 4 // title rule, footer row, two borders
	if bodyH < 1 {
		bodyH = 1
	}

	lines := make([]string, 0, len(m.QueueItems))
	for i, it := range m.QueueItems {
		marker := "  "
		if i == m.QueueSel {
			marker = "› "
		}
		lines = append(lines, fmt.Sprintf("%s%q  @%s", marker, it.Prompt, it.Repo))
	}
	if len(lines) == 0 {
		lines = []string{"  (queue empty)"}
	}

	var b strings.Builder
	b.WriteString("┌" + labelRule("queue", inner) + "┐\n")
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		b.WriteString("│" + padTo(l, inner) + "│\n")
	}
	var hints string
	if m.QueueAdding {
		if m.addStage == 0 {
			hints = " prompt: " + m.Buffer + "█"
		} else {
			hints = " repo: " + m.Buffer + "█"
		}
	} else {
		hints = " ↑/↓  a add  d dispatch  x remove  esc close"
	}
	b.WriteString("│" + padTo(hints, inner) + "│\n")
	b.WriteString("└" + strings.Repeat("─", inner) + "┘")
	return b.String()
}

// browseView renders the directory-browser overlay: a framed list of
// subdirectories with the ›-marker on BrowseSel, the current dir as a title,
// and a footer of browse keybindings.
func browseView(m Model, w, h int) string {
	inner := w - 2
	if inner < 1 {
		inner = 1
	}
	bodyH := h - 4 // title rule, footer row, two borders
	if bodyH < 1 {
		bodyH = 1
	}

	title := m.BrowseDir
	if title == "" {
		title = "(default)"
	}

	lines := make([]string, 0, len(m.BrowseEntries))
	for i, e := range m.BrowseEntries {
		marker := "  "
		if i == m.BrowseSel {
			marker = "› "
		}
		lines = append(lines, marker+e.Name+"/")
	}
	if len(lines) == 0 {
		lines = []string{"  (no subdirectories)"}
	}

	var b strings.Builder
	b.WriteString("┌" + labelRule("choose dir: "+title, inner) + "┐\n")
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		b.WriteString("│" + padTo(l, inner) + "│\n")
	}
	var hints string
	if m.MakingDir {
		hints = " new dir: " + m.Buffer + "█"
	} else {
		hints = " ↑/↓  ↵ open  h up  c choose  m new-dir  esc cancel"
	}
	b.WriteString("│" + padTo(hints, inner) + "│\n")
	b.WriteString("└" + strings.Repeat("─", inner) + "┘")
	return b.String()
}
