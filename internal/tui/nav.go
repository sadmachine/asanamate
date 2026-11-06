package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// wideWidth is the narrowest terminal that shows the Places panel.
const wideWidth = 160

// navW is the Places panel's width, border included.
const navW = 26

// maxNavProjects caps the recent projects the Places panel lists; p finds the rest.
const maxNavProjects = 8

// maxNavSavedViews caps named views; V offers the full list.
const maxNavSavedViews = 5

// navItem is one row of the Places panel: a place to open (Now, My Tasks,
// Activity, a saved view, or a project), or, with none, a heading or note.
type navItem struct {
	label, right string
	project      *asana.Ref // nil with isView means My Tasks
	isView       bool
	savedView    string
	heading      bool
	active       bool
	activity     bool
	now          bool
	dim          bool // the row does not apply to the open view
}

func (it navItem) selectable() bool {
	return it.isView || it.activity || it.now || it.savedView != ""
}

// showNav reports whether the layout has room for the Places panel.
func (m *Model) showNav() bool {
	return !m.navHidden && m.width >= wideWidth
}

// navWidth is the width the Places panel takes, 0 when it is hidden.
func (m *Model) navWidth() int {
	if m.showNav() {
		return navW
	}
	return 0
}

// navItems lists the places to go: Now, My Tasks, Activity, saved views,
// and recent projects. How the open place is grouped, sorted, or filtered lives
// in the statusline and its own keys.
func (m *Model) navItems() []navItem {
	count := fmt.Sprint(len(m.visible))
	list := !m.activityOpen && m.viewProject == nil
	items := []navItem{
		{label: "Now", now: true, active: list && m.nowOpen},
		{label: "My Tasks", isView: true, active: list && !m.nowOpen},
	}
	for i := range items {
		if items[i].active {
			items[i].right = count
		}
	}
	activityItem := navItem{label: "Activity", activity: true, active: m.activityOpen}
	if n := m.activity.newActivity(); n > 0 && !m.activityOpen {
		activityItem.right = fmt.Sprint(n)
	}
	items = append(items, activityItem)
	named := m.recentSavedViewNames()
	if len(named) > 0 {
		items = append(items, navItem{label: "Saved views", right: "V", heading: true, dim: m.activityOpen})
		for _, name := range named[:min(len(named), maxNavSavedViews)] {
			it := navItem{label: ticket.OneLine(name), savedView: name, active: name == m.savedView && !m.activityOpen, dim: m.activityOpen}
			if it.active && m.savedViewModified() {
				it.label += " *"
			}
			items = append(items, it)
		}
		items = appendMore(items, max(len(named)-maxNavSavedViews, 0))
	}
	items = append(items, navItem{label: "Projects", right: "p", heading: true, dim: m.activityOpen})
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
		it := navItem{label: ticket.OneLine(names[gid]), project: &asana.Ref{GID: gid, Name: names[gid]}, isView: true, dim: m.activityOpen}
		if gidOf(m.viewProject) == gid && !m.activityOpen {
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

// navView renders the Places panel's rows, width by height cells.
func (m *Model) navView(width, height int) string {
	lines, _ := m.navRows(width, height)
	return strings.Join(lines, "\n")
}

// navRows draws the Places panel's lines that fit in height rows, and for
// each line its selectable row's index, the navCursor that selects it, or -1.
func (m *Model) navRows(width, height int) (lines []string, rows []int) {
	items := m.navItems()
	selectedLine, selectable := -1, 0
	for i, it := range items {
		if it.heading {
			if i > 0 {
				lines, rows = append(lines, ""), append(rows, -1)
			}
			rule := m.navRule(it.label, it.right, width)
			if it.dim {
				rule = dimStyle.Render(ansi.Strip(rule))
			}
			lines, rows = append(lines, rule), append(rows, -1)
			continue
		}
		row := -1
		if it.selectable() {
			row, selectable = selectable, selectable+1
		}
		mark := "  "
		if it.active {
			mark = m.headingStyle(m.accentStyle, m.focusNav).Render("▌") + " "
		}
		label := it.label
		if it.project != nil {
			label = m.projectLabel(*it.project, lipgloss.NewStyle())
		}
		switch {
		case it.dim:
			label = dimStyle.Render(label)
		case it.active:
			label = titleStyle.Render(label)
		}
		right := dimStyle.Render(it.right)
		room := width - ansi.StringWidth(mark) - ansi.StringWidth(it.right) - 1
		label = ansi.Truncate(label, max(room, 1), "…")
		line := mark + label + strings.Repeat(" ", max(width-ansi.StringWidth(mark)-ansi.StringWidth(label)-ansi.StringWidth(it.right), 1)) + right
		if m.focusNav && i == m.navIndex(items) {
			line = selectedStyle.Render(ansi.Strip(line))
			selectedLine = len(lines)
		}
		lines, rows = append(lines, line), append(rows, row)
	}
	if len(lines) > height {
		start := max(selectedLine-height+1, 0)
		lines, rows = lines[start:start+height], rows[start:start+height]
	}
	return lines, rows
}

// navRule is a heading rule with its section's key, if any, set in the
// rule's right end: ── Projects ──── [p] ─
func (m *Model) navRule(title, key string, width int) string {
	if key == "" {
		return m.rule(title, width, false, m.focusNav)
	}
	line := m.sym.border.Top
	tail := " [" + key + "] " + line
	return m.rule(title, width-ansi.StringWidth(tail), false, m.focusNav) +
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

// focusNavPanel gives the Places panel focus, starting on the viewed view.
func (m *Model) focusNavPanel() {
	m.focusNav, m.navCursor = true, 0
	n := 0
	for _, it := range m.navItems() {
		if !it.selectable() {
			continue
		}
		if (it.isView || it.activity || it.now) && it.active {
			m.navCursor = n
			return
		}
		n++
	}
}

// updateNav runs a [keys.views] binding while the Places panel has focus.
func (m *Model) updateNav(name string) tea.Cmd {
	items := m.navItems()
	var selectable []navItem
	for _, it := range items {
		if it.selectable() {
			selectable = append(selectable, it)
		}
	}
	// The rows can shrink under the cursor, such as when projects reload.
	m.navCursor = min(m.navCursor, len(selectable)-1)
	switch name {
	case "down":
		m.navCursor = min(m.navCursor+1, len(selectable)-1)
	case "up":
		m.navCursor = max(m.navCursor-1, 0)
	case "top":
		m.navCursor = 0
	case "bottom":
		m.navCursor = len(selectable) - 1
	case "open":
		it := selectable[m.navCursor]
		m.focusNav = false
		if it.activity {
			if m.activityOpen {
				return nil
			}
			return m.openActivity()
		}
		if it.now {
			if it.active {
				return nil
			}
			return m.openNow()
		}
		if m.activityOpen {
			m.closeActivity()
			m.layout()
		}
		if it.savedView != "" {
			return m.applySavedView(it.savedView)
		}
		if it.active {
			return nil
		}
		return m.pickedProject(it.project)
	}
	return nil
}
