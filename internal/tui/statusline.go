package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// mode returns the statusline's mode name and pill style, and the key hints
// for what has focus.
func (m *Model) mode() (name string, pill lipgloss.Style, hints [][2]string) {
	targets := [2]string{keyLabel("main", "down", "up"), "targets"}
	scroll := [2]string{keyLabel("main", "scroll_down", "scroll_up"), "scroll"}
	pane := [2]string{keyLabel("main", "next_pane"), "pane"}
	list := [2]string{keyLabel("main", "focus_list"), "list"}
	switch {
	case m.loading && m.tasks == nil:
		// Only quit works until the first tasks land.
		return "NORMAL", normalPill, [][2]string{{keyLabel("main", "quit"), "quit"}}
	case m.filtering:
		return "FILTER", filterPill, [][2]string{{typedLabel("filter", "done"), "apply"}, {typedLabel("filter", "cancel"), "cancel"}, {typedLabel("filter", "help"), "guide"}}
	case m.focusReader && m.fieldKey == commentKey:
		return "EDIT", editPill, [][2]string{targets, {keyLabel("main", "open"), "add comment"}, scroll, list}
	case m.focusReader && m.editableTarget():
		return "EDIT", editPill, [][2]string{targets, {keyLabel("main", "open"), "edit"}, scroll, list}
	case m.focusReader:
		if m.readerView != config.ViewMarkdown {
			hints := [][2]string{targets}
			if t, ok := m.selectedDetail(); ok && m.selectedComment(t) != nil {
				hints = append(hints, m.keyHints("copy_comment")...)
			}
			return "READ", readPill, append(hints, scroll, pane, list)
		}
		return "READ", readPill, [][2]string{{keyLabel("main", "down", "up"), "scroll"}, pane, list}
	case m.focusNav:
		return "VIEWS", normalPill, [][2]string{{keyLabel("views", "down", "up"), "move"}, {keyLabel("views", "open"), "open"}, list, {keyLabel("main", "help"), "keys"}}
	}
	return "NORMAL", normalPill, m.keyHints("action", "edit", "log_time", "copy_link", "filter", "projects", "help")
}

// statusline is the bottom bar: a mode pill, where the list is and what it
// shows, then the key hints for what has focus, or the latest status message
// in their place.
func (m *Model) statusline() string {
	name, pillStyle, hints := m.mode()
	pill := pillStyle.Render(" " + name + " ")
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
	all := keyBindings()
	partner := map[string]binding{}
	for _, b := range all {
		if b.Pair != "" {
			partner[b.Pair] = b
		}
	}
	type row struct{ label, desc, group string }
	var rows []row
	keyW := 0
	for _, b := range all {
		if b.Pair != "" || !m.available(b) {
			continue
		}
		label := keymap.Label(b.keys...)
		if p, ok := partner[b.Name]; ok && len(p.keys) > 0 {
			label = keymap.Label(b.keys[0], p.keys[0])
		}
		rows = append(rows, row{label, b.Desc, b.Group})
		keyW = max(keyW, ansi.StringWidth(label))
	}
	keyStyle := m.accentStyle.Width(keyW + 2)
	head := editPill.Reverse(false)
	cols := make([]string, len(helpGroups))
	for i, g := range helpGroups {
		lines := []string{head.Render(g)}
		for _, r := range rows {
			if r.group == g {
				lines = append(lines, keyStyle.Render(r.label)+r.desc)
			}
		}
		cols[i] = lipgloss.NewStyle().PaddingRight(4).Render(strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return m.accentStyle.Render("Keys") + "\n\n" + body + "\n\n" + dimStyle.Render("any key closes")
}
