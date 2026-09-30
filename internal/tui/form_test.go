package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

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
