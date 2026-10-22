package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// sideBySideW is the narrowest reader that puts the agents and subtasks
// beside the details card instead of below it.
const sideBySideW = 90

// renderCards renders t as the reading pane's cards view: the name, the
// local branch, repo, and time lines, a details card with the agents and
// subtasks beside it (or below, when narrow), titled sections, and the comments
// as a timeline, all
// width columns wide. It records the line of each editable row in
// m.fieldLines, and the last line of each multi-line one in m.fieldEnds.
func (m *Model) renderCards(t ticket.Ticket, width int) string {
	head := lipgloss.NewStyle().Width(width).Render(tagTitle(ticket.Clean(t.Name), titleStyle))
	if line := m.branchLine(t.Task, width); line != "" {
		head += "\n" + line
	}
	if line := m.repoLine(t.Task, width); line != "" {
		head += "\n" + line
	}
	if value := m.timeSummaryStyled(t.GID); value != "" {
		line := dimStyle.Render(m.sym.icon(iconClock)) + value
		head += "\n" + ansi.Truncate(line, width, "…")
	}
	m.fieldLines, m.fieldEnds = map[string]int{}, map[string]int{}
	fields := m.fieldTargets(t)
	m.cardTargets = append(m.cardTargets[:0], fields[:len(fields)-1]...)
	var agentLines []string
	for _, a := range m.viewAgents(t.Task) {
		agentLines = append(agentLines, m.agentLabel(a, stateStyles[a.State].Bold(true))+dimStyle.Render(" — "+ticket.OneLine(filepath.Base(a.Path))))
	}
	notes, hidden := m.agentNotes(t.Task)
	for _, n := range notes {
		agentLines = append(agentLines, dimStyle.Render(ticket.OneLine(n)))
	}
	if hidden > 0 {
		style := dimStyle.Italic(true)
		if m.fieldKey == otherAgentsKey {
			style = m.fieldStyle()
		}
		agentLines = append(agentLines, style.Render("+ Show "+otherAgents(hidden)))
	}
	// markOthers records the Show agents row, the Agents body's last line, for
	// a body that starts on line. Each agent row takes one line.
	markOthers := func(line int) {
		if hidden == 0 {
			return
		}
		m.fieldLines[otherAgentsKey] = line + len(agentLines) - 1
		m.cardTargets = append(m.cardTargets, otherAgentsKey)
	}
	subtaskTitle, subtasks := m.subtasks(t)

	// Rows start below the head, a blank line, and the card's top edge.
	top := lipgloss.Height(head) + 2
	var blocks []string
	add := func(key, title, body string) {
		if body != "" {
			block := m.rule(title, width, m.fieldKey == key, m.focusReader) + "\n" + body
			m.fieldLines[key] = lipgloss.Height(strings.Join(blocks, "\n\n")) + 1
			m.fieldEnds[key] = m.fieldLines[key] + lipgloss.Height(block) - 1
			m.cardTargets = append(m.cardTargets, key)
			blocks = append(blocks, block)
		}
	}
	if width >= sideBySideW && (len(agentLines) > 0 || subtasks != "") {
		leftW := (width - 1) / 2
		rightW := width - leftW - 1
		var side []string
		sideLine := lipgloss.Height(head) + 1
		if len(agentLines) > 0 {
			key := agentsKey
			m.fieldLines[key] = sideLine
			m.cardTargets = append(m.cardTargets, key)
			card := m.card(m.sym.robot+"   Agents", cutLines(agentLines, rightW-4), rightW, m.fieldKey == key)
			side = append(side, card)
			m.fieldEnds[key] = sideLine + lipgloss.Height(card) - 1
			markOthers(sideLine + 1)
			sideLine += lipgloss.Height(card) + 1
		}
		if subtasks != "" {
			key := "section:subtasks"
			m.fieldLines[key] = sideLine
			m.cardTargets = append(m.cardTargets, key)
			card := m.card(subtaskTitle, wrap(subtasks, rightW-4), rightW, m.fieldKey == key)
			m.fieldEnds[key] = sideLine + lipgloss.Height(card) - 1
			side = append(side, card)
		}
		details := m.card("Details", m.detailsBody(t, leftW-4, top), leftW, false)
		blocks = []string{head, lipgloss.JoinHorizontal(lipgloss.Top, details, " ", strings.Join(side, "\n"))}
		add("section:description", "Description", m.renderRich(t.HTMLNotes, width))
	} else {
		blocks = []string{head, m.card("Details", m.detailsBody(t, width-4, top), width, false)}
		add(agentsKey, "Agents", cutLines(agentLines, width))
		markOthers(m.fieldLines[agentsKey] + 1)
		add("section:description", "Description", m.renderRich(t.HTMLNotes, width))
		add("section:subtasks", subtaskTitle, wrap(subtasks, width))
	}
	add("section:blocked-by", "Blocked by", wrap(refLines(t.Dependencies), width))
	add("section:blocking", "Blocking", wrap(refLines(t.Dependents), width))

	var attachments []string
	for i, a := range t.Attachments {
		attachments = append(attachments, ansi.Truncate(fmt.Sprintf("%d. %s%s", i+1, m.sym.icon(iconClip), ticket.OneLine(a.Name)), width, "…"))
	}
	add("section:attachments", fmt.Sprintf("Attachments %d", len(t.Attachments)), strings.Join(attachments, "\n"))

	// The add row stays visible above the comment timeline.
	m.fieldLines[commentKey] = lipgloss.Height(strings.Join(blocks, "\n\n")) + 2
	m.cardTargets = append(m.cardTargets, commentKey)
	m.clipped = map[string]bool{}
	comments := []string{dimStyle.Render("none")}
	if len(t.Comments) > 0 {
		comments = nil
		line := m.fieldLines[commentKey] + 2
		first := 0
		opts := m.deps.Config.Reader.Comments
		if latest := opts.Latest; !m.showComments && latest > 0 && len(t.Comments) > latest {
			first = len(t.Comments) - latest
			m.fieldLines[allCommentsKey] = line
			m.cardTargets = append(m.cardTargets, allCommentsKey)
			style := dimStyle.Italic(true)
			if m.fieldKey == allCommentsKey {
				style = m.fieldStyle()
			}
			comments = append(comments, style.Render(fmt.Sprintf("+ Show all %d comments", len(t.Comments))))
			line += 2
		}
		for i := first; i < len(t.Comments); i++ {
			c := t.Comments[i]
			key := commentTarget(c, i)
			m.fieldLines[key] = line
			m.cardTargets = append(m.cardTargets, key)
			preview := 0
			if i < len(t.Comments)-1 && !m.openComments[key] {
				preview = opts.PreviewLines
			}
			block, clipped := m.comment(c, width, m.fieldKey == key, preview)
			m.clipped[key] = clipped
			m.fieldEnds[key] = line + lipgloss.Height(block) - 1
			line += lipgloss.Height(block) + 1
			comments = append(comments, block)
		}
	}
	heading := m.rule(fmt.Sprintf("Comments %d", len(t.Comments)), width, false, m.focusReader)
	addComment := m.addRow(commentKey, "+ Add comment")
	blocks = append(blocks, heading+"\n"+addComment+"\n\n"+strings.Join(comments, "\n\n"))
	return strings.Join(blocks, "\n\n")
}

// branchLine is the faint line under the ticket's name naming the branch its
// actions use, cut to width; "" when that is only the name's slug.
func (m *Model) branchLine(t asana.Task, width int) string {
	branch := action.Branch(t, m.deps.State.TaskBranches[t.GID], m.deps.Config.BranchField, m.rowContext().preferred)
	if branch == action.Slug(t.Name) {
		return ""
	}
	return ansi.Truncate(branchStyle.Render(m.sym.icon(iconBranch)+ticket.OneLine(branch)), width, "…")
}

// repoLine is the faint line under the summary naming the local repo a
// repo = true action on t runs in, its path cut to fit width; "" when the
// action would ask.
func (m *Model) repoLine(t asana.Task, width int) string {
	path, own := m.effectiveRepo(t)
	if path == "" {
		return ""
	}
	note := " · local"
	if own {
		note = " · local, this ticket only"
	}
	icon := m.sym.icon(iconRepo)
	path = truncatePath(repo.CollapseHome(path), width-ansi.StringWidth(icon+note))
	return dimStyle.Render(icon + path + note)
}

// truncatePath cuts path to width by dropping the middle of its directory,
// keeping its last element: "/private/va…/repo".
func truncatePath(path string, width int) string {
	if ansi.StringWidth(path) <= width {
		return path
	}
	dir, base := filepath.Split(path)
	tail := "…" + string(filepath.Separator) + base
	if avail := width - ansi.StringWidth(tail); avail > 0 {
		return ansi.Truncate(dir, avail, "") + tail
	}
	return ansi.Truncate(tail, width, "…")
}

// subtasks returns the subtasks section's title, with a progress bar, and its
// lines; both are "" when t has none.
func (m *Model) subtasks(t ticket.Ticket) (title, body string) {
	if len(t.Subtasks) == 0 {
		return "", ""
	}
	lines := make([]string, len(t.Subtasks))
	done := 0
	for i, s := range t.Subtasks {
		lines[i] = dimStyle.Render(m.sym.open) + " " + ticket.OneLine(s.Name)
		if s.Completed {
			done++
			lines[i] = okStyle.Render(m.sym.done) + " " + dimStyle.Strikethrough(true).Render(ticket.OneLine(s.Name))
		}
	}
	return fmt.Sprintf("Subtasks %s %d/%d", m.progress(done, len(t.Subtasks)), done, len(t.Subtasks)), strings.Join(lines, "\n")
}

// progressCells is the width of a progress bar.
const progressCells = 5

// progress draws done of total as a bar of progressCells cells.
func (m *Model) progress(done, total int) string {
	on := done * progressCells / max(total, 1)
	return okStyle.Render(strings.Repeat(m.sym.barOn, on)) + dimStyle.Render(strings.Repeat(m.sym.barOff, progressCells-on))
}

// comment renders one comment of the timeline: a colored dot, the author, and
// the day, over the body behind a gutter in the author's color. A preview
// above 0 keeps only that many lines of the body's text, ending in see more,
// and reports whether it cut any.
func (m *Model) comment(c asana.Story, width int, selected bool, preview int) (string, bool) {
	author := ticket.Author(c)
	day, err := time.Parse(time.DateOnly, ticket.Day(c.CreatedAt))
	when := ticket.Day(c.CreatedAt)
	if err == nil {
		when = shortDate(day, m.now())
	}
	head := authorStyle(author).Render("●") + " " + titleStyle.Render(author) + dimStyle.Render(" · "+when)
	if selected {
		head = m.fieldStyle().Render(ansi.Strip(head))
	}
	body := m.renderRich(c.HTMLText, width-2)
	gutterStyle := authorStyle(author)
	if selected {
		gutterStyle = m.markerStyle
	}
	gutter := gutterStyle.Render("│") + " "
	lines := strings.Split(body, "\n")
	clipped := false
	if preview > 0 {
		lines, clipped = previewText(lines, preview, width-2)
	}
	if r := m.reactions(c.Reactions); r != "" {
		lines = append(lines, ansi.Truncate(r, width-2, "…"))
	}
	for i, l := range lines {
		lines[i] = gutter + l
	}
	return head + "\n" + strings.Join(lines, "\n"), clipped
}

// previewText keeps the first n lines of text in lines, skipping blank ones,
// and ends the last kept line with see more, within width. It returns lines
// unchanged when they hold no more text than that.
func previewText(lines []string, n, width int) ([]string, bool) {
	var kept []string
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) == "" {
			continue
		}
		if len(kept) == n {
			more := dimStyle.Italic(true).Render(" see more …")
			last := kept[len(kept)-1]
			// Rendered lines are padded to width; cut the padding, not the text.
			text, room := ansi.StringWidth(strings.TrimRight(ansi.Strip(last), " ")), width-ansi.StringWidth(more)
			if text > room {
				last = ansi.Truncate(last, room, "…")
			} else {
				last = ansi.Truncate(last, text, "")
			}
			kept[len(kept)-1] = last + more
			return kept, true
		}
		kept = append(kept, lines[i])
	}
	return lines, false
}

// reactions renders a comment's reactions as each emoji with its count, the
// user's own as pills, or as a total count when the symbol set has no emoji;
// "" when it has none.
func (m *Model) reactions(rs []asana.Reaction) string {
	var parts []string
	total := 0
	for _, r := range rs {
		if r.Count <= 0 {
			continue
		}
		total += r.Count
		style := dimStyle
		if r.Reacted {
			style = reactedStyle
		}
		// One space each side pads the pill; plain reactions get the same
		// space so they line up with it.
		parts = append(parts, style.Render(fmt.Sprintf(" %s %d ", ticket.OneLine(r.Emoji), r.Count)))
	}
	switch {
	case total == 0:
		return ""
	case total == 1 && !m.sym.emoji:
		return dimStyle.Render("1 reaction")
	case !m.sym.emoji:
		return dimStyle.Render(fmt.Sprintf("%d reactions", total))
	}
	return strings.Join(parts, " ")
}

// authorStyle colors an author the same way every time, from colors.authors.
func authorStyle(name string) lipgloss.Style { return hashStyle(authorStyles, name) }

// shortDate shows a date as "Sep 26", with the year when it is not this year's.
func shortDate(d, today time.Time) string {
	if d.Year() == today.Year() {
		return d.Format("Jan 2")
	}
	return d.Format("Jan 2 2006")
}

// detailMeta inserts the Add project row after the ticket's projects. It
// leaves out the URL, which o and y cover, and the Status of an open ticket.
func detailMeta(t ticket.Ticket) []ticket.Field {
	rows := slices.DeleteFunc(t.Meta(), func(f ticket.Field) bool {
		return f.Key == "" && (f.Label == "URL" || f.Label == "Status" && !t.Completed)
	})
	index := -1
	for i, f := range rows {
		if strings.HasPrefix(f.Key, ticket.KeyProject+":") {
			index = i + 1
		}
	}
	if index < 0 {
		index = slices.IndexFunc(rows, func(f ticket.Field) bool { return f.Key == ticket.KeyMyTasks })
		if index < 0 {
			index = len(rows)
		}
	}
	return slices.Insert(rows, index, ticket.Field{Key: addProjectKey, Label: "+ Add project"})
}

// detailsBody lists the built-in fields, then any custom fields below a rule,
// ending with the empty ones or a row that shows them.
// top is the reader line of its first row, for m.fieldLines.
func (m *Model) detailsBody(t ticket.Ticket, width, top int) string {
	meta, custom, empty := detailMeta(t), t.FieldValues(), t.EmptyFields()
	if m.showEmpty {
		custom, empty = append(custom, empty...), nil
	}
	labelW := 0
	for _, f := range append(meta, custom...) {
		if f.Key != addProjectKey {
			labelW = max(labelW, ansi.StringWidth(f.Label))
		}
	}
	labelW = min(labelW, width/3)
	line := top
	rows := func(fields []ticket.Field) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			if f.Key == addProjectKey {
				m.fieldLines[f.Key] = line
				label := lipgloss.NewStyle().Width(labelW + 2).Render("")
				out[i] = lipgloss.JoinHorizontal(lipgloss.Top, label, lipgloss.NewStyle().Width(max(width-labelW-2, 1)).Render(m.addRow(f.Key, f.Label)))
				line += lipgloss.Height(out[i])
				continue
			}
			value := f.Value
			if value == "" {
				value = dimStyle.Render("—")
			}
			switch {
			case f.Label == "Status" && f.Key == "":
				value = m.statusBadge(t.Completed)
			case strings.HasPrefix(f.Key, ticket.KeyProject+":"):
				gid := strings.TrimPrefix(f.Key, ticket.KeyProject+":")
				for _, membership := range t.Memberships {
					if membership.Project.GID == gid {
						var section string
						if membership.Section != nil {
							section = " / " + ticket.Clean(membership.Section.Name)
						}
						// Cut the project name so its section stays on the row,
						// unless that leaves too little of the name to read.
						value = m.projectLabel(membership.Project, lipgloss.NewStyle())
						if room := width - labelW - 2 - ansi.StringWidth(section); room >= 12 {
							value = ansi.Truncate(value, room, "…")
						}
						value += section
						break
					}
				}
			case strings.HasPrefix(f.Key, ticket.KeyField+":"):
				for _, field := range t.CustomFields {
					if f.Key == ticket.FieldKey(field.GID) && m.deps.Config.BranchField != "" && asana.SameFieldName(field.Name, m.deps.Config.BranchField) {
						value = branchStyle.Render(value)
						break
					}
				}
				if style, ok := fieldEnumStyle(t, f.Key); ok {
					value = style.Render("● " + value)
				}
			case f.Key == ticket.KeyDue:
				if due, _, ok := asana.DueDays(t.DueOn, m.now()); ok {
					rel, style := dueLabel(t.DueOn, m.now())
					value = style.Render(shortDate(due, m.now()) + " (" + rel + ")")
				}
			}
			labelStyle := dimStyle
			if f.Key != "" {
				m.fieldLines[f.Key] = line
				if f.Key == m.fieldKey {
					labelStyle = m.fieldStyle()
				}
			}
			label := lipgloss.NewStyle().Width(labelW + 2).Render(labelStyle.Render(ansi.Truncate(f.Label, labelW, "…")))
			out[i] = lipgloss.JoinHorizontal(lipgloss.Top, label, lipgloss.NewStyle().Width(max(width-labelW-2, 1)).Render(value))
			line += lipgloss.Height(out[i])
		}
		return out
	}
	lines := rows(meta)
	if len(custom) > 0 || len(empty) > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(m.sym.border.Top, width)))
		line++
		lines = append(lines, rows(custom)...)
	}
	if len(empty) > 0 {
		m.fieldLines[emptyFieldsKey] = line
		style := dimStyle.Italic(true)
		if m.fieldKey == emptyFieldsKey {
			style = m.fieldStyle()
		}
		label := fmt.Sprintf("+ Show %d empty fields", len(empty))
		if len(empty) == 1 {
			label = "+ Show 1 empty field"
		}
		lines = append(lines, style.Render(label))
	}
	return strings.Join(lines, "\n")
}

// fieldEnumStyle is the enum color style of t's custom field with key, and
// whether it has one.
func fieldEnumStyle(t ticket.Ticket, key string) (lipgloss.Style, bool) {
	for _, f := range t.CustomFields {
		if ticket.FieldKey(f.GID) == key {
			return enumStyle(f)
		}
	}
	return lipgloss.Style{}, false
}

func (m *Model) statusBadge(completed bool) string {
	if completed {
		return okStyle.Render(m.sym.done + " done")
	}
	return warnStyle.Render(m.sym.open + " open")
}

// fieldStyle marks the row the cards view has tabbed to.
func (m *Model) fieldStyle() lipgloss.Style { return m.markerStyle.Reverse(true) }

// addRow styles an inline add action, highlighting it when selected.
func (m *Model) addRow(key, label string) string {
	if m.fieldKey == key {
		return m.fieldStyle().Render(label)
	}
	return m.headingStyle(m.accentStyle, m.focusReader).Render(label)
}

// headingStyle is style for a heading in a focused pane, or the muted style in
// an unfocused one, so only the focused pane shows color.
func (m *Model) headingStyle(style lipgloss.Style, focused bool) lipgloss.Style {
	if focused {
		return style
	}
	return dimStyle
}

// rule is a section heading drawn as a full-width line with the title in it,
// marked when it is the tabbed-to row, and muted when its pane lacks focus.
func (m *Model) rule(title string, width int, selected, focused bool) string {
	line := m.sym.border.Top
	lead := line + line + " "
	title = ansi.Truncate(title, max(width-ansi.StringWidth(lead)-2, 1), "…")
	fill := max(width-ansi.StringWidth(lead)-ansi.StringWidth(title)-1, 0)
	ruleStyle := lipgloss.NewStyle().Foreground(borderColor)
	headStyle := m.headingStyle(m.accentStyle, focused)
	if selected {
		headStyle = m.fieldStyle()
	}
	return ruleStyle.Render(lead) + headStyle.Render(title) + " " + ruleStyle.Render(strings.Repeat(line, fill))
}

// card boxes body in a border with title set into the top edge. body must
// already fit width-4 columns (border and padding take the rest).
func (m *Model) card(title, body string, width int, selected bool) string {
	box := lipgloss.NewStyle().Border(m.sym.border).BorderTop(false).BorderForeground(borderColor).
		Padding(0, 1).Width(width).Render(body)
	edge := lipgloss.NewStyle().Foreground(borderColor)
	head := titleStyle
	if selected {
		head = m.fieldStyle()
	}
	return m.topEdge(head.Render(title), "", lipgloss.Width(box), edge) + "\n" + box
}

// panel boxes a pane's body, width by height cells in all, with title and
// right set into the top edge. A focused panel's edge is in the accent color
// and its title is a filled tab; alert, if not nil, colors the edge and title
// instead, such as while the pane loads. With layout.focus.dim, an
// unfocused panel's body is faint. body must already fit width-2 by height-2
// cells.
func (m *Model) panel(title, right, body string, width, height int, focused bool, alert color.Color) string {
	edge, head := lipgloss.NewStyle().Foreground(borderColor), dimStyle
	if focused {
		edge = lipgloss.NewStyle().Foreground(m.accentStyle.GetForeground())
	}
	if alert != nil {
		edge = lipgloss.NewStyle().Foreground(alert)
	}
	switch {
	case focused:
		head = lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(edge.GetForeground())
	case alert != nil:
		head = lipgloss.NewStyle().Bold(true).Foreground(alert)
	case m.focusDim:
		body = faint(body)
	}
	box := lipgloss.NewStyle().Border(m.sym.border).BorderTop(false).BorderForeground(edge.GetForeground()).
		Width(width).Height(height - 1).MaxHeight(height - 1).Render(body)
	if right != "" {
		right = dimStyle.Render(right)
	}
	top := m.topEdge(head.Render(title), right, width, edge)
	if height <= 2 {
		// No room for a body: the top edge over the bottom edge.
		b := m.sym.border
		return top + "\n" + edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, max(width-2, 0))+b.BottomRight)
	}
	return top + "\n" + box
}

// faint draws s in the terminal's faint intensity over its own colors,
// renewing it after every reset.
func faint(s string) string {
	const on, reset = "\x1b[2m", "\x1b[m"
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = strings.ReplaceAll(line, "\x1b[0m", reset)
		lines[i] = on + strings.ReplaceAll(line, reset, reset+on) + reset
	}
	return strings.Join(lines, "\n")
}

// topEdge is a box's top border, width columns wide, with the styled title
// set in after the corner and the styled right, if any, before the far corner.
// The title is cut to fit.
func (m *Model) topEdge(title, right string, width int, edge lipgloss.Style) string {
	b := m.sym.border
	lead, tail := b.TopLeft+b.Top+" ", b.TopRight
	if right != "" {
		tail = " " + right + " " + edge.Render(b.Top+b.TopRight)
	}
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(tail) - 1
	if room < 1 && right != "" {
		// Too narrow for both: keep the title.
		return m.topEdge(title, "", width, edge)
	}
	title = ansi.Truncate(title, max(room, 1), "…")
	fill := max(room-ansi.StringWidth(title), 0)
	if right == "" {
		tail = edge.Render(tail)
	}
	return edge.Render(lead) + title + " " + edge.Render(strings.Repeat(b.Top, fill)) + tail
}

// renderBody renders Markdown to fit width columns, capped at
// reader.max_text_width, without the document margins the markdown view uses.
func (m *Model) renderBody(md string, width int) string {
	if md == "" {
		return ""
	}
	return strings.Trim(m.glamour(md, m.textWidth(width), true), "\n")
}

// textWidth caps width at reader.max_text_width when one is set.
func (m *Model) textWidth(width int) int {
	if limit := m.deps.Config.Reader.MaxTextWidth; limit > 0 {
		return min(width, limit)
	}
	return width
}

func wrap(s string, width int) string {
	if s == "" {
		return ""
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// cutLines joins lines, each cut to width with an ellipsis.
func cutLines(lines []string, width int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(out, "\n")
}

func refLines(refs []asana.Ref) string {
	lines := make([]string, len(refs))
	for i, r := range refs {
		lines[i] = "- " + ticket.OneLine(r.Name)
	}
	return strings.Join(lines, "\n")
}

// commentTarget is the card target key of comment c at index i.
func commentTarget(c asana.Story, i int) string {
	if c.GID == "" {
		return fmt.Sprintf("comment:index:%d", i)
	}
	return "comment:" + c.GID
}
