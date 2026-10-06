package tui

import (
	"maps"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// formModal renders the same control spec for actions and time providers.
type formModal struct {
	title    string
	spec     form.Spec
	values   map[string]string
	cursor   int
	input    textinput.Model
	editing  bool
	picker   *picker
	busy     bool
	err      string
	onSubmit func(map[string]string) tea.Cmd
}

func newFormModal(title string, spec form.Spec, defaults map[string]string, onSubmit func(map[string]string) tea.Cmd) *formModal {
	f := &formModal{title: title, spec: spec, values: map[string]string{}, onSubmit: onSubmit, input: textinput.New()}
	f.input.Prompt = "> "
	firstMissing := -1
	for i, field := range spec.Fields {
		if field.Type == form.Select {
			if field.HasOption(defaults[field.ID]) {
				f.values[field.ID] = defaults[field.ID]
			} else if len(field.Options) == 1 {
				f.values[field.ID] = field.Options[0].ID
			} else if firstMissing < 0 {
				firstMissing = i
			}
		} else if firstMissing < 0 {
			firstMissing = i
		}
	}
	if firstMissing >= 0 {
		f.cursor = firstMissing
	}
	return f
}

func (f *formModal) update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if f.busy {
		return false, nil
	}
	if f.picker != nil {
		res, cmd := f.picker.update(msg)
		if res.cancelled {
			f.picker = nil
		} else if res.done {
			f.values[f.spec.Fields[f.cursor].ID] = res.item.Value.(string)
			f.picker = nil
			f.err = ""
		}
		return false, cmd
	}
	field := f.spec.Fields[f.cursor]
	if f.editing {
		switch name := bound("form_field", msg, true); name {
		case "cancel":
			f.editing = false
			return false, nil
		case "done", "next":
			f.values[field.ID] = strings.TrimSpace(f.input.Value())
			f.editing = false
			f.err = ""
			if name == "next" {
				f.cursor = (f.cursor + 1) % len(f.spec.Fields)
			}
			return false, nil
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return false, cmd
	}
	switch bound("form", msg, false) {
	case "cancel":
		return true, nil
	case "prev":
		f.cursor = (f.cursor - 1 + len(f.spec.Fields)) % len(f.spec.Fields)
	case "next":
		f.cursor = (f.cursor + 1) % len(f.spec.Fields)
	case "edit":
		if field.Type == form.Select {
			items := make([]pickItem, len(field.Options))
			for i, option := range field.Options {
				items[i] = pickItem{Label: ticket.OneLine(option.Name), Hint: option.ID, Value: option.ID}
			}
			f.picker = newPicker(nil, ticket.OneLine(field.Label), items)
		} else {
			f.input.SetValue(f.values[field.ID])
			f.input.Focus()
			f.editing = true
		}
	case "submit":
		if err := f.spec.ValidateValues(f.values); err != nil {
			f.err = err.Error()
			return false, nil
		}
		return false, f.onSubmit(maps.Clone(f.values))
	}
	return false, nil
}

func (f *formModal) view(width, height int, accent lipgloss.Style) string {
	if f.picker != nil {
		return f.picker.view(width, height, accent)
	}
	var b strings.Builder
	b.WriteString(accent.Render(ticket.OneLine(f.title)) + "\n")
	for i, field := range f.spec.Fields {
		value := f.values[field.ID]
		if field.Type == form.Select {
			value = field.LabelFor(value)
		}
		if value == "" {
			value = "(choose)"
		}
		line := ticket.OneLine(field.Label) + ": " + ticket.OneLine(value)
		if i == f.cursor && f.editing {
			f.input.SetWidth(max(width-len(field.Label)-4, 1))
			line = ticket.OneLine(field.Label) + ": " + f.input.View()
		}
		line = ansi.Truncate(line, max(width-2, 1), "…")
		if i == f.cursor && !f.editing {
			line = accent.Reverse(true).Render("▸ " + ansi.Strip(line))
		} else if i == f.cursor {
			line = "▸ " + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if f.err != "" {
		b.WriteString(errorStyle.Render(ticket.OneLine(f.err)) + "\n")
	}
	b.WriteString(hintLine(accent, [2]string{keyLabel("form", "edit"), "edit"}, [2]string{keyLabel("form", "next"), "next"},
		[2]string{keyLabel("form", "submit"), "submit"}, [2]string{keyLabel("form", "cancel"), "cancel"}))
	return b.String()
}
