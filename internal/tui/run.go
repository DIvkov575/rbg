package tui

import (
	"os"
	"time"

	"github.com/divkov575/rbg/internal/session"
)

// Deps are the loop's collaborators, injected so the loop stays thin.
type Deps struct {
	Fetch      func() ([]session.Session, error)                   // list agents
	Transcript func(name string) (string, error)                   // rendered transcript
	Attach     func(name string) error                             // hand terminal to claude
	Launch     func(dir, task string) error                        // spawn a new agent in dir
	Kill       func(name string) error                             // forget/terminate an agent
	Dirs       func(dir string) (string, string, []DirItem, error) // list subdirs: returns dir, parent, entries
	MakeDir    func(dir string) (string, error)                    // create a dir, returns its abs path
	Now        func() string                                       // RFC3339 timestamp (defaults to time.Now)
}

// Run drives the dashboard until the user quits. It enters raw mode on the
// terminal fd, draws on each key, and fulfills model Actions via deps.
func Run(d Deps, io Stdio) error {
	sessions, err := d.Fetch()
	if err != nil {
		return err
	}
	now := d.Now
	if now == nil {
		now = func() string { return time.Now().UTC().Format(time.RFC3339) }
	}
	m := New(sessions)
	stamp := func(mm Model) Model {
		w, h := termSize(os.Stdin.Fd())
		return mm.SetSize(w, h).withNow(now())
	}
	// loadSelected fetches and stores the selected agent's transcript so the
	// right pane is never empty on load/refresh.
	loadSelected := func(mm Model) Model {
		if name := mm.SelectedName(); name != "" {
			if text, err := d.Transcript(name); err == nil {
				mm = mm.SetTranscript(text)
			}
		}
		return mm
	}
	m = loadSelected(stamp(m))

	restore, err := rawMode(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer func() { restore() }()

	draw(io.Out, m)
	for {
		raw := readRaw(io.In)
		if raw == nil {
			return nil // EOF → quit
		}
		var act Action
		if m.Browsing && m.MakingDir {
			// Name-entry sub-mode: reuse the text-input decoder so letters like
			// 'j'/'k'/'c' are typed literally rather than triggering browse nav.
			k, r, isRune := decodeKeyInput(raw)
			switch {
			case isRune:
				m = m.InputRune(r)
			case k == KeyBackspace:
				m = m.Backspace()
			default:
				m, act = Update(m, k)
			}
		} else if m.Browsing {
			m, act = Update(m, decodeKeyBrowse(raw))
		} else if m.Input {
			k, r, isRune := decodeKeyInput(raw)
			switch {
			case isRune:
				m = m.InputRune(r)
			case k == KeyBackspace:
				m = m.Backspace()
			default:
				m, act = Update(m, k)
			}
		} else {
			m, act = Update(m, decodeKey(raw))
		}
		switch act {
		case ActionQuit:
			return nil
		case ActionLoadTranscript:
			if text, err := d.Transcript(m.SelectedName()); err == nil {
				m = m.SetTranscript(text)
			}
		case ActionRefresh:
			if s, err := d.Fetch(); err == nil {
				m = loadSelected(m.SetSessions(s))
			}
		case ActionBrowse:
			if d.Dirs != nil {
				if dir, parent, items, err := d.Dirs(m.BrowseDir); err == nil {
					m = m.SetBrowse(dir, parent, items)
				}
			}
		case ActionMkdir:
			if d.MakeDir != nil {
				newpath, err := d.MakeDir(m.BrowseDir + "/" + m.DirName())
				if err == nil {
					m = m.EnteredDir(newpath)
					if d.Dirs != nil {
						if dir, parent, items, derr := d.Dirs(m.BrowseDir); derr == nil {
							m = m.SetBrowse(dir, parent, items)
						}
					}
				}
				// on error: stay in making-dir mode (buffer preserved) so the
				// user can correct the name or Esc out.
			}
		case ActionLaunch:
			if d.Launch != nil {
				_ = d.Launch(m.ChosenDir(), m.LaunchTask())
			}
			if s, err := d.Fetch(); err == nil {
				m = m.SetSessions(s)
			}
		case ActionKill:
			if d.Kill != nil {
				_ = d.Kill(m.SelectedName())
			}
			if s, err := d.Fetch(); err == nil {
				m = m.SetSessions(s)
			}
		case ActionAttach:
			name := m.SelectedName()
			restore()          // cooked mode for interactive claude
			_ = d.Attach(name) // blocks until the user exits claude
			newRestore, rerr := rawMode(os.Stdin.Fd())
			if rerr != nil {
				return rerr
			}
			restore = newRestore
		}
		m = stamp(m)
		draw(io.Out, m)
	}
}
