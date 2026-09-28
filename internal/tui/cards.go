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
	accentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	borderColor = lipgloss.Color("8")
	openStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	doneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// renderCards renders t as the reading pane's cards view: a details card,
// titled sections, and one card per comment, all width columns wide.
func (m *Model) renderCards(t ticket.Ticket, width int) string {
	blocks := []string{
		lipgloss.NewStyle().Bold(true).Width(width).Render(ticket.Clean(t.Name)),
		m.card("Details", m.detailsBody(t, width-4), width),
	}
	add := func(title, body string) {
		if body != "" {
			blocks = append(blocks, m.rule(title, width)+"\n"+body)
		}
	}

	var agentLines []string
	for _, a := range m.viewAgents(t.Task) {
		agentLines = append(agentLines, m.agentLabel(a)+dimStyle.Render(" — "+ticket.OneLine(filepath.Base(a.Path))))
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

	if len(t.Comments) > 0 {
		comments := make([]string, len(t.Comments))
		for i, c := range t.Comments {
			title := ticket.Author(c) + dimStyle.Render(" · "+ticket.Day(c.CreatedAt))
			comments[i] = m.card(title, m.renderBody(ticket.HTMLToMarkdown(c.HTMLText), width-4), width)
		}
		add(fmt.Sprintf("Comments %d", len(t.Comments)), strings.Join(comments, "\n\n"))
	}
	return strings.Join(blocks, "\n\n")
}

// detailsBody lists the built-in fields, then any custom fields below a rule.
func (m *Model) detailsBody(t ticket.Ticket, width int) string {
	meta, custom := t.Meta(), t.FieldValues()
	labelW := 0
	for _, f := range append(meta, custom...) {
		labelW = max(labelW, ansi.StringWidth(f.Label))
	}
	labelW = min(labelW, width/3)
	rows := func(fields []ticket.Field) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			value := f.Value
			if f.Label == "Status" {
				value = m.statusBadge(t.Completed)
			}
			label := lipgloss.NewStyle().Width(labelW + 2).Render(dimStyle.Render(ansi.Truncate(f.Label, labelW, "…")))
			out[i] = lipgloss.JoinHorizontal(lipgloss.Top, label, lipgloss.NewStyle().Width(max(width-labelW-2, 1)).Render(value))
		}
		return out
	}
	lines := rows(meta)
	if len(custom) > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(m.sym.border.Top, width)))
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

// rule is a section heading drawn as a full-width line with the title in it.
func (m *Model) rule(title string, width int) string {
	line := m.sym.border.Top
	lead := line + line + " "
	title = ansi.Truncate(title, max(width-ansi.StringWidth(lead)-2, 1), "…")
	fill := max(width-ansi.StringWidth(lead)-ansi.StringWidth(title)-1, 0)
	ruleStyle := lipgloss.NewStyle().Foreground(borderColor)
	return ruleStyle.Render(lead) + accentStyle.Render(title) + " " + ruleStyle.Render(strings.Repeat(line, fill))
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

// renderBody renders Markdown to fit width columns, without the document
// margins the markdown view uses.
func (m *Model) renderBody(md string, width int) string {
	if md == "" {
		return ""
	}
	return strings.Trim(m.glamour(md, width, true), "\n")
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
