package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// binding is one row of the key help: its keys, what they do, and the help
// column it sits in. splitOnly bindings need the reading pane.
type binding struct {
	keys, desc, group string
	splitOnly         bool
}

// helpGroups are the key help's columns, in order.
var helpGroups = []string{"Move", "Ticket", "View"}

var bindings = []binding{
	{keys: "j/k", desc: "up / down", group: "Move"},
	{keys: "g/G", desc: "top / end", group: "Move"},
	{keys: "0-2", desc: "jump to panel", group: "Move", splitOnly: true},
	{keys: "tab", desc: "reader / next field", group: "Move", splitOnly: true},
	{keys: "esc", desc: "back to the list", group: "Move"},
	{keys: "/", desc: "filter", group: "Move"},
	{keys: "p", desc: "projects", group: "Move"},
	{keys: "enter", desc: "run action", group: "Ticket"},
	{keys: "e", desc: "edit", group: "Ticket"},
	{keys: "f", desc: "attachments", group: "Ticket"},
	{keys: "o", desc: "open in browser", group: "Ticket"},
	{keys: "r", desc: "reload", group: "Ticket"},
	{keys: "v", desc: "cards / markdown", group: "View", splitOnly: true},
	{keys: "b", desc: "group by", group: "View"},
	{keys: "=", desc: "fit list", group: "View"},
	{keys: "?", desc: "this help", group: "View"},
	{keys: "q", desc: "quit", group: "View"},
}

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
	case m.filtering:
		return "FILTER", filterColor, [][2]string{{"enter", "apply"}, {"esc", "done"}}
	case m.focusReader && m.fieldKey != "":
		return "EDIT", editColor, [][2]string{{"tab", "next field"}, {"enter", "edit"}, {"esc", "list"}}
	case m.focusReader:
		return "READ", readColor, [][2]string{{"j/k", "scroll"}, {"tab", "fields"}, {"esc", "list"}, {"?", "keys"}}
	case m.focusNav:
		return "VIEWS", m.accentStyle.GetForeground(), [][2]string{{"j/k", "move"}, {"enter", "open"}, {"esc", "list"}, {"?", "keys"}}
	}
	return "NORMAL", m.accentStyle.GetForeground(), [][2]string{{"enter", "act"}, {"e", "edit"}, {"/", "filter"}, {"p", "proj"}, {"?", "keys"}}
}

// statusline is the bottom bar: a mode pill, where the list is and what it
// shows, then the key hints for what has focus, or the latest status message
// in their place.
func (m *Model) statusline() string {
	name, pillColor, hints := m.mode()
	pill := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(pillColor).Render(" " + name + " ")
	sep := dimStyle.Render(" │ ")
	segs := []string{m.sym.icon(iconView) + m.viewName()}
	if m.filtering {
		segs = append(segs, m.filterInput.View())
	} else if f := m.filterInput.Value(); f != "" {
		segs = append(segs, m.sym.icon(iconFilter)+f)
	}
	if m.groupBy != "" {
		segs = append(segs, m.sym.icon(iconGroup)+m.groupBy)
	}
	segs = append(segs, fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks)))
	if s := m.sym.summary(m.linkedAgents(), m.frame); s != "" {
		segs = append(segs, s)
	}
	left := pill + " " + strings.Join(segs, sep)

	right := titleStyle.Render(ticket.OneLine(m.status))
	if m.status == "" {
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = m.accentStyle.Render(h[0]) + " " + dimStyle.Render(h[1])
		}
		right = strings.Join(parts, "  ")
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		// Status messages matter more than where the list is.
		if m.status == "" {
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
	split := !m.deps.NoPreview
	keyW := 0
	for _, b := range bindings {
		keyW = max(keyW, ansi.StringWidth(b.keys))
	}
	keyStyle := m.accentStyle.Width(keyW + 2)
	head := lipgloss.NewStyle().Bold(true).Foreground(editColor)
	cols := make([]string, len(helpGroups))
	for i, g := range helpGroups {
		lines := []string{head.Render(g)}
		for _, b := range bindings {
			if b.group == g && (split || !b.splitOnly) {
				lines = append(lines, keyStyle.Render(b.keys)+b.desc)
			}
		}
		cols[i] = lipgloss.NewStyle().PaddingRight(4).Render(strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return m.accentStyle.Render("Keys") + "\n\n" + body + "\n\n" + dimStyle.Render("any key closes")
}
