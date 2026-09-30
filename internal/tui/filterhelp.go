package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/filter"
)

// filterHints keeps available fields visible next to the filter editor.
func (m *Model) filterHints() []string {
	if !m.filtering || m.width < 1 || m.bodyHeight() < 4 {
		return nil
	}
	parts := []string{"FILTER BY"}
	for _, key := range filter.Keys() {
		parts = append(parts, key+":")
	}
	parts = append(parts, "? guide")
	var lines []string
	var line []string
	width := 0
	for _, part := range parts {
		gap := 0
		if len(line) > 0 {
			gap = 2
		}
		if width+gap+ansi.StringWidth(part) > m.width && len(line) > 0 {
			lines = append(lines, m.styleFilterHint(line))
			line, width, gap = nil, 0, 0
		}
		line = append(line, part)
		width += gap + ansi.StringWidth(part)
	}
	if len(line) > 0 {
		lines = append(lines, m.styleFilterHint(line))
	}
	if maxLines := m.bodyHeight() - 3; len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = m.styleFilterHint([]string{"? guide"})
	}
	return lines
}

func (m *Model) filterHintHeight() int { return len(m.filterHints()) }

func (m *Model) styleFilterHint(parts []string) string {
	styled := make([]string, len(parts))
	for i, part := range parts {
		switch part {
		case "FILTER BY":
			styled[i] = dimStyle.Render(part)
		case "? guide":
			styled[i] = m.accentStyle.Render("?") + dimStyle.Render(" guide")
		default:
			styled[i] = m.accentStyle.Render(part)
		}
	}
	return strings.Join(styled, "  ")
}

func (m *Model) filterHelpView() string {
	key := func(s string) string { return m.accentStyle.Render(s) }
	if m.width < 72 {
		return key("Filters") + "\n\n" +
			key("words") + " search titles\n" +
			key("section:  project:") + "\n" +
			key("assignee:  tag:") + " match names\n" +
			key("is:open  is:done") + "\n" +
			key("agent:any  agent:none") + "\n" +
			key("agent:<state>") + "\n" +
			dimStyle.Render("waiting, working, completed,") + "\n" +
			dimStyle.Render("idle, unknown") + "\n\n" +
			"Spaces combine terms. " + key("-term") + " excludes.\n" +
			key(`"two words"`) + " groups words.\n\n" +
			dimStyle.Render(`is:open section:"in progress"`) + "\n" +
			dimStyle.Render(`-tag:blocked`) + "\n" +
			dimStyle.Render("any key closes")
	}
	return key("Filters") + "\n\n" +
		key("words") + " search titles\n" +
		key("section:  project:  assignee:  tag:") + " match names\n" +
		key("is:open  is:done") + " filter completion\n" +
		key("agent:any  agent:none  agent:<state>") + " filter linked agents\n" +
		dimStyle.Render("States: waiting, working, completed, idle, unknown") + "\n\n" +
		"Spaces combine terms. " + key("-term") + " excludes. " + key(`"two words"`) + " groups words.\n\n" +
		dimStyle.Render(`is:open section:"in progress" -tag:blocked`) + "\n\n" +
		dimStyle.Render("any key closes · return to filter")
}
