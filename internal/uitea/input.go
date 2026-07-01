package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// inputKind is what the input overlay composes.
type inputKind int

const (
	inputCreate inputKind = iota // task for a new agent (repo already chosen)
	inputSend                    // follow-up task for a running agent
)

// inputModel is the single-line task-entry overlay. For create the repo was
// already chosen in the picker and is carried here; for send the target agent
// is carried. Both collect one task string.
type inputModel struct {
	kind   inputKind
	target string // agent name (send)
	repo   string // chosen repo (create)
	buf    string
}

func newCreateInput(repo string) inputModel { return inputModel{kind: inputCreate, repo: repo} }
func newSendInput(target string) inputModel { return inputModel{kind: inputSend, target: target} }

// createDone is returned when the create flow finishes, carrying the spec.
type createDone struct{ spec core.Agent }

// sendDone is returned when the send flow finishes.
type sendDone struct {
	target, task string
}

// key handles one keystroke. Returns updated input, done, and (on done) a
// createDone/sendDone, or nil on cancel (Esc). An empty task keeps the overlay
// open (task is required).
func (in inputModel) key(s string, r rune) (inputModel, bool, any) {
	switch s {
	case "esc":
		return in, true, nil // cancel
	case "backspace":
		if n := len(in.buf); n > 0 {
			in.buf = in.buf[:n-1]
		}
		return in, false, nil
	case "enter":
		task := strings.TrimSpace(in.buf)
		if task == "" {
			return in, false, nil // task required; stay
		}
		if in.kind == inputSend {
			return in, true, sendDone{target: in.target, task: task}
		}
		// name is auto-derived by the engine from the task.
		return in, true, createDone{spec: core.Agent{Repo: in.repo, Task: task}}
	case "rune":
		in.buf += string(r)
		return in, false, nil
	}
	return in, false, nil
}

// prompt is the label for the input overlay.
func (in inputModel) prompt() string {
	if in.kind == inputSend {
		return "Follow-up to " + in.target
	}
	if in.repo == "" {
		return "New agent · task (no repo)"
	}
	return "New agent · task · " + in.repo
}
