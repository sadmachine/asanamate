package tui

import "charm.land/lipgloss/v2"

// Colors are ANSI numbers, so they follow the terminal's own color scheme.
var (
	borderColor = lipgloss.Color("8")
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	// Bright cyan sets working agents apart from completed ones.
	workingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	loadingStyle  = modalStyle.BorderForeground(warnStyle.GetForeground()).Padding(2, 4)
)
