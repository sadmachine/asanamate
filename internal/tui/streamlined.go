package tui

import (
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

// chipGap separates chips that share a row.
const chipGap = "   "

// chipRows lays chips out in rows width columns wide, the first on reader
// line top, as many to a row as fit. An alone chip, or one wider than half
// the width, takes a row of its own, cut to fit. It marks the selected chip
// and records each target's line.
func (m *Model) chipRows(chips []chip, width, top int) []string {
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
		case row != "" && ansi.StringWidth(row+chipGap)+w > width:
			end()
		}
		if row != "" {
			row += chipGap
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
// by its icon when the symbol set has one and its label otherwise.
func (m *Model) metaChips(t ticket.Ticket, width int) []chip {
	icons := map[string]string{ticket.KeyDue: iconDue, ticket.KeyProject: iconFolder}
	var chips []chip
	for _, f := range detailMeta(t) {
		if f.Key == addProjectKey {
			chips = append(chips, chip{value: m.addRow(f.Key, f.Label), key: f.Key})
			continue
		}
		kind, _, _ := strings.Cut(f.Key, ":")
		prefix := m.sym.icon(icons[kind])
		if prefix == "" && (f.Label != "Status" || f.Key != "") {
			prefix = dimStyle.Render(f.Label) + " "
		}
		chips = append(chips, chip{prefix: prefix, value: m.fieldValue(t, f, width), key: f.Key})
	}
	return chips
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
