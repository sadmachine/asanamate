package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/updatecheck"
)

type updateAvailableMsg struct{ version string }

func checkForUpdate(current string) tea.Cmd {
	return func() tea.Msg {
		version, _ := updatecheck.Check(current)
		return updateAvailableMsg{version: version}
	}
}
