package tui

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// actionBuilder owns a draft until Save. Nested editors never write files.
type actionBuilder struct {
	file     config.ActionFile
	infoIcon string
	menu     *picker
	input    *inputBox
	back     []func()
	root     bool
	done     bool
	save     func(config.ActionFile) error
}

func (m *Model) openActionBuilder() {
	path := m.deps.ConfigPath
	if path == "" {
		var err error
		path, err = config.Path()
		if err != nil {
			m.status = err.Error()
			return
		}
	}
	dir := config.ActionsDir(path)
	files, err := config.LoadActionFiles(dir)
	if err != nil {
		m.status = err.Error()
		return
	}
	open := func(file config.ActionFile) tea.Cmd {
		m.modal = nil
		b := &actionBuilder{file: file, infoIcon: m.sym.info}
		b.save = func(file config.ActionFile) error {
			actions, err := config.SaveActionFile(dir, file)
			if err != nil {
				return err
			}
			m.deps.Config.Actions = actions
			m.lastAction = -1 // filename order may have changed
			m.status = "Saved " + file.Name
			if file.Original != nil {
				m.status += " (original kept in actions/*.bak)"
			}
			return nil
		}
		b.showAction()
		m.builder = b
		return nil
	}
	items := []pickItem{{Label: "Create action", Value: config.ActionFile{Action: config.Action{Mode: config.ModeForeground}}}}
	for _, file := range files {
		items = append(items, pickItem{Label: ticket.OneLine(file.Action.Name), Hint: file.Name, Value: file})
	}
	m.modal = newPicker(pickValue(open), "Build / edit actions", items)
}

func builderRow(label, value, help string, edit func()) pickItem {
	return pickItem{Label: label, Hint: ticket.OneLine(value), Help: help, Value: edit}
}

func (b *actionBuilder) show(title string, rows []pickItem) {
	cursor := 0
	if b.menu != nil && b.menu.title == title && len(b.menu.matches) > 0 {
		cursor = b.menu.matches[b.menu.cursor]
	}
	b.menu = newPicker(pickValue(func(edit func()) tea.Cmd { edit(); return nil }), title, rows)
	b.menu.helpIcon = b.infoIcon
	b.menu.cursor = min(cursor, len(rows)-1)
	b.root = false
}

func (b *actionBuilder) descend(parent, child func()) {
	cursor := 0
	if len(b.menu.matches) > 0 {
		cursor = b.menu.matches[b.menu.cursor]
	}
	b.back = append(b.back, func() { parent(); b.menu.cursor = min(cursor, len(b.menu.matches)-1) })
	child()
}

func (b *actionBuilder) text(title string, value *string, parent func(), multiline bool) {
	help := ""
	if b.menu != nil && len(b.menu.matches) > 0 {
		help = b.menu.items[b.menu.matches[b.menu.cursor]].Help
	}
	b.input = newInputBox(title, "")
	b.input.help = help
	b.input.helpIcon = b.infoIcon
	b.input.area.MaxContentHeight = math.MaxInt
	b.input.area.SetValue(*value)
	original, displayed := *value, b.input.area.Value()
	acknowledged := ""
	hasAcknowledged := false
	b.input.onSubmit = func(string) tea.Cmd {
		text := b.input.area.Value()
		// Textarea sanitizes tabs and control characters. Merely opening an
		// editor must not change an existing command, and conversions on edit
		// need an explicit acknowledgement before they are saved.
		if multiline && text == displayed && !b.input.sanitizedPaste {
			text = original
		} else if multiline && (displayed != original || b.input.sanitizedPaste) && (!hasAcknowledged || text != acknowledged) {
			acknowledged = text
			hasAcknowledged = true
			b.input.err = "Editor changes tabs/control characters. Ctrl+S again accepts; Esc cancels."
			return nil
		}
		if !multiline {
			text = strings.TrimSpace(text)
		}
		*value = text
		b.input = nil
		parent()
		return nil
	}
}

func (b *actionBuilder) choice(title string, choices []string, set func(string), parent func()) {
	rows := make([]pickItem, len(choices))
	for i, value := range choices {
		rows[i] = builderRow(value, "", "", func() { set(value); b.goBack() })
	}
	b.descend(parent, func() { b.show(title, rows) })
}

func (b *actionBuilder) showAction() {
	a := &b.file.Action
	confirm := "inherit"
	if a.ConfirmWrites != nil {
		confirm = onOff(*a.ConfirmWrites)
	}
	context := a.Context
	if context == "" {
		context = "everywhere"
	}
	inputTitle := a.Input
	if inputTitle == "" {
		inputTitle = "(disabled)"
	}
	rows := []pickItem{
		builderRow("Filename", b.file.Name, "Action filename inside actions/, ending in .toml. Existing filenames stay fixed.", func() {
			if b.file.Original != nil {
				b.menu.err = "Existing filenames stay fixed; create a new action to use another filename"
				return
			}
			b.text("Filename", &b.file.Name, b.showAction, false)
		}),
		builderRow("Name", a.Name, "Required display name in the action menu.", func() { b.text("Name", &a.Name, b.showAction, false) }),
		builderRow("Key", a.Key, "One-character shortcut. Must be unique among actions with the same context.", func() { b.text("Key", &a.Key, b.showAction, false) }),
		builderRow("Mode", a.Mode, "Foreground resumes the TUI; background runs detached; exit closes the TUI.", func() {
			b.choice("Mode: foreground resumes; background detaches; exit quits", []string{config.ModeForeground, config.ModeBackground, config.ModeExit}, func(v string) { a.Mode = v }, b.showAction)
		}),
		builderRow("Resolve repo", onOff(a.Repo), "When on, resolve the ticket’s repository and run the command inside it.", func() { a.Repo = !a.Repo; b.showAction() }),
		builderRow("Choose agent", onOff(a.Agent), "When on, choose a linked agent before running; exposes ASANAMATE_AGENT_* values.", func() { a.Agent = !a.Agent; b.showAction() }),
		builderRow("Context", context, "Everywhere shows on any ticket; comment shows only for a highlighted comment.", func() {
			b.choice("Context", []string{"everywhere", config.ContextComment}, func(v string) {
				a.Context = v
				if v == "everywhere" {
					a.Context = ""
				}
			}, b.showAction)
		}),
		builderRow("Command", a.Command, "Required shell command (/bin/sh -c). Use $ASANAMATE_* variables for ticket data.", func() {
			b.text("Command (/bin/sh -c)", &a.Command, b.showAction, true)
		}),
		builderRow("Input title", inputTitle, "Leave blank to skip free-text input. Set a title to ask for notes before running.", func() {
			b.text("Input title", &a.Input, b.showAction, false)
		}),
		builderRow("Confirm writes", confirm, "Inherit the global write-confirmation setting, or override it for this action.", func() {
			b.choice("Confirm writes: inherit global setting or override", []string{"inherit", "on", "off"}, func(v string) {
				a.ConfirmWrites = nil
				if v != "inherit" {
					on := v == "on"
					a.ConfirmWrites = &on
				}
			}, b.showAction)
		}),
		builderRow("Form fields", fmt.Sprint(len(a.Form.Fields)), "Optional select/hour prompts before running. Answers become ASANAMATE_PARAM_<ID>.", func() { b.descend(b.showAction, b.showFields) }),
		builderRow("Save action", "ctrl+s", "Validate and write the action file. Changes become available immediately.", b.submit),
	}
	b.show("Action builder", rows)
	b.root = true
}

func (b *actionBuilder) showFields() {
	rows := []pickItem{builderRow("Add field", "select or hours", "Add a required select or hours prompt. Fields appear in the order listed.", func() {
		b.file.Action.Form.Fields = append(b.file.Action.Form.Fields, form.Field{Type: form.Select})
		b.openField(len(b.file.Action.Form.Fields) - 1)
	})}
	for i, field := range b.file.Action.Form.Fields {
		rows = append(rows, builderRow(fmt.Sprintf("%d. %s", i+1, field.Label), field.ID+" · "+field.Type, "Edit this field’s ID, label, type, remembered choices, and options.", func() { b.openField(i) }))
	}
	b.show("Form fields (ordered; esc returns)", rows)
}

func (b *actionBuilder) openField(i int) {
	show := func() { b.showField(i) }
	b.descend(b.showFields, show)
}

func (b *actionBuilder) showField(i int) {
	f := &b.file.Action.Form.Fields[i]
	parent := func() { b.showField(i) }
	rows := []pickItem{
		builderRow("ID", f.ID, "Unique snake_case ID. Answer is exported as ASANAMATE_PARAM_<ID>.", func() {
			b.text("Field ID", &f.ID, parent, false)
		}),
		builderRow("Label", f.Label, "Required label shown beside this prompt when the action runs.", func() { b.text("Field label", &f.Label, parent, false) }),
		builderRow("Type", f.Type, "Select requires options; hours requires a positive decimal number.", func() {
			b.choice("Field type", []string{form.Select, form.Hours}, func(v string) { f.Type = v }, parent)
		}),
		builderRow("Remember", onOff(f.Remember), "For select fields only: remember the chosen option for the Asana project.", func() { f.Remember = !f.Remember; parent() }),
		builderRow("Options", fmt.Sprint(len(f.Options)), "Select choices need unique IDs and display names. Hours fields cannot have options.", func() { b.descend(parent, func() { b.showOptions(i) }) }),
		builderRow("Delete field", "Remove from draft", "Remove this field and its options from the draft. File changes only on Save action.", func() {
			fields := b.file.Action.Form.Fields
			b.file.Action.Form.Fields = append(fields[:i], fields[i+1:]...)
			b.goBack()
		}),
	}
	b.show("Field (hours cannot have options or remember)", rows)
}

func (b *actionBuilder) showOptions(field int) {
	f := &b.file.Action.Form.Fields[field]
	rows := []pickItem{builderRow("Add option", "Stable ID + display name", "Add a select choice. Options appear in the order listed.", func() {
		f.Options = append(f.Options, form.Option{})
		b.openOption(field, len(f.Options)-1)
	})}
	for i, option := range f.Options {
		rows = append(rows, builderRow(fmt.Sprintf("%d. %s", i+1, option.Name), option.ID, "Edit the value passed to the command and its display name.", func() { b.openOption(field, i) }))
	}
	b.show("Select options (ordered; esc returns)", rows)
}

func (b *actionBuilder) openOption(field, option int) {
	b.descend(func() { b.showOptions(field) }, func() { b.showOption(field, option) })
}

func (b *actionBuilder) showOption(field, option int) {
	o := &b.file.Action.Form.Fields[field].Options[option]
	parent := func() { b.showOption(field, option) }
	b.show("Option", []pickItem{
		builderRow("ID", o.ID, "Required unique option ID. This value is passed to the command when selected.", func() { b.text("Option ID", &o.ID, parent, false) }),
		builderRow("Name", o.Name, "Required display name shown in the select menu.", func() { b.text("Option name", &o.Name, parent, false) }),
		builderRow("Delete option", "Remove from draft", "Remove this choice from the draft. File changes only on Save action.", func() {
			f := &b.file.Action.Form.Fields[field]
			f.Options = append(f.Options[:option], f.Options[option+1:]...)
			b.goBack()
		}),
	})
}

func (b *actionBuilder) goBack() {
	if len(b.back) == 0 {
		b.done = true
		return
	}
	last := len(b.back) - 1
	parent := b.back[last]
	b.back = b.back[:last]
	parent()
}

func (b *actionBuilder) submit() {
	if err := b.save(b.file); err != nil {
		b.menu.err = err.Error()
		return
	}
	b.done = true
}

func (b *actionBuilder) update(msg tea.Msg) tea.Cmd {
	if b.input != nil {
		res, cmd := b.input.update(msg)
		if res.cancelled {
			b.input = nil
		} else if res.done {
			return tea.Batch(cmd, b.input.onSubmit(res.free))
		}
		return cmd
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if b.root && bound("builder", key, false) == "save" {
		b.submit()
		return nil
	}
	res, cmd := b.menu.update(key)
	if res.cancelled {
		b.goBack()
	} else if res.done {
		return tea.Batch(cmd, b.menu.onPick(res))
	}
	return cmd
}

func (b *actionBuilder) view(width, height int, accent lipgloss.Style) string {
	if b.input != nil {
		return b.input.view(width, height, accent)
	}
	view := b.menu.view(width, height, accent)
	hint := " back"
	if b.root {
		hint = " discard draft"
	}
	view = strings.Replace(view, dimStyle.Render(" cancel"), dimStyle.Render(hint), 1)
	if b.root {
		view += dimStyle.Render(" · ") + accent.Render("ctrl+s") + dimStyle.Render(" save")
	}
	return view
}

func (m *Model) updateActionBuilder(msg tea.Msg) tea.Cmd {
	cmd := m.builder.update(msg)
	if m.builder.done {
		m.builder = nil
	}
	return cmd
}
