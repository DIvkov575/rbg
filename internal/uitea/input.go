package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// inputKind is what the input overlay composes.
type inputKind int

const (
	inputCreate inputKind = iota // name → repo → task, then Create
	inputSend                    // single task, then Send to target
)

// inputStage tracks which create field is being entered. Names are auto-derived
// by the engine from the task, so the create flow only collects repo then task.
type inputStage int

const (
	stageRepo inputStage = iota
	stageTask
)

// inputModel is the text-entry overlay. For create it walks name→repo→task; for
// send it collects one task for the target agent.
type inputModel struct {
	kind   inputKind
	target string // agent name (send)
	stage  inputStage
	repo   string // committed create field
	buf    string
}

func newCreateInput() inputModel { return inputModel{kind: inputCreate, stage: stageRepo} }
func newSendInput(target string) inputModel {
	return inputModel{kind: inputSend, target: target}
}

// createDone is returned when the create flow finishes, carrying the spec.
type createDone struct{ spec core.Agent }

// sendDone is returned when the send flow finishes.
type sendDone struct {
	target, task string
}

// key handles one keystroke. It returns the updated input, and one of:
//   - done=false: still editing (stay in input mode)
//   - done=true, result=createDone/sendDone: submit
//   - done=true, result=nil: cancelled (Esc)
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
		return in.submit()
	case "rune":
		in.buf += string(r)
		return in, false, nil
	}
	return in, false, nil
}

func (in inputModel) submit() (inputModel, bool, any) {
	val := strings.TrimSpace(in.buf)
	if in.kind == inputSend {
		if val == "" {
			return in, false, nil // task required; stay
		}
		return in, true, sendDone{target: in.target, task: val}
	}
	// create: repo(optional) → task. The name is auto-derived from the task by
	// the engine, so it isn't collected here.
	switch in.stage {
	case stageRepo:
		in.repo = val // optional
		in.buf = ""
		in.stage = stageTask
		return in, false, nil
	default: // stageTask
		if val == "" {
			return in, false, nil // task required; stay
		}
		return in, true, createDone{spec: core.Agent{Repo: in.repo, Task: val}}
	}
}

// prompt is the label for the current field.
func (in inputModel) prompt() string {
	if in.kind == inputSend {
		return "Follow-up to " + in.target
	}
	switch in.stage {
	case stageRepo:
		return "New agent · repo, blank for none (1/2)"
	default:
		return "New agent · task (2/2)"
	}
}
