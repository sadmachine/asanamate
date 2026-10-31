package tui

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/dateinput"
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
		switch {
		case field.Type == form.Select && field.HasOption(defaults[field.ID]):
			f.values[field.ID] = defaults[field.ID]
		case field.Default != "":
			f.values[field.ID] = field.Default
		case field.Type == form.Select && len(field.Options) == 1:
			f.values[field.ID] = field.Options[0].ID
		case field.Type == form.Bool:
			f.values[field.ID] = "false"
		case field.Type == form.Date:
			f.values[field.ID] = time.Now().Format(time.DateOnly)
		case field.Type == form.String:
		case firstMissing < 0:
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
	scope := "form"
	if f.editing {
		scope = "form_field"
	}
	if bound(scope, msg, f.editing) == "external_editor" && field.Type != form.Select && field.Type != form.Bool {
		value := f.values[field.ID]
		if f.editing {
			value = f.input.Value()
		}
		return false, externalEditor(value, func(text string, err error) {
			if err != nil {
				f.err = err.Error()
				return
			}
			f.input.SetValue(strings.TrimSpace(text))
			f.input.Focus()
			f.editing = true
			f.err = ""
		})
	}
	if f.editing {
		switch name := bound("form_field", msg, true); name {
		case "cancel":
			f.editing = false
			return false, nil
		case "done", "next":
			value := strings.TrimSpace(f.input.Value())
			if field.Type == form.Date {
				// date -d answers in milliseconds, so it runs inline.
				date, err := dateinput.Resolve(context.Background(), value)
				if err != nil {
					f.err = err.Error()
					return false, nil
				}
				value = date
			}
			f.values[field.ID] = value
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
		switch field.Type {
		case form.Select:
			items := make([]pickItem, len(field.Options))
			for i, option := range field.Options {
				items[i] = pickItem{Label: ticket.OneLine(option.Name), Hint: option.ID, Value: option.ID}
			}
			f.picker = newPicker(nil, ticket.OneLine(field.Label), items)
		case form.Bool:
			f.values[field.ID] = strconv.FormatBool(f.values[field.ID] != "true")
		default:
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

// fieldAt returns the field view draws on line.
func (f *formModal) fieldAt(line int) (int, bool) {
	i := line - 1 - len(f.spec.Info) // below the title and info lines
	return i, i >= 0 && i < len(f.spec.Fields)
}

func (f *formModal) view(width, height int, look modalLook) string {
	if f.picker != nil {
		return f.picker.view(width, height, look)
	}
	accent := look.accent
	var b strings.Builder
	b.WriteString(accent.Render(ticket.OneLine(f.title)) + "\n")
	for _, line := range f.spec.Info {
		b.WriteString(dimStyle.Render(ansi.Truncate(ticket.OneLine(line), max(width-2, 1), "…")) + "\n")
	}
	for i, field := range f.spec.Fields {
		value := f.values[field.ID]
		if field.Type == form.Select {
			value = field.LabelFor(value)
		}
		if value == "" && field.Type == form.String {
			value = "(empty)"
		} else if value == "" {
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
		b.WriteString(look.message(statusError, ticket.OneLine(f.err), width, 0) + "\n")
	}
	editorScope := "form"
	if f.editing {
		editorScope = "form_field"
	}
	b.WriteString(hintLine(accent, [2]string{keyLabel("form", "edit"), "edit"}, [2]string{keyLabel("form", "next"), "next"},
		[2]string{keyLabel("form", "submit"), "submit"}, [2]string{keyLabel(editorScope, "external_editor"), "editor"}, [2]string{keyLabel("form", "cancel"), "cancel"}))
	return b.String()
}
