package uitea

import (
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// pickerModel is a filterable project chooser shown at the start of the create
// flow. It offers a "no repo" option plus the unified suggestions from the
// engine (local/remote checkouts, GitHub repos, in-use repos). Typing filters
// by label substring; up/down move; enter chooses.
type pickerModel struct {
	all    []core.Project // full suggestion list (index 0 is the synthetic "no repo")
	filter string
	cursor int
}

// noRepoProject is the always-present first choice: a repo-less agent.
var noRepoProject = core.Project{Label: "(no repo — task only)", Repo: "", Origin: ""}

func newPicker(projects []core.Project) pickerModel {
	all := append([]core.Project{noRepoProject}, projects...)
	return pickerModel{all: all}
}

// matches returns the projects whose label contains the filter (case-insensitive).
// The "no repo" option always matches so it's reachable regardless of filter.
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

// clamp keeps the cursor within the filtered bounds.
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

// pickerDone is returned when a project is chosen, carrying the selected repo.
type pickerDone struct{ repo string }

// key handles a keystroke. Returns updated picker, done, and (on done) a
// pickerDone with the chosen repo, or nil on cancel (Esc).
func (p pickerModel) key(s string, r rune) (pickerModel, bool, any) {
	// Arrows navigate; printable runes filter (so you can type any letter into
	// the filter without it being captured as a nav key).
	switch s {
	case "esc":
		return p, true, nil // cancel the whole create flow
	case "up":
		p.cursor--
		p.clamp()
	case "down":
		p.cursor++
		p.clamp()
	case "enter":
		m := p.matches()
		if p.cursor >= 0 && p.cursor < len(m) {
			return p, true, pickerDone{repo: m[p.cursor].Repo}
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
	return p, false, nil
}
