package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// listState is the ticket list: its tasks, the visible rows and their
// layout, the cursor, and the filter being edited.
type listState struct {
	tasks       []asana.Task
	pinExtras   []asana.Task // pinned tickets absent from the normal list
	visible     []asana.Task
	groups      []string    // group label per visible task; nil when ungrouped
	pinnedCount int         // leading visible rows in the Pinned section
	viewingRow  bool        // visible[pinnedCount] is the temporary ticket, under Viewing
	newCount    int         // rows after Pinned and Viewing in the New section
	newHeld     string      // the New ticket being read, kept there until the cursor leaves
	sortBy      config.Sort // ordering within groups; empty By keeps Asana order
	groupBy     string      // list field the list is grouped by; "" for none
	listW       int         // fitted list pane width; 0 for the default split
	cursor      int
	cols        []int           // width of each list field column; 0 when empty or folded
	folded      []bool          // list columns whose one value per section shows in its header
	badgeW      int             // width of the widest agent badge
	titleW      int             // width of the widest ticket title
	changed     map[string]bool // My Tasks tickets modified since last shown
	filterInput textinput.Model
	// filterBefore is the filter when editing started; the filter cancel key restores it.
	filterBefore   string
	filtering      bool
	separator      bool        // lines frame each ticket; starts at list.separator
	spacing        bool        // blank lines around group headers; starts at list.header.spacing
	selectionStyle string      // selected row style; starts at list.selection.style
	viewing        *asana.Task // edited ticket kept listed after a reload drops it, until the selection moves
}

// listTitle is the list panel's title and its right-hand count, or a
// loading note while tasks load.
func (m *Model) listTitle() (title, count string) {
	count = fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks))
	if m.loading {
		count = "loading"
		if m.sym.spinner != nil {
			count = m.sym.spinner[m.frame%len(m.sym.spinner)] + " " + count
		}
	}
	if note := m.foldedNote(0, ""); m.groups == nil && note != "" && !m.loading {
		count = note + " · " + count
	}
	title = "[1] Tickets · " + m.viewName()
	if len(m.deps.State.SavedViews) > 0 {
		title += " · " + m.savedViewLabel()
	}
	return title, count
}

// skeletonWidths are the placeholder rows' title widths, in percent.
var skeletonWidths = []int{70, 55, 82, 64, 48, 76, 60, 88, 52, 68, 58, 74}

// skeleton fills the list with placeholder rows while the first tasks load;
// a lighter band runs down them with the spinner.
func (m *Model) skeleton(width, height int) string {
	bar, lit := lipgloss.NewStyle().Foreground(borderColor), dimStyle
	colW := min(12, width/4)
	lines := make([]string, min(height, len(skeletonWidths)))
	for i := range lines {
		style := bar
		if m.sym.spinner != nil && i == m.frame%len(lines) {
			style = lit
		}
		titleW := max((width-colW-6)*skeletonWidths[i]/100, 1)
		row := "▆ " + strings.Repeat("▆", titleW)
		lines[i] = style.Render(row + strings.Repeat(" ", max(width-ansi.StringWidth(row)-colW, 1)) + strings.Repeat("▆", max(colW-2, 0)))
	}
	return strings.Join(lines, "\n")
}

// selectionBar reports whether the selected ticket is drawn as a reversed bar
// (the list has focus and selection.style is bar).
func (m *Model) selectionBar() bool {
	return m.selectionStyle == config.StyleBar && m.listFocused()
}

// renderRowLine fits line into width with flag after it and tail
// right-aligned. A selected line is bold, or with selectionBar highlighted
// across the row up to the tail. Flag and tail style themselves.
func (m *Model) renderRowLine(line, flag, tail string, width int, selected bool) string {
	tailW := ansi.StringWidth(tail)
	if tail != "" && width-tailW-1 < 8 {
		tail, tailW = "", 0
	}
	room := width
	if tail != "" {
		room = width - tailW - 1
	}
	if flag != "" {
		room = max(room-ansi.StringWidth(flag)-1, 1)
	}
	line = ansi.Truncate(line, room, "…")
	switch {
	case selected && m.selectionBar() && flag != "":
		fill := strings.Repeat(" ", max(width-tailW-ansi.StringWidth(line)-1-ansi.StringWidth(flag), 0))
		return selectedStyle.Render(ansi.Strip(line)+" ") + flag + selectedStyle.Render(fill) + tail
	case selected && m.selectionBar():
		return selectedStyle.Render(ansi.Strip(line)+strings.Repeat(" ", max(width-tailW-ansi.StringWidth(line), 0))) + tail
	case selected:
		line = titleStyle.Render(ansi.Strip(line))
	}
	if flag != "" {
		line += " " + flag
	}
	if tail == "" {
		return line
	}
	return line + strings.Repeat(" ", max(width-ansi.StringWidth(line)-tailW, 1)) + tail
}

func (m *Model) listView(width, height int) string {
	switch {
	case m.loading && m.tasks == nil:
		return m.skeleton(width, height)
	case len(m.visible) == 0:
		return m.emptyView(width, height)
	}
	lines, _ := m.listRows(width, height)
	return strings.Join(lines, "\n")
}

// listRows draws the tickets that fit in height rows, and for each line the
// index in visible of the ticket it shows, or -1 for a header, separator, or
// blank line. It needs a visible ticket.
func (m *Model) listRows(width, height int) (lines []string, rows []int) {
	add := func(line string, row int) {
		lines, rows = append(lines, line), append(rows, row)
	}
	itemH, sepH := 1, 0
	if m.deps.Config.List.Layout == config.LayoutMulti {
		itemH = 2
	}
	if m.separator {
		sepH = 1
	}
	// Separators frame every item and neighbours share one; a group header
	// takes the place of the separator above its item. The first row shown
	// always has its group header. With spacing, headers get a blank line
	// below, and above too unless they top the view.
	spacing := 0
	if m.spacing {
		spacing = 1
	}
	header := func(i, start int) bool {
		if m.groups == nil {
			return false
		}
		if i == start || m.groups[i] != m.groups[i-1] {
			return true
		}
		label, _ := m.retainedSection(i)
		prev, _ := m.retainedSection(i - 1)
		return label != prev
	}
	above := func(i, start int) int {
		if i == start {
			return 0
		}
		return spacing
	}
	rowH := func(i, start int) int {
		if header(i, start) {
			return above(i, start) + itemH + 1 + spacing
		}
		return itemH + sepH
	}
	// Scroll so the cursor's row is the last that fits, then fill below it.
	start, used := m.cursor, sepH+rowH(m.cursor, m.cursor)
	for start > 0 {
		next := used - rowH(start, start) + rowH(start, start-1) + rowH(start-1, start-1)
		if next > height {
			break
		}
		start, used = start-1, next
	}
	end := m.cursor + 1
	for end < len(m.visible) && used+rowH(end, start) <= height {
		used += rowH(end, start)
		end++
	}
	counts := m.groupCounts()
	sep := dimStyle.Render(strings.Repeat("─", width))
	// The marker style keeps a gutter on every row so text doesn't shift.
	var gutter string
	cursor := m.marker()
	if cursor != "" {
		gutter = strings.Repeat(" ", ansi.StringWidth(cursor))
		cursorStyle := m.markerStyle
		if !m.listFocused() {
			cursorStyle = dimStyle
		}
		cursor = cursorStyle.Render(cursor)
	}
	rowW := max(width-ansi.StringWidth(gutter)-listGutter, 1)
	cols := m.rowCols(rowW)
	for i := start; i < end; i++ {
		switch {
		case header(i, start):
			for range above(i, start) {
				add("", -1)
			}
			if label, n := m.retainedSection(i); label != "" {
				style := m.viewingStyle
				switch {
				case i < m.pinnedCount:
					style = m.pinnedStyle
				case label == newLabel:
					style = newHeaderStyle()
				}
				add(m.groupHeader(m.headingStyle(style, m.listFocused()), label, n, width, m.foldedNote(i, label)), -1)
			} else {
				add(m.groupHeader(m.headingStyle(m.groupStyle(m.groups[i]), m.listFocused()), m.groups[i], counts[m.groups[i]], width, m.foldedNote(i, m.groups[i])), -1)
			}
			for range spacing {
				add("", -1)
			}
		case sepH > 0:
			add(sep, -1)
		}
		row, rowTail := m.listRow(i, cols)
		for j, line := range row {
			flag, tail := "", ""
			if j == 0 {
				flag, tail = m.newBadge(i), rowTail
			}
			lead := gutter
			if i == m.cursor {
				lead = cursor
			}
			add(lead+m.renderRowLine(line, flag, tail, rowW, i == m.cursor), i)
		}
	}
	if sepH > 0 {
		add(sep, -1)
	}
	return lines, rows
}

// emptyView centers a note on why the list is empty, with the keys that
// change what it shows.
func (m *Model) emptyView(width, height int) string {
	title, why := "Nothing here.", "No tickets in "+m.viewName()
	if len(m.tasks) > 0 {
		title, why = "All clear.", "Nothing matches "+warnStyle.Render(m.filterInput.Value())
	}
	var hints []string
	for _, h := range [][2]string{{"filter", "filter"}, {"projects", "projects"}, {"reload", "reload"}} {
		if k := keyLabel("main", h[0]); k != "" {
			hints = append(hints, m.accentStyle.Render(k)+" "+dimStyle.Render(h[1]))
		}
	}
	block := lipgloss.JoinVertical(lipgloss.Center,
		okStyle.Bold(true).Render(m.sym.done), "", titleStyle.Render(title), dimStyle.Render(why), "",
		strings.Join(hints, "   "))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// groupCounts returns the number of visible tickets per group label, leaving
// out retained rows so a group sharing their label keeps its own count.
func (m *Model) groupCounts() map[string]int {
	counts := map[string]int{}
	for i, g := range m.groups {
		if label, _ := m.retainedSection(i); label == "" {
			counts[g]++
		}
	}
	return counts
}

// marker is the selected row's left marker with its gap, or "" unless
// selection.style is marker.
func (m *Model) marker() string {
	if m.selectionStyle != config.StyleMarker {
		return ""
	}
	return m.sym.cursor + " "
}

// listRow returns the lines of visible ticket i's row and the tail
// right-aligned on its first line: the single layout's field columns sized by
// cols and agent badge, or the multi layout's badge.
func (m *Model) listRow(i int, cols []int) (row []string, tail string) {
	t := m.visible[i]
	mark := m.sym.open
	if t.Completed {
		mark = m.sym.done
	}
	name := ticket.OneLine(t.Name)
	linked := m.viewAgents(t)
	bar := i == m.cursor && m.selectionBar()
	badge := m.sym.badge(linked, m.frame, bar)
	nameStyle := lipgloss.NewStyle()
	switch {
	case len(linked) > 0 && linked[0].State == agents.Waiting && i != m.cursor:
		nameStyle = stateStyles[agents.Waiting]
	case t.Completed:
		nameStyle = dimStyle.Strikethrough(true)
	case m.distant(i):
		nameStyle = dimStyle
	}
	name = tagTitle(name, nameStyle)
	title := dimStyle.Render(mark) + "  " + name
	rc, today := m.rowContext(), m.now()
	if m.deps.Config.List.Layout == config.LayoutMulti {
		var parts []string
		for j, column := range m.listColumns() {
			v, style := columnValue(t, column, rc, today)
			if v == "" || j < len(m.folded) && m.folded[j] {
				continue
			}
			parts = append(parts, style.Render(v))
		}
		details := ""
		if len(parts) > 0 {
			details = "   " + strings.Join(parts, dimStyle.Render(" · "))
		}
		return []string{title, details}, badge
	}
	// A highlighted bar runs through the columns, which then lose their colors.
	pad := func(s string, w int) string {
		s += strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
		if bar && s != "" {
			return selectedStyle.Render(ansi.Strip(s))
		}
		return s
	}
	var cells []string
	for j, column := range m.listColumns() {
		if cols[j] == 0 {
			continue
		}
		v, style := columnValue(t, column, rc, today)
		v = ansi.Truncate(v, cols[j], "…")
		cells = append(cells, pad(style.Render(v), cols[j]))
	}
	if m.badgeW > 0 {
		cells = append(cells, badge+pad("", m.badgeW-ansi.StringWidth(badge)))
	}
	return []string{title}, strings.Join(cells, pad("", 2))
}

// rowCols returns the field column widths for rows width wide, hiding columns
// until titles get minTitleW, or the widest title if shorter: other fields
// from the right, then due. The agent badge always stays.
func (m *Model) rowCols(width int) []int {
	cols := slices.Clone(m.cols)
	columns := m.listColumns()
	// The title line also holds the check mark and the two spaces after it.
	room := min(m.titleW, minTitleW) + ansi.StringWidth(m.sym.open) + 2
	var order, due []int
	for j := len(cols) - 1; j >= 0; j-- {
		if columns[j].GID == "" && isDue(strings.TrimSpace(columns[j].Field)) {
			due = append(due, j)
		} else {
			order = append(order, j)
		}
	}
	for _, j := range append(order, due...) {
		if width-tailWidth(cols, m.badgeW)-1 >= room {
			break
		}
		cols[j] = 0
	}
	return cols
}

// tailWidth is the width of a row tail with these field columns and badge.
func tailWidth(cols []int, badgeW int) int {
	w, n := badgeW, min(badgeW, 1)
	for _, c := range cols {
		if c > 0 {
			w, n = w+c, n+1
		}
	}
	return w + 2*max(n-1, 0)
}

// measureColumns sizes each list field column to its widest visible value,
// up to maxColW, the badge column to the widest agent badge, and titleW to
// the widest title.
func (m *Model) measureColumns() {
	columns := m.listColumns()
	m.cols, m.badgeW, m.titleW = make([]int, len(columns)), 0, 0
	rc, today := m.rowContext(), m.now()
	for _, t := range m.visible {
		m.titleW = max(m.titleW, ansi.StringWidth(ticket.OneLine(t.Name)))
		for j, column := range columns {
			v, _ := columnValue(t, column, rc, today)
			m.cols[j] = min(max(m.cols[j], ansi.StringWidth(v)), maxColW)
		}
		m.badgeW = max(m.badgeW, ansi.StringWidth(m.sym.badge(m.viewAgents(t), 0, false)))
	}
	m.folded = m.foldedColumns(columns)
	for j, folded := range m.folded {
		if folded {
			m.cols[j] = 0
		}
	}
}

// sectionKey names the section visible ticket i sits in: Pinned, Viewing,
// New, its group, or "" for the whole ungrouped list. Retained sections get a
// prefix so a group named like one stays apart.
func (m *Model) sectionKey(i int) string {
	if label, _ := m.retainedSection(i); label != "" {
		return "\x00" + label
	}
	if m.groups == nil {
		return ""
	}
	return m.groups[i]
}

// foldedColumns reports the list columns that hold one value in each
// section. Their values show once in the section's header, so the rows leave
// them out. Nothing folds while every section holds a single ticket.
func (m *Model) foldedColumns(columns []state.Column) []bool {
	folded := make([]bool, len(columns))
	sections := map[string]bool{}
	for i := range m.visible {
		sections[m.sectionKey(i)] = true
	}
	if len(sections) >= len(m.visible) {
		return folded
	}
	rc, today := m.rowContext(), m.now()
	for j, column := range columns {
		values := map[string]string{}
		folded[j] = true
		for i, t := range m.visible {
			v, _ := columnValue(t, column, rc, today)
			if prev, ok := values[m.sectionKey(i)]; ok && prev != v {
				folded[j] = false
				break
			}
			values[m.sectionKey(i)] = v
		}
	}
	return folded
}

// foldedNote lists the folded column values of the section that visible
// ticket i opens, leaving out a value its header label already shows.
func (m *Model) foldedNote(i int, label string) string {
	if i >= len(m.visible) {
		return ""
	}
	var parts []string
	rc, today := m.rowContext(), m.now()
	for j, column := range m.listColumns() {
		if j >= len(m.folded) || !m.folded[j] {
			continue
		}
		if v, _ := columnValue(m.visible[i], column, rc, today); v != "" && !strings.EqualFold(v, label) {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// fitList sizes the list pane to its widest row or group header. paneWidths
// caps it to leave the reader readerMinW columns. A fixed layout.list_width
// leaves it alone.
func (m *Model) fitList() {
	if m.fixedListW() > 0 {
		return
	}
	gutter := ansi.StringWidth(m.marker())
	w := 0
	counts := m.groupCounts()
	for i := range m.visible {
		if m.groups != nil && (i == 0 || m.sectionKey(i) != m.sectionKey(i-1)) {
			label, n := m.retainedSection(i)
			if label == "" {
				label, n = m.groups[i], counts[m.groups[i]]
			}
			w = max(w, groupHeaderWidth(label, n, m.foldedNote(i, label)))
		}
	}
	for i := range m.visible {
		row, tail := m.listRow(i, m.cols)
		for j, line := range row {
			lineW := gutter + ansi.StringWidth(line)
			if flag := m.newBadge(i); j == 0 && flag != "" {
				lineW += 1 + ansi.StringWidth(flag)
			}
			if j == 0 && tail != "" {
				lineW += 2 + ansi.StringWidth(tail)
			}
			w = max(w, lineW)
		}
	}
	// The title needs its corners, the spaces around it and the count, and
	// one rule cell between them.
	title, count := m.listTitle()
	m.listW = max(w+listGutter+panelFrame, ansi.StringWidth(title)+ansi.StringWidth(count)+8)
	m.layout()
}
