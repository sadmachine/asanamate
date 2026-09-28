package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var (
	borderColor = lipgloss.Color("8")
	openStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	doneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// renderCards renders t as the reading pane's cards view: a details card,
// titled sections, and one card per comment, all width columns wide. It
// records the line of each editable row in m.fieldLines.
func (m *Model) renderCards(t ticket.Ticket, width int) string {
	name := lipgloss.NewStyle().Bold(true).Width(width).Render(ticket.Clean(t.Name))
	m.fieldLines = map[string]int{}
	blocks := []string{
		name,
		// Rows start below the name, a blank line, and the card's top edge.
		m.card("Details", m.detailsBody(t, width-4, lipgloss.Height(name)+2), width),
	}
	add := func(title, body string) {
		if body != "" {
			blocks = append(blocks, m.rule(title, width, false)+"\n"+body)
		}
	}

	var agentLines []string
	for _, a := range m.viewAgents(t.Task) {
		agentLines = append(agentLines, m.agentLabel(a, stateStyles[a.State])+dimStyle.Render(" — "+ticket.OneLine(filepath.Base(a.Path))))
	}
	for _, n := range m.agentNotes(t.Task) {
		agentLines = append(agentLines, dimStyle.Render(ticket.OneLine(n)))
	}
	add("Agents", wrap(strings.Join(agentLines, "\n"), width))
	add("Description", m.renderBody(t.Description(), width))

	var subtasks []string
	done := 0
	for _, s := range t.Subtasks {
		line := m.sym.open + " " + ticket.OneLine(s.Name)
		if s.Completed {
			done++
			line = dimStyle.Render(m.sym.done + " " + ticket.OneLine(s.Name))
		}
		subtasks = append(subtasks, line)
	}
	if len(t.Subtasks) > 0 {
		add(fmt.Sprintf("Subtasks %d/%d", done, len(t.Subtasks)), wrap(strings.Join(subtasks, "\n"), width))
	}
	add("Blocked by", wrap(refLines(t.Dependencies), width))
	add("Blocking", wrap(refLines(t.Dependents), width))

	var attachments []string
	for i, a := range t.Attachments {
		attachments = append(attachments, fmt.Sprintf("%d. %s\n   %s", i+1, ticket.OneLine(a.Name),
			dimStyle.Render(ansi.Truncate(ticket.OneLine(ticket.AttachmentURL(a)), width-3, "…"))))
	}
	add("Attachments", strings.Join(attachments, "\n"))

	// The Comments heading always shows: tabbing to it adds a comment.
	comments := []string{dimStyle.Render("none")}
	if len(t.Comments) > 0 {
		// Cards hug the capped text: its width plus border and padding.
		cardW := m.textWidth(width-4) + 4
		comments = make([]string, len(t.Comments))
		for i, c := range t.Comments {
			title := ticket.Author(c) + dimStyle.Render(" · "+ticket.Day(c.CreatedAt))
			comments[i] = m.card(title, m.renderBody(ticket.HTMLToMarkdown(c.HTMLText), cardW-4), cardW)
		}
	}
	m.fieldLines[commentKey] = lipgloss.Height(strings.Join(blocks, "\n\n")) + 1
	heading := m.rule(fmt.Sprintf("Comments %d", len(t.Comments)), width, m.fieldKey == commentKey)
	blocks = append(blocks, heading+"\n"+strings.Join(comments, "\n\n"))
	return strings.Join(blocks, "\n\n")
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
			if f.Label == "Status" {
				value = m.statusBadge(t.Completed)
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
		return doneStyle.Render(m.sym.done + " done")
	}
	return openStyle.Render(m.sym.open + " open")
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
	b := m.sym.border
	edge := lipgloss.NewStyle().Foreground(borderColor)
	box := lipgloss.NewStyle().Border(b).BorderTop(false).BorderForeground(borderColor).
		Padding(0, 1).Width(width).Render(body)
	boxW := lipgloss.Width(box)
	lead := b.TopLeft + b.Top + " "
	title = ansi.Truncate(title, max(boxW-ansi.StringWidth(lead)-ansi.StringWidth(b.TopRight)-1, 1), "…")
	fill := max(boxW-ansi.StringWidth(lead)-ansi.StringWidth(title)-1-ansi.StringWidth(b.TopRight), 0)
	top := edge.Render(lead) + titleStyle.Render(title) + " " + edge.Render(strings.Repeat(b.Top, fill)+b.TopRight)
	return top + "\n" + box
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
