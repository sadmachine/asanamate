package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

// Styles from the active palette. applyPalette sets them; New calls it once,
// since a process runs one Model. init applies the defaults so code that runs
// before New, and tests, see the built-in dark theme.
var (
	borderColor   color.Color
	okStyle       lipgloss.Style
	warnStyle     lipgloss.Style
	errorStyle    lipgloss.Style
	workingStyle  lipgloss.Style
	dimStyle      lipgloss.Style
	selectedStyle lipgloss.Style
	loadingStyle  lipgloss.Style
	authorStyles  []lipgloss.Style
	// Mode pills: normal (also VIEWS and loading), filter, read, and edit.
	normalPill, filterPill, readPill, editPill lipgloss.Style
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	modalStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func init() { applyPalette(config.Config{}.Palette(true)) }

// applyPalette sets the package styles from p.
func applyPalette(p config.Palette) {
	c := p.Colors
	borderColor = lipStyle(c.Border).GetForeground()
	okStyle, warnStyle, errorStyle = lipStyle(c.OK), lipStyle(c.Warn), lipStyle(c.Error)
	workingStyle, dimStyle, selectedStyle = lipStyle(c.Working), lipStyle(c.Muted), lipStyle(c.Highlight)
	loadingStyle = modalStyle.BorderForeground(warnStyle.GetForeground()).Padding(2, 4)
	authorStyles = authorStyles[:0]
	for _, s := range c.Authors {
		authorStyles = append(authorStyles, lipStyle(s))
	}
	if len(authorStyles) == 0 {
		authorStyles = append(authorStyles, lipgloss.NewStyle())
	}
	normalPill, filterPill = pillStyle(c.Mode.Normal), pillStyle(c.Mode.Filter)
	readPill, editPill = pillStyle(c.Mode.Read), pillStyle(c.Mode.Edit)
}

// lipStyle draws a themed role.
func lipStyle(s config.Style) lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.FG != "" {
		st = st.Foreground(lipgloss.Color(s.FG))
	}
	if s.BG != "" {
		st = st.Background(lipgloss.Color(s.BG))
	}
	for _, attr := range []struct {
		on  *bool
		set func(lipgloss.Style, bool) lipgloss.Style
	}{
		{s.Bold, lipgloss.Style.Bold}, {s.Italic, lipgloss.Style.Italic}, {s.Underline, lipgloss.Style.Underline},
		{s.Faint, lipgloss.Style.Faint}, {s.Reverse, lipgloss.Style.Reverse},
	} {
		if attr.on != nil {
			st = attr.set(st, *attr.on)
		}
	}
	return st
}

// pillStyle draws a mode pill: bold unless the style says otherwise, and
// reversed when it has no background, so its foreground fills it.
func pillStyle(s config.Style) lipgloss.Style {
	bold := true
	st := lipStyle(s.Over(config.Style{Bold: &bold}))
	if s.BG == "" {
		st = st.Reverse(true)
	}
	return st
}
