package tui

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// sideBySideW is the narrowest reader that puts the agents and subtasks
// beside the details card instead of below it.
const sideBySideW = 90

// renderCards renders t as the reading pane's cards view: the name and a
// summary line, a details card with the agents and subtasks beside it (or
// below, when narrow), titled sections, and the comments as a timeline, all
// width columns wide. It records the line of each editable row in
// m.fieldLines.
func (m *Model) renderCards(t ticket.Ticket, width int) string {
	head := lipgloss.NewStyle().Bold(true).Width(width).Render(ticket.Clean(t.Name))
	if chips := m.summaryLine(t.Task, width); chips != "" {
		head += "\n" + chips
	}
	m.fieldLines = map[string]int{}
	var agentLines []string
	for _, a := range m.viewAgents(t.Task) {
		agentLines = append(agentLines, m.agentLabel(a, stateStyles[a.State])+dimStyle.Render(" — "+ticket.OneLine(filepath.Base(a.Path))))
	}
	for _, n := range m.agentNotes(t.Task) {
		agentLines = append(agentLines, dimStyle.Render(ticket.OneLine(n)))
	}
	subtaskTitle, subtasks := m.subtasks(t)

	// Rows start below the head, a blank line, and the card's top edge.
	top := lipgloss.Height(head) + 2
	var blocks []string
	add := func(title, body string) {
		if body != "" {
			blocks = append(blocks, m.rule(title, width, false)+"\n"+body)
		}
	}
	if width >= sideBySideW && (len(agentLines) > 0 || subtasks != "") {
		leftW := (width - 1) / 2
		rightW := width - leftW - 1
		var side []string
		if len(agentLines) > 0 {
			side = append(side, m.card(m.sym.robot+"  Agents", wrap(strings.Join(agentLines, "\n"), rightW-4), rightW))
		}
		if subtasks != "" {
			side = append(side, m.card(subtaskTitle, wrap(subtasks, rightW-4), rightW))
		}
		details := m.card("Details", m.detailsBody(t, leftW-4, top), leftW)
		blocks = []string{head, lipgloss.JoinHorizontal(lipgloss.Top, details, " ", strings.Join(side, "\n"))}
		add("Description", m.renderBody(t.Description(), width))
	} else {
		blocks = []string{head, m.card("Details", m.detailsBody(t, width-4, top), width)}
		add("Agents", wrap(strings.Join(agentLines, "\n"), width))
		add("Description", m.renderBody(t.Description(), width))
		add(subtaskTitle, wrap(subtasks, width))
	}
	add("Blocked by", wrap(refLines(t.Dependencies), width))
	add("Blocking", wrap(refLines(t.Dependents), width))

	var attachments []string
	for i, a := range t.Attachments {
		attachments = append(attachments, fmt.Sprintf("%d. %s%s\n   %s", i+1, m.sym.icon(iconClip), ticket.OneLine(a.Name),
			dimStyle.Render(ansi.Truncate(ticket.OneLine(ticket.AttachmentURL(a)), width-3, "…"))))
	}
	add(fmt.Sprintf("Attachments %d", len(t.Attachments)), strings.Join(attachments, "\n"))

	// The Comments heading always shows: tabbing to it adds a comment.
	comments := []string{dimStyle.Render("none")}
	if len(t.Comments) > 0 {
		comments = make([]string, len(t.Comments))
		for i, c := range t.Comments {
			comments[i] = m.comment(c, width)
		}
	}
	m.fieldLines[commentKey] = lipgloss.Height(strings.Join(blocks, "\n\n")) + 1
	heading := m.rule(fmt.Sprintf("Comments %d", len(t.Comments)), width, m.fieldKey == commentKey)
	blocks = append(blocks, heading+"\n"+strings.Join(comments, "\n\n"))
	return strings.Join(blocks, "\n\n")
}

// summaryLine is the faint line under the ticket's name: its project and
// section, due date, and branch, cut to width.
func (m *Model) summaryLine(t asana.Task, width int) string {
	var chips []string
	rc := m.rowContext()
	for _, ms := range t.Memberships {
		if rc.projectGID == "" || ms.Project.GID == rc.projectGID {
			chip := m.sym.icon(iconFolder) + ticket.OneLine(ms.Project.Name)
			if ms.Section != nil {
				chip += " › " + ticket.OneLine(ms.Section.Name)
			}
			chips = append(chips, dimStyle.Render(chip))
			break
		}
	}
	if rel, style := dueLabel(t.DueOn, m.now()); rel != "" {
		due, _, _ := dueDays(t.DueOn, m.now())
		chips = append(chips, style.Render(m.sym.icon(iconDue)+shortDate(due, m.now())+" · "+rel))
	}
	if name := m.deps.Config.BranchField; name != "" {
		if f, ok := t.Field(name, rc.preferred); ok && f.Value() != "" {
			chips = append(chips, dimStyle.Render(m.sym.icon(iconBranch)+ticket.OneLine(f.Value())))
		}
	}
	return ansi.Truncate(strings.Join(chips, "   "), width, "…")
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
// the day, over the body behind a faint gutter.
func (m *Model) comment(c asana.Story, width int) string {
	author := ticket.Author(c)
	day, err := time.Parse(time.DateOnly, ticket.Day(c.CreatedAt))
	when := ticket.Day(c.CreatedAt)
	if err == nil {
		when = shortDate(day, m.now())
	}
	head := authorStyle(author).Render("●") + " " + titleStyle.Render(author) + dimStyle.Render(" · "+when)
	body := m.renderBody(ticket.HTMLToMarkdown(c.HTMLText), width-2)
	gutter := lipgloss.NewStyle().Foreground(borderColor).Render("│") + " "
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = gutter + l
	}
	return head + "\n" + strings.Join(lines, "\n")
}

// authorColors tell comment authors apart: ANSI blue, magenta, cyan, green,
// yellow, and red.
var authorColors = []string{"4", "5", "6", "2", "3", "1"}

// authorStyle colors an author the same way every time.
func authorStyle(name string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(authorColors[h.Sum32()%uint32(len(authorColors))]))
}

// shortDate shows a date as "Sep 26", with the year when it is not this year's.
func shortDate(d, today time.Time) string {
	if d.Year() == today.Year() {
		return d.Format("Jan 2")
	}
	return d.Format("Jan 2 2006")
}

// detailsBody lists the built-in fields, then any custom fields below a rule.
// top is the reader line of its first row, for m.fieldLines.
func (m *Model) detailsBody(t ticket.Ticket, width, top int) string {
	meta, custom := t.Meta(), t.FieldValues()
	labelW := 0
	for _, f := range append(meta, custom...) {
		labelW = max(labelW, ansi.StringWidth(f.Label))
	}
	labelW = min(labelW, width/3)
	line := top
	rows := func(fields []ticket.Field) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			value := f.Value
			switch f.Label {
			case "Status":
				value = m.statusBadge(t.Completed)
			case "Due":
				if due, _, ok := dueDays(t.DueOn, m.now()); ok {
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
	if len(custom) > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(m.sym.border.Top, width)))
		line++
		lines = append(lines, rows(custom)...)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) statusBadge(completed bool) string {
	if completed {
		return okStyle.Render(m.sym.done + " done")
	}
	return warnStyle.Render(m.sym.open + " open")
}

// fieldStyle marks the row the cards view has tabbed to.
func (m *Model) fieldStyle() lipgloss.Style { return m.markerStyle.Reverse(true) }

// rule is a section heading drawn as a full-width line with the title in it,
// marked when it is the tabbed-to row.
func (m *Model) rule(title string, width int, selected bool) string {
	line := m.sym.border.Top
	lead := line + line + " "
	title = ansi.Truncate(title, max(width-ansi.StringWidth(lead)-2, 1), "…")
	fill := max(width-ansi.StringWidth(lead)-ansi.StringWidth(title)-1, 0)
	ruleStyle := lipgloss.NewStyle().Foreground(borderColor)
	headStyle := m.accentStyle
	if selected {
		headStyle = m.fieldStyle()
	}
	return ruleStyle.Render(lead) + headStyle.Render(title) + " " + ruleStyle.Render(strings.Repeat(line, fill))
}

// card boxes body in a border with title set into the top edge. body must
// already fit width-4 columns (border and padding take the rest).
func (m *Model) card(title, body string, width int) string {
	box := lipgloss.NewStyle().Border(m.sym.border).BorderTop(false).BorderForeground(borderColor).
		Padding(0, 1).Width(width).Render(body)
	edge := lipgloss.NewStyle().Foreground(borderColor)
	return m.topEdge(titleStyle.Render(title), "", lipgloss.Width(box), edge) + "\n" + box
}

// panel boxes a pane's body, width by height cells in all, with title and
// right set into the top edge. The focused panel's edge is in the accent color.
// body must already fit width-2 by height-2 cells.
func (m *Model) panel(title, right, body string, width, height int, focused bool) string {
	edge, head := lipgloss.NewStyle().Foreground(borderColor), dimStyle
	if focused {
		edge, head = lipgloss.NewStyle().Foreground(m.accentStyle.GetForeground()), m.accentStyle
	}
	box := lipgloss.NewStyle().Border(m.sym.border).BorderTop(false).BorderForeground(edge.GetForeground()).
		Width(width).Height(height - 1).MaxHeight(height - 1).Render(body)
	if right != "" {
		right = dimStyle.Render(right)
	}
	return m.topEdge(head.Render(title), right, width, edge) + "\n" + box
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

func refLines(refs []asana.Ref) string {
	lines := make([]string, len(refs))
	for i, r := range refs {
		lines[i] = "- " + ticket.OneLine(r.Name)
	}
	return strings.Join(lines, "\n")
}
