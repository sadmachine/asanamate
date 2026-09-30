package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// wideWidth is the narrowest terminal that shows the views panel.
const wideWidth = 160

// navW is the views panel's width, border included.
const navW = 26

// maxNavProjects caps the recent projects the views panel lists; p finds the rest.
const maxNavProjects = 8

// navKeys are the keys the views panel handles while it has focus.
var navKeys = []string{"j", "k", "down", "up", "g", "G", "home", "end", "enter", "l"}

// navItem is one row of the views panel: a view to open, a grouping to
// pick, or, with neither, a heading or an agent count.
type navItem struct {
	label, right string
	project      *asana.Ref // nil with isView means My Tasks
	isView       bool
	group        *string
	heading      bool
	active       bool
}

func (it navItem) selectable() bool { return it.isView || it.group != nil }

// showNav reports whether the layout has room for the views panel.
func (m *Model) showNav() bool {
	return !m.deps.NoPreview && m.width >= wideWidth
}

// navWidth is the width the views panel takes, 0 when it is hidden.
func (m *Model) navWidth() int {
	if m.showNav() {
		return navW
	}
	return 0
}

// navItems lists My Tasks and the recent projects, the groupings, and the
// linked agents by state.
func (m *Model) navItems() []navItem {
	count := fmt.Sprint(len(m.visible))
	items := []navItem{{label: "My Tasks", isView: true, active: m.viewProject == nil}}
	if m.viewProject == nil {
		items[0].right = count
	}
	items = append(items, navItem{label: "Projects", right: "p", heading: true})
	names := map[string]string{}
	for _, p := range m.projects {
		names[p.GID] = p.Name
	}
	var gids []string
	if m.viewProject != nil {
		names[m.viewProject.GID] = cmp.Or(names[m.viewProject.GID], m.viewProject.Name)
		gids = append(gids, m.viewProject.GID)
	}
	for _, gid := range m.deps.State.RecentProjects {
		if names[gid] != "" && !slices.Contains(gids, gid) && len(gids) < maxNavProjects {
			gids = append(gids, gid)
		}
	}
	for _, gid := range gids {
		it := navItem{label: ticket.OneLine(names[gid]), project: &asana.Ref{GID: gid, Name: names[gid]}, isView: true}
		if gidOf(m.viewProject) == gid {
			it.active, it.right = true, count
		}
		items = append(items, it)
	}
	if m.loadingProjects {
		items = append(items, navItem{label: dimStyle.Render("loading…")})
	} else {
		hidden := 0
		for _, p := range m.projects {
			if !slices.Contains(gids, p.GID) {
				hidden++
			}
		}
		items = appendMore(items, hidden)
	}

	items = append(items, navItem{label: "Group by", right: "b", heading: true})
	groups := uniqueFold(append(append([]string{""}, m.deps.Config.List.Fields...), m.deps.Config.List.GroupBy, m.groupBy))
	for _, g := range groups {
		items = append(items, navItem{label: cmp.Or(ticket.OneLine(g), "none"), group: &g, active: strings.EqualFold(g, m.groupBy)})
	}
	hidden := 0
	for _, g := range m.groupings() {
		if !slices.ContainsFunc(groups, func(o string) bool { return strings.EqualFold(o, g) }) {
			hidden++
		}
	}
	items = appendMore(items, hidden)

	if m.deps.Config.AgentsEnabled() {
		items = append(items, navItem{label: "Agents", heading: true})
		linked := m.linkedAgents()
		for _, st := range agents.States {
			if n := countState(linked, st); n > 0 {
				items = append(items, navItem{label: stateStyles[st].Render(m.sym.agent(st, m.frame)) + " " + string(st), right: fmt.Sprint(n)})
			}
		}
		if len(linked) == 0 {
			items = append(items, navItem{label: dimStyle.Render("none running")})
		}
	}
	return items
}

// appendMore adds a dim row counting the hidden entries a section's picker
// offers, when there are any.
func appendMore(items []navItem, hidden int) []navItem {
	if hidden == 0 {
		return items
	}
	return append(items, navItem{label: dimStyle.Render(fmt.Sprintf("+%d more", hidden))})
}

// navView renders the views panel's rows, width by height cells.
func (m *Model) navView(width, height int) string {
	items := m.navItems()
	lines := make([]string, 0, len(items))
	for i, it := range items {
		if it.heading {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.navRule(it.label, it.right, width))
			continue
		}
		mark := "  "
		switch {
		case it.group != nil && it.active:
			mark = m.accentStyle.Render("●") + " "
		case it.group != nil:
			mark = dimStyle.Render("○") + " "
		case it.isView && it.active:
			mark = m.accentStyle.Render("▌") + " "
		}
		label := it.label
		if it.active {
			label = titleStyle.Render(label)
		}
		right := dimStyle.Render(it.right)
		room := width - ansi.StringWidth(mark) - ansi.StringWidth(it.right) - 1
		label = ansi.Truncate(label, max(room, 1), "…")
		line := mark + label + strings.Repeat(" ", max(width-ansi.StringWidth(mark)-ansi.StringWidth(label)-ansi.StringWidth(it.right), 1)) + right
		if m.focusNav && i == m.navIndex(items) {
			line = selectedStyle.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// navRule is a heading rule with its section's key, if any, set in the
// rule's right end: ── Projects ──── [p] ─
func (m *Model) navRule(title, key string, width int) string {
	if key == "" {
		return m.rule(title, width, false)
	}
	line := m.sym.border.Top
	tail := " [" + key + "] " + line
	return m.rule(title, width-ansi.StringWidth(tail), false) +
		dimStyle.Render(" [") + m.accentStyle.Render(key) + dimStyle.Render("] ") +
		lipgloss.NewStyle().Foreground(borderColor).Render(line)
}

// navIndex is the index in items of the navCursor'th selectable item.
func (m *Model) navIndex(items []navItem) int {
	n := 0
	for i, it := range items {
		if it.selectable() {
			if n == m.navCursor {
				return i
			}
			n++
		}
	}
	return -1
}

// focusNavPanel gives the views panel focus, starting on the viewed view.
func (m *Model) focusNavPanel() {
	m.focusNav, m.navCursor = true, 0
	n := 0
	for _, it := range m.navItems() {
		if !it.selectable() {
			continue
		}
		if it.isView && it.active {
			m.navCursor = n
			return
		}
		n++
	}
}

// updateNav handles a key while the views panel has focus.
func (m *Model) updateNav(k string) tea.Cmd {
	items := m.navItems()
	var selectable []navItem
	for _, it := range items {
		if it.selectable() {
			selectable = append(selectable, it)
		}
	}
	// The rows can shrink under the cursor, such as when a grouping by a
	// custom field is dropped.
	m.navCursor = min(m.navCursor, len(selectable)-1)
	switch k {
	case "j", "down":
		m.navCursor = min(m.navCursor+1, len(selectable)-1)
	case "k", "up":
		m.navCursor = max(m.navCursor-1, 0)
	case "g", "home":
		m.navCursor = 0
	case "G", "end":
		m.navCursor = len(selectable) - 1
	case "enter", "l":
		it := selectable[m.navCursor]
		m.focusNav = false
		if it.group != nil {
			return m.pickedGroup(*it.group)
		}
		if it.active {
			return nil
		}
		return m.pickedProject(it.project)
	}
	return nil
}
