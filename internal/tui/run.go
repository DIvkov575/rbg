package tui

import (
	"os"

	"github.com/divkov575/rbg/internal/session"
)

// Deps are the loop's collaborators, injected so the loop stays thin.
type Deps struct {
	Fetch      func() ([]session.Session, error) // list agents
	Transcript func(name string) (string, error) // rendered transcript
	Attach     func(name string) error           // hand terminal to claude
}

// Run drives the dashboard until the user quits. It enters raw mode on the
// terminal fd, draws on each key, and fulfills model Actions via deps.
func Run(d Deps, io Stdio) error {
	sessions, err := d.Fetch()
	if err != nil {
		return err
	}
	m := New(sessions)

	restore, err := rawMode(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer func() { restore() }()

	draw(io.Out, m)
	for {
		k := readKey(io.In)
		var act Action
		m, act = Update(m, k)
		switch act {
		case ActionQuit:
			return nil
		case ActionLoadTranscript:
			if text, err := d.Transcript(m.SelectedName()); err == nil {
				m = m.SetTranscript(text)
			}
		case ActionRefresh:
			if s, err := d.Fetch(); err == nil {
				m = m.SetSessions(s)
			}
		case ActionAttach:
			name := m.SelectedName()
			restore()          // cooked mode for interactive claude
			_ = d.Attach(name) // blocks until the user exits claude
			newRestore, rerr := rawMode(os.Stdin.Fd())
			if rerr != nil {
				// Can't resume raw mode; the terminal is already cooked (safe)
				// from the restore() above, so exit rather than loop blind.
				return rerr
			}
			restore = newRestore // back to raw; defer closure sees the new value
		}
		draw(io.Out, m)
	}
}
