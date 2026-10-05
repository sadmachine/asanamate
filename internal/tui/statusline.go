package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Mode pill colors: accent for the list, then ANSI cyan, magenta, and yellow.
var (
	readColor   = lipgloss.Color("6")
	editColor   = lipgloss.Color("5")
	filterColor = warnStyle.GetForeground()
)

// mode returns the statusline's mode name and pill color, and the key hints
// for what has focus.
func (m *Model) mode() (name string, pill color.Color, hints [][2]string) {
	switch {
	case m.loading && m.tasks == nil:
		// Only q works until the first tasks land.
		return "NORMAL", m.accentStyle.GetForeground(), [][2]string{{"q", "quit"}}
	case m.filtering:
		return "FILTER", filterColor, [][2]string{{"enter", "apply"}, {"esc", "done"}}
	case m.focusReader && m.fieldKey == commentKey:
		return "EDIT", editColor, [][2]string{{"j/k", "targets"}, {"enter", "add comment"}, {"↑/↓", "scroll"}, {"esc", "list"}}
	case m.focusReader && m.editableTarget():
		return "EDIT", editColor, [][2]string{{"j/k", "targets"}, {"enter", "edit"}, {"↑/↓", "scroll"}, {"esc", "list"}}
	case m.focusReader:
		if m.readerView != config.ViewMarkdown {
			hints := [][2]string{{"j/k", "targets"}}
			if t, ok := m.selectedDetail(); ok && m.selectedComment(t) != nil {
				hints = append(hints, m.keyHints("y")...)
			}
			return "READ", readColor, append(hints, [2]string{"↑/↓", "scroll"}, [2]string{"tab", "pane"}, [2]string{"esc", "list"})
		}
		return "READ", readColor, [][2]string{{"j/k", "scroll"}, {"tab", "pane"}, {"esc", "list"}}
	case m.focusNav:
		return "VIEWS", m.accentStyle.GetForeground(), [][2]string{{"j/k", "move"}, {"enter", "open"}, {"esc", "list"}, {"?", "keys"}}
	}
	return "NORMAL", m.accentStyle.GetForeground(), m.keyHints("space", "e", "t", "c", "/", "p", "?")
}

// statusline is the bottom bar: a mode pill, where the list is and what it
// shows, then the key hints for what has focus, or the latest status message
// in their place.
func (m *Model) statusline() string {
	name, pillColor, hints := m.mode()
	pill := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(pillColor).Render(" " + name + " ")
	sep := dimStyle.Render(" │ ")
	segs := []string{m.sym.icon(iconView) + m.viewName()}
	if _, _, split := m.paneWidths(); !split && len(m.deps.State.SavedViews) > 0 {
		segs = append(segs, m.savedViewLabel())
	}
	if m.filtering {
		segs = append(segs, m.filterInput.View())
	} else if f := m.filterInput.Value(); f != "" {
		segs = append(segs, m.sym.icon(iconFilter)+f)
	}
	if m.groupBy != "" {
		segs = append(segs, m.sym.icon(iconGroup)+m.groupBy)
	}
	if m.sortBy.By != "" {
		segs = append(segs, "sort: "+sortLabel(m.sortBy))
	}
	segs = append(segs, fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks)), m.refreshLabel())
	if s := m.sym.summary(m.linkedAgents(), m.frame); s != "" {
		segs = append(segs, s)
	}
	left := pill + " " + strings.Join(segs, sep)

	status := m.status
	if len(m.notices) > 0 {
		status = m.notices[0]
	}
	right := titleStyle.Render(ticket.OneLine(status))
	if status == "" {
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = m.accentStyle.Render(h[0]) + " " + dimStyle.Render(h[1])
		}
		right = strings.Join(parts, "  ")
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		// Status messages matter more than where the list is.
		if status == "" {
			return ansi.Truncate(left, m.width, "…")
		}
		return ansi.Truncate(pill+" "+right, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// viewName is the viewed project's name, or My Tasks.
func (m *Model) viewName() string {
	if m.viewProject != nil {
		return ticket.Clean(m.viewProject.Name)
	}
	return "My Tasks"
}

// linkedAgents returns the agents linked to any loaded ticket, each once:
// tickets can share a branch.
func (m *Model) linkedAgents() []agents.Agent {
	var linked []agents.Agent
	seen := map[agents.Agent]bool{}
	for _, t := range m.tasks {
		for _, a := range m.viewAgents(t) {
			if !seen[a] {
				seen[a] = true
				linked = append(linked, a)
			}
		}
	}
	return linked
}

// helpView renders the key help in one column per help group.
func (m *Model) helpView() string {
	var shown []binding
	keyW := 0
	for _, b := range keyBindings() {
		if (!b.splitOnly || !m.deps.NoPreview) && (b.keys[0] != "t" || m.deps.Config.TimeTrackingEnabled()) {
			shown = append(shown, b)
			keyW = max(keyW, ansi.StringWidth(b.helpLabel()))
		}
	}
	keyStyle := m.accentStyle.Width(keyW + 2)
	head := lipgloss.NewStyle().Bold(true).Foreground(editColor)
	cols := make([]string, len(helpGroups))
	for i, g := range helpGroups {
		lines := []string{head.Render(g)}
		for _, b := range shown {
			if b.group == g {
				lines = append(lines, keyStyle.Render(b.helpLabel())+b.desc)
			}
		}
		cols[i] = lipgloss.NewStyle().PaddingRight(4).Render(strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return m.accentStyle.Render("Keys") + "\n\n" + body + "\n\n" + dimStyle.Render("any key closes")
}
