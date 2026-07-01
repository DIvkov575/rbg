package uitea

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the Bubble Tea dashboard over ops and blocks until the user
// quits. It uses the alternate screen buffer so the dashboard restores the
// terminal on exit.
func Run(ops Ops) error {
	p := tea.NewProgram(New(ops), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
