package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestActionUsesSharedFormAndPassesParameter(t *testing.T) {
	spec := form.Spec{Fields: []form.Field{{ID: "target", Label: "Target", Type: form.Select, Remember: true, Options: []form.Option{{ID: "prod", Name: "Production"}, {ID: "stage", Name: "Staging"}}}}}
	m, st := testModel(t, config.Config{Actions: []config.Action{{Name: "Deploy", Key: "d", Mode: config.ModeExit, Command: "true", Form: spec}}})
	tk := ticket.Ticket{Task: asana.Task{GID: "42", Name: "Fix", Memberships: []asana.Membership{{Project: asana.Ref{GID: "7", Name: "Web"}}}}}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["42"] = tk
	m.openActionMenu()
	if cmd := m.pickedAction(0); cmd != nil || m.form == nil || m.ExitCommand() != nil {
		t.Fatalf("action form = %+v, cmd = %v", m.form, cmd)
	}
	m.form.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form.picker == nil {
		t.Fatal("select did not open options")
	}
	m.form.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form.values["target"] != "prod" {
		t.Fatalf("selected value = %q", m.form.values["target"])
	}
	_, cmd := m.form.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil || m.form != nil || m.ExitCommand() == nil || !slices.Contains(m.ExitCommand().Env, "ASANAMATE_PARAM_TARGET=prod") {
		t.Fatal("form choice did not reach action command")
	}
	if got := st.RecentFormChoices("action:d", "7", "target"); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("saved action choice = %v", got)
	}
	m.openActionMenu()
	m.pickedAction(0)
	if m.form == nil || m.form.values["target"] != "prod" {
		t.Fatal("action form did not restore choice")
	}
}

func TestSharedFormEditsHoursAndCancelsPicker(t *testing.T) {
	spec := form.Spec{Fields: []form.Field{
		{ID: "project", Label: "Project", Type: form.Select, Options: []form.Option{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}}},
		{ID: "hours", Label: "Hours", Type: form.Hours},
	}}
	f := newFormModal("Log", spec, nil, func(map[string]string) tea.Cmd { return nil })
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	f.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.picker != nil || f.values["project"] != "" {
		t.Fatal("picker cancellation changed form")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	f.update(tea.KeyPressMsg{Code: tea.KeyTab})
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	f.input.SetValue("1.25")
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.values["project"] != "a" || f.values["hours"] != "1.25" || f.spec.ValidateValues(f.values) != nil {
		t.Fatalf("form values = %v", f.values)
	}
}

func builderPick(t *testing.T, m *Model, label string) {
	t.Helper()
	if m.builder == nil {
		t.Fatal("builder closed")
	}
	p := m.builder.menu
	p.input.SetValue("")
	p.refilter()
	for i, item := range p.items {
		if item.Label == label {
			p.cursor = i
			m.Update(key("enter"))
			return
		}
	}
	t.Fatalf("builder row %q missing in %q", label, p.title)
}

func builderText(t *testing.T, m *Model, label, value string) {
	t.Helper()
	builderPick(t, m, label)
	if m.builder.input == nil {
		t.Fatalf("%s did not open text editor", label)
	}
	m.builder.input.area.SetValue("")
	m.Update(tea.PasteMsg{Content: value})
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.builder.input != nil {
		t.Fatalf("%s editor stayed open", label)
	}
}

func TestActionBuilderCreatesEditsAndReloads(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	m.loading = false
	m.Update(key("A"))
	if m.modal == nil || m.modal.title != "Build / edit actions" {
		t.Fatal("ctrl+a did not open builder menu")
	}
	m.Update(key("enter"))
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.builder == nil || m.builder.menu.err == "" {
		t.Fatal("empty draft accepted")
	}
	builderText(t, m, "Filename", "10-deploy.toml")
	builderText(t, m, "Name", "Deploy")
	builderText(t, m, "Key", "D")
	builderPick(t, m, "Mode")
	builderPick(t, m, config.ModeBackground)
	builderPick(t, m, "Resolve repo")
	builderPick(t, m, "Choose agent")
	builderPick(t, m, "Context")
	builderPick(t, m, config.ContextComment)
	command := "  printf '%s\\n' \"$ASANAMATE_PARAM_TARGET\"\n# multiline\n"
	builderText(t, m, "Command", command)
	builderText(t, m, "Input title", "Notes")
	builderPick(t, m, "Confirm writes")
	builderPick(t, m, "off")
	builderPick(t, m, "Form fields")
	builderPick(t, m, "Add field")
	builderText(t, m, "ID", "target")
	builderText(t, m, "Label", "Target")
	builderPick(t, m, "Remember")
	builderPick(t, m, "Options")
	builderPick(t, m, "Add option")
	builderText(t, m, "ID", "prod")
	builderText(t, m, "Name", "Production")
	m.Update(key("esc"))
	m.Update(key("esc"))
	m.Update(key("esc"))
	builderPick(t, m, "Add field")
	builderText(t, m, "ID", "hours")
	builderText(t, m, "Label", "Hours")
	builderPick(t, m, "Type")
	builderPick(t, m, form.Hours)
	m.Update(key("esc"))
	m.Update(key("esc"))
	no := false
	want := config.Action{Name: "Deploy", Key: "D", Mode: config.ModeBackground, Repo: true, Agent: true, Context: config.ContextComment, Command: command, Input: "Notes", ConfirmWrites: &no,
		Form: form.Spec{Fields: []form.Field{
			{ID: "target", Label: "Target", Type: form.Select, Remember: true, Options: []form.Option{{ID: "prod", Name: "Production"}}},
			{ID: "hours", Label: "Hours", Type: form.Hours},
		}},
	}
	m.lastAction = 0
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.builder != nil || len(m.deps.Config.Actions) != 1 || !reflect.DeepEqual(m.deps.Config.Actions[0], want) || m.lastAction != -1 {
		t.Fatalf("saved actions: %+v, builder: %+v", m.deps.Config.Actions, m.builder)
	}
	// Open the existing action through Settings, then edit its nested option.
	m.Update(key("s"))
	m.Update(key("a"))
	m.Update(key("down"))
	m.Update(key("enter"))
	builderPick(t, m, "Filename")
	if m.builder.input != nil || m.builder.menu.err == "" {
		t.Fatal("existing file allowed rename")
	}
	builderText(t, m, "Name", "Ship")
	builderPick(t, m, "Confirm writes")
	builderPick(t, m, "inherit")
	builderPick(t, m, "Form fields")
	builderPick(t, m, "1. Target")
	builderPick(t, m, "Options")
	builderPick(t, m, "1. Production")
	builderText(t, m, "Name", "Live")
	for range 4 {
		m.Update(key("esc"))
	}
	builderPick(t, m, "Save action")
	want.Name = "Ship"
	want.ConfirmWrites = nil
	want.Form.Fields[0].Options[0].Name = "Live"
	if m.builder != nil || !reflect.DeepEqual(m.deps.Config.Actions[0], want) {
		t.Fatalf("edit did not reload: %+v", m.deps.Config.Actions)
	}
	files, err := config.LoadActionFiles(config.ActionsDir(m.deps.ConfigPath))
	if err != nil || len(files) != 1 || !reflect.DeepEqual(files[0].Action, want) {
		t.Fatalf("disk: %+v, %v", files, err)
	}
}

func TestActionBuilderCancelAndDeleteStayInDraft(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	m.loading = false
	original := config.ActionFile{Name: "draft.toml", Action: config.Action{Name: "Original", Key: "o", Mode: config.ModeForeground, Command: "true", Form: form.Spec{Fields: []form.Field{{ID: "target", Label: "Target", Type: form.Select, Options: []form.Option{{ID: "prod", Name: "Production"}}}}}}}
	dir := config.ActionsDir(m.deps.ConfigPath)
	actions, err := config.SaveActionFile(dir, original)
	if err != nil {
		t.Fatal(err)
	}
	m.deps.Config.Actions = actions
	before, _ := os.ReadFile(filepath.Join(dir, original.Name))
	m.openActionBuilder()
	m.Update(key("down"))
	m.Update(key("enter"))
	builderPick(t, m, "Name")
	m.builder.input.area.SetValue("Discarded")
	m.Update(key("esc"))
	if m.builder.file.Action.Name != "Original" {
		t.Fatal("esc committed text")
	}
	m.builder.menu.cursor = 0
	m.Update(key("tab"))
	if m.builder.menu.cursor != 1 {
		t.Fatal("tab did not move")
	}
	m.Update(key("shift+tab"))
	if m.builder.menu.cursor != 0 {
		t.Fatal("shift+tab did not move")
	}
	builderPick(t, m, "Form fields")
	builderPick(t, m, "1. Target")
	builderPick(t, m, "Options")
	builderPick(t, m, "1. Production")
	builderPick(t, m, "Delete option")
	if len(m.builder.file.Action.Form.Fields[0].Options) != 0 {
		t.Fatal("option not deleted")
	}
	m.Update(key("esc"))
	builderPick(t, m, "Delete field")
	m.Update(key("esc"))
	builderText(t, m, "Name", "Unsaved")
	m.Update(key("esc"))
	after, _ := os.ReadFile(filepath.Join(dir, original.Name))
	if m.builder != nil || string(before) != string(after) || m.deps.Config.Actions[0].Name != "Original" || len(m.deps.Config.Actions[0].Form.Fields[0].Options) != 1 {
		t.Fatal("cancel changed disk or running actions")
	}
	// Validate a duplicate key without closing the draft or creating a file.
	m.openActionBuilder()
	m.Update(key("enter"))
	builderText(t, m, "Filename", "new.toml")
	builderText(t, m, "Name", "New")
	builderText(t, m, "Key", "o")
	builderText(t, m, "Command", "true")
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.builder == nil || !strings.Contains(m.builder.menu.err, "already used") {
		t.Fatal("duplicate key did not leave error in draft")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.toml")); !os.IsNotExist(err) {
		t.Fatal("invalid draft created file")
	}
}

func TestActionBuilderCommandEditorPreservesLongAndUnchangedCommands(t *testing.T) {
	b := &actionBuilder{}
	b.showAction()
	command := strings.Repeat("printf '%s\\n' 'line'\n", 150)
	b.text("Command", &command, b.showAction, true)
	if b.input.area.Value() != command {
		t.Fatal("long command truncated")
	}
	b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if strings.Count(command, "\n") != 150 {
		t.Fatal("accept truncated long command")
	}
	command = "cat <<-EOF\n\ttext\n\tEOF\n"
	original := command
	b.text("Command", &command, b.showAction, true)
	b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if command != original {
		t.Fatal("unchanged editor converted command tabs")
	}
	b.text("Command", &command, b.showAction, true)
	changed := b.input.area.Value() + "# edited\n"
	b.input.area.SetValue(changed)
	b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if b.input == nil || b.input.err == "" || command != original {
		t.Fatal("conversion silently committed")
	}
	b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if b.input != nil || command != changed {
		t.Fatal("acknowledged edit did not commit")
	}
}

func TestActionBuilderCommandPasteRequiresConversionAcknowledgement(t *testing.T) {
	for _, original := range []string{"", "# existing\n"} {
		for _, pasted := range []string{"cat <<-EOF\n\ttext\n\tEOF\n", "printf 'a\x00b'\n", "\x00"} {
			t.Run(fmt.Sprintf("%q/%q", original, pasted), func(t *testing.T) {
				b := &actionBuilder{}
				b.showAction()
				command := original
				b.text("Command", &command, b.showAction, true)
				b.update(tea.PasteMsg{Content: pasted})
				converted := b.input.area.Value()
				b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
				if b.input == nil || b.input.err == "" || command != original {
					t.Fatal("pasted conversion silently committed")
				}
				b.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
				if b.input != nil || command != converted {
					t.Fatal("acknowledged paste did not commit")
				}
			})
		}
	}
}

func TestActionBuilderHelpFollowsSelectionAndStaysWhileEditing(t *testing.T) {
	m, _ := testModel(t, config.Default())
	b := &actionBuilder{file: config.ActionFile{Action: config.Action{Input: "Deployment notes"}}}
	b.showAction()
	m.builder = b
	b.menu.cursor = 8 // Input title
	view := ansi.Strip(b.view(80, 22, m.accentStyle))
	if !strings.Contains(view, "Leave blank to skip free-text input.") {
		t.Fatal("selected Input title has no description")
	}
	m.Update(key("up"))
	view = ansi.Strip(b.view(80, 22, m.accentStyle))
	if !strings.Contains(view, "Use $ASANAMATE_* variables") || strings.Contains(view, "Leave blank to skip") {
		t.Fatal("help did not follow selection")
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	if b.input == nil || b.input.area.Value() != "Deployment notes" {
		t.Fatal("input editor did not open populated")
	}
	view = ansi.Strip(b.view(80, 12, m.accentStyle))
	if !strings.Contains(view, "Leave blank to skip free-text input.") || !strings.Contains(view, "Deployment notes") {
		t.Fatal("populated editor hid help")
	}
	if lipgloss.Height(view) > 12 {
		t.Fatal("editor help pushed controls outside modal")
	}
	m.Update(key("esc"))
	builderPick(t, m, "Form fields")
	builderPick(t, m, "Add field")
	b.menu.cursor = 3 // Remember
	view = ansi.Strip(b.view(80, 16, m.accentStyle))
	if !strings.Contains(view, "For select fields only") {
		t.Fatal("nested field has no help")
	}
}

func TestActionBuilderBrowseSearchAndInfoIcon(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	m.openActionBuilder()
	if !m.modal.browseFirst {
		t.Fatal("action chooser opened in search mode")
	}
	m.Update(key("enter"))
	b := m.builder
	m.Update(key("j"))
	m.Update(key("k"))
	if b.menu.cursor != 0 || b.menu.input.Value() != "" {
		t.Fatal("builder j/k filtered")
	}
	m.Update(key("/"))
	for _, r := range "input" {
		m.Update(key(string(r)))
	}
	view := ansi.Strip(b.view(90, 22, m.accentStyle))
	if !strings.Contains(view, "(i) Leave blank") {
		t.Fatal("help has no info icon")
	}
	m.Update(key("esc"))
	if m.builder == nil || b.menu.searching || b.menu.cursor != 8 {
		t.Fatal("esc discarded draft instead of clearing search")
	}
	m.Update(key("enter"))
	if b.input == nil {
		t.Fatal("selected Input title did not open")
	}
	m.Update(tea.PasteMsg{Content: "j/k notes"})
	if b.input.area.Value() != "j/k notes" {
		t.Fatal("navigation intercepted editor text")
	}
	if !strings.Contains(ansi.Strip(b.view(90, 12, m.accentStyle)), "(i) Leave blank") {
		t.Fatal("editor help has no icon")
	}
	m.Update(key("esc"))
	builderPick(t, m, "Form fields")
	if !b.menu.browseFirst || b.menu.searching {
		t.Fatal("nested menu did not start in browse mode")
	}
}

func TestActionBuilderInfoIconFollowsSymbolSet(t *testing.T) {
	for _, tc := range []struct{ set, icon string }{
		{config.SymbolsNerd, "\uf05a"},
		{config.SymbolsUnicode, "(i)"},
		{config.SymbolsASCII, "(i)"},
	} {
		t.Run(tc.set, func(t *testing.T) {
			m, _ := testModel(t, config.Default())
			m.sym = newSymbols(tc.set, nil, false)
			m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
			m.openActionBuilder()
			m.Update(key("enter"))
			b := m.builder
			b.menu.cursor = 8
			if !strings.Contains(ansi.Strip(b.view(90, 22, m.accentStyle)), tc.icon+" Leave blank") {
				t.Fatal("action help used wrong info icon")
			}
			m.Update(key("enter"))
			if !strings.Contains(ansi.Strip(b.view(90, 12, m.accentStyle)), tc.icon+" Leave blank") {
				t.Fatal("text editor used wrong info icon")
			}
			m.Update(key("esc"))
			builderPick(t, m, "Form fields")
			if !strings.Contains(ansi.Strip(b.view(90, 16, m.accentStyle)), tc.icon+" Add a required") {
				t.Fatal("nested help used wrong info icon")
			}
		})
	}
}
