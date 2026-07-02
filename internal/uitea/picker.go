package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// pickerModel is the project selector for spawning a new agent. It shows the
// unified project list (local checkouts, remote checkouts, GitHub repos,
// in-use repos) with a type-to-filter, plus a "(no repo)" option. After a
// project is chosen the caller collects the task, then spawns.
type pickerModel struct {
	all      []core.Project // index 0 is the synthetic "(no repo)" option
	filter   string
	cursor   int
	choosing bool   // true = picking a project; false = typing the task
	repo     string // chosen repo (once choosing is done)
	task     string // task buffer while typing
}

var noRepoProject = core.Project{Label: "(no repo — task only)", Repo: "", Origin: ""}

func newPicker(projects []core.Project) pickerModel {
	all := append([]core.Project{noRepoProject}, projects...)
	return pickerModel{all: all, choosing: true}
}

// matches returns projects whose label contains the filter (the "(no repo)"
// option always matches so it stays reachable).
func (p pickerModel) matches() []core.Project {
	if p.filter == "" {
		return p.all
	}
	f := strings.ToLower(p.filter)
	var out []core.Project
	for i, pr := range p.all {
		if i == 0 || strings.Contains(strings.ToLower(pr.Label), f) {
			out = append(out, pr)
		}
	}
	return out
}

func (p *pickerModel) clamp() {
	n := len(p.matches())
	switch {
	case n == 0:
		p.cursor = 0
	case p.cursor < 0:
		p.cursor = 0
	case p.cursor >= n:
		p.cursor = n - 1
	}
}

// pickerAction is what a keystroke asks the enclosing model to do.
type pickerAction int

const (
	pickerNone   pickerAction = iota
	pickerCancel              // esc
	pickerSpawn               // task submitted → spawn in the chosen project
)

// key advances the picker. While choosing: arrows move, runes filter, enter
// selects the highlighted project and switches to task entry. While typing the
// task: runes/backspace edit, enter submits (spawn), esc cancels either stage.
func (p pickerModel) key(s string, r rune) (pickerModel, pickerAction) {
	if p.choosing {
		switch s {
		case "esc":
			return p, pickerCancel
		case "up":
			p.cursor--
			p.clamp()
		case "down":
			p.cursor++
			p.clamp()
		case "enter":
			m := p.matches()
			if p.cursor >= 0 && p.cursor < len(m) {
				p.repo = m[p.cursor].Repo
				p.choosing = false // move to task entry
			}
		case "backspace":
			if n := len(p.filter); n > 0 {
				p.filter = p.filter[:n-1]
				p.clamp()
			}
		case "rune":
			p.filter += string(r)
			p.clamp()
		}
		return p, pickerNone
	}
	// task-entry stage
	switch s {
	case "esc":
		return p, pickerCancel
	case "enter":
		if strings.TrimSpace(p.task) != "" {
			return p, pickerSpawn
		}
	case "backspace":
		if n := len(p.task); n > 0 {
			p.task = p.task[:n-1]
		}
	case "rune":
		p.task += string(r)
	}
	return p, pickerNone
}
