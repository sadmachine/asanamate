package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/ticket"
)

// chip is one field of the streamlined view: a styled prefix, the styled
// value, and its target key, or "" when it is not a target. An alone chip
// takes a row of its own.
type chip struct {
	prefix, value, key string
	alone              bool
}

// chipGap separates chips that share a row, drawn in the muted style.
const chipGap = " · "

// chipRows lays chips out in rows width columns wide, the first on reader
// line top, as many to a row as fit. An alone chip, or one wider than half
// the width, takes a row of its own, cut to fit. It marks the selected chip
// and records each target's line.
func (m *Model) chipRows(chips []chip, width, top int) []string {
	gap := dimStyle.Render(chipGap)
	var rows []string
	row := ""
	end := func() {
		if row != "" {
			rows, row = append(rows, row), ""
		}
	}
	for _, c := range chips {
		text := c.prefix + c.value
		if c.key != "" && c.key == m.fieldKey {
			text = m.fieldStyle().Render(ansi.Strip(text))
		}
		w := ansi.StringWidth(text)
		alone := c.alone || w > width/2
		switch {
		case alone:
			end()
			text = ansi.Truncate(text, width, "…")
		case row != "" && ansi.StringWidth(row+gap)+w > width:
			end()
		}
		if row != "" {
			row += gap
		}
		if c.key != "" {
			m.fieldLines[c.key] = top + len(rows)
		}
		row += text
		if alone {
			end()
		}
	}
	end()
	return rows
}

// metaChips are t's built-in fields, in the details card's order, each led
// by its icon when the symbol set has one and its label otherwise. While the
// focused reader selects one of them, they stack one to a row, each led by
// its icon and then its label, in aligned columns.
func (m *Model) metaChips(t ticket.Ticket, width int) []chip {
	icons := map[string]string{ticket.KeyAssignee: iconUser, ticket.KeyDue: iconDue, ticket.KeyMyTasks: iconMyTasks, ticket.KeyProject: iconProject}
	meta := detailMeta(t)
	stacked := m.focusReader && slices.ContainsFunc(meta, func(f ticket.Field) bool { return f.Key != "" && f.Key == m.fieldKey })
	var chips []chip
	glyphs := make([]string, len(meta))
	for i, f := range meta {
		if f.Key == addProjectKey {
			chips = append(chips, chip{value: m.addRow(f.Key, f.Label), key: f.Key, alone: stacked})
			continue
		}
		kind, _, _ := strings.Cut(f.Key, ":")
		prefix := m.sym.icon(icons[kind])
		switch {
		case stacked:
			glyphs[i], prefix = prefix, dimStyle.Render(f.Label)+" "
		case prefix == "" && (f.Label != "Status" || f.Key != ""):
			prefix = dimStyle.Render(f.Label) + " "
		}
		chips = append(chips, chip{prefix: prefix, value: m.fieldValue(t, f, width), key: f.Key, alone: stacked})
	}
	if stacked {
		glyphW, labelW := 0, 0
		for i, c := range chips {
			glyphW, labelW = max(glyphW, ansi.StringWidth(glyphs[i])), max(labelW, ansi.StringWidth(c.prefix))
		}
		for i := range chips {
			chips[i].prefix = padRight(glyphs[i], glyphW) + padRight(chips[i].prefix, labelW)
		}
	}
	return chips
}

// padRight pads s with spaces to width columns.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

// fieldChips are t's custom fields, each led by its label: the set ones, and
// the empty ones once shown, else a row that shows them.
func (m *Model) fieldChips(t ticket.Ticket, width int) []chip {
	fields, empty := t.FieldValues(), t.EmptyFields()
	if m.showEmpty {
		fields, empty = append(fields, empty...), nil
	}
	var chips []chip
	for _, f := range fields {
		chips = append(chips, chip{prefix: dimStyle.Render(ticket.Clean(f.Label)) + " ", value: m.fieldValue(t, f, width), key: f.Key})
	}
	if len(empty) > 0 {
		style := dimStyle.Italic(true)
		if m.fieldKey == emptyFieldsKey {
			style = m.fieldStyle()
		}
		chips = append(chips, chip{value: style.Render(emptyFieldsLabel(len(empty))), key: emptyFieldsKey, alone: true})
	}
	return chips
}
