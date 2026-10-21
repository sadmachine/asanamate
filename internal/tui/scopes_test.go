package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/keymap"
)

// firstKey is the first default key of a binding.
func firstKey(scope, name string) tea.KeyPressMsg {
	return key(keymap.Default().Keys(scope, name)[0])
}

// TestEveryScopeBindingRuns presses each binding outside [keys.main] and
// checks its effect. The scopes are switch statements on binding names, so
// a misspelled case would otherwise never fire. A new binding without a
// check here fails the test.
func TestEveryScopeBindingRuns(t *testing.T) {
	views := func(t *testing.T, names ...string) *Model {
		m := wideModel(t, 200)
		m.Update(key("0"))
		for _, n := range names {
			m.Update(firstKey("views", n))
		}
		return m
	}
	filtering := func(t *testing.T, name string) *Model {
		m := splitModel(t)
		m.Update(key("/"))
		m.Update(key("x"))
		m.Update(firstKey("filter", name))
		return m
	}
	abc := func() *picker { return newPicker(nil, "Pick", items("A", "B", "C")) }
	twoFields := func() *formModal {
		spec := form.Spec{Fields: []form.Field{{ID: "a", Label: "A", Type: form.Hours}, {ID: "b", Label: "B", Type: form.Hours}}}
		return newFormModal("Form", spec, nil, func(map[string]string) tea.Cmd { return nil })
	}
	editing := func(name string) *formModal {
		f := twoFields()
		f.update(firstKey("form", "edit"))
		f.input.SetValue("1")
		f.update(firstKey("form_field", name))
		return f
	}

	checks := map[string]map[string]func(t *testing.T){
		"views": {
			"down": func(t *testing.T) { expect(t, views(t, "down").navCursor == 1, "cursor did not move down") },
			"up":   func(t *testing.T) { expect(t, views(t, "down", "up").navCursor == 0, "cursor did not move up") },
			"top": func(t *testing.T) {
				expect(t, views(t, "down", "down", "top").navCursor == 0, "cursor did not go to the top")
			},
			"bottom": func(t *testing.T) { expect(t, views(t, "bottom").navCursor > 1, "cursor did not go to the bottom") },
			"open":   func(t *testing.T) { expect(t, !views(t, "down", "open").focusNav, "open left the panel focused") },
		},
		"feed": {
			"next_source": func(t *testing.T) {
				expect(t, feedSources(t, "next_source").feed.source == "ci", "next_source did not show ci")
			},
			"prev_source": func(t *testing.T) {
				expect(t, feedSources(t, "prev_source").feed.source == "ci", "prev_source did not show ci")
			},
			"select": func(t *testing.T) {
				m := feedSources(t, "select")
				t1, _ := m.selected()
				expect(t, !m.focusFeed && t1.GID == "2", "select did not select the item's ticket")
			},
		},
		"filter": {
			"done": func(t *testing.T) {
				m := filtering(t, "done")
				expect(t, !m.filtering && m.filterInput.Value() != "", "done")
			},
			"cancel": func(t *testing.T) {
				m := filtering(t, "cancel")
				expect(t, !m.filtering && m.filterInput.Value() == "", "cancel")
			},
			"help": func(t *testing.T) { expect(t, filtering(t, "help").help, "help did not open") },
		},
		"picker": {
			"down": func(t *testing.T) { p := abc(); p.update(firstKey("picker", "down")); expect(t, p.cursor == 1, "down") },
			"up": func(t *testing.T) {
				p := abc()
				p.cursor = 1
				p.update(firstKey("picker", "up"))
				expect(t, p.cursor == 0, "up")
			},
			"top": func(t *testing.T) {
				p := abc()
				p.cursor = 2
				p.update(firstKey("picker", "top"))
				expect(t, p.cursor == 0, "top")
			},
			"bottom": func(t *testing.T) {
				p := abc()
				p.update(firstKey("picker", "bottom"))
				expect(t, p.cursor == 2, "bottom")
			},
			"choose": func(t *testing.T) {
				res, _ := abc().update(firstKey("picker", "choose"))
				expect(t, res.done && res.item != nil && res.item.Label == "A", "choose")
			},
			"toggle": func(t *testing.T) {
				p := newMultiPicker(nil, "Pick", items("A"), map[int]bool{})
				p.update(firstKey("picker", "toggle"))
				expect(t, p.checked[0], "toggle")
			},
			"copy": func(t *testing.T) {
				p := abc()
				p.links = true
				res, _ := p.update(firstKey("picker", "copy"))
				expect(t, res.done && res.copy, "copy")
			},
			"use_typed": func(t *testing.T) {
				p := abc()
				p.allowFree = true
				search(p, "x")
				res, _ := p.update(firstKey("picker", "use_typed"))
				expect(t, res.done && res.free == "x", "use_typed")
			},
			"search": func(t *testing.T) {
				p := abc()
				p.update(firstKey("picker", "search"))
				expect(t, p.searching, "search")
			},
			"cancel": func(t *testing.T) {
				res, _ := abc().update(firstKey("picker", "cancel"))
				expect(t, res.cancelled, "cancel")
			},
		},
		"form": {
			"external_editor": func(t *testing.T) {
				t.Setenv("VISUAL", "true")
				f := twoFields()
				_, cmd := f.update(firstKey("form", "external_editor"))
				runEditorTest(t, &Model{form: f}, cmd)
				expect(t, f.editing && f.err == "", "external_editor")
			},
			"next": func(t *testing.T) {
				f := twoFields()
				f.update(firstKey("form", "next"))
				expect(t, f.cursor == 1, "next")
			},
			"prev": func(t *testing.T) {
				f := twoFields()
				f.update(firstKey("form", "prev"))
				expect(t, f.cursor == 1, "prev wraps")
			},
			"edit": func(t *testing.T) { f := twoFields(); f.update(firstKey("form", "edit")); expect(t, f.editing, "edit") },
			"submit": func(t *testing.T) {
				f := twoFields()
				f.update(firstKey("form", "submit"))
				expect(t, f.err != "", "submit did not validate")
			},
			"cancel": func(t *testing.T) {
				cancelled, _ := twoFields().update(firstKey("form", "cancel"))
				expect(t, cancelled, "cancel")
			},
		},
		"form_field": {
			"external_editor": func(t *testing.T) {
				t.Setenv("VISUAL", "true")
				f := twoFields()
				f.editing = true
				_, cmd := f.update(firstKey("form_field", "external_editor"))
				runEditorTest(t, &Model{form: f}, cmd)
				expect(t, f.editing && f.err == "", "external_editor")
			},
			"done": func(t *testing.T) {
				f := editing("done")
				expect(t, !f.editing && f.values["a"] == "1" && f.cursor == 0, "done")
			},
			"next": func(t *testing.T) {
				f := editing("next")
				expect(t, !f.editing && f.values["a"] == "1" && f.cursor == 1, "next")
			},
			"cancel": func(t *testing.T) { f := editing("cancel"); expect(t, !f.editing && f.values["a"] == "", "cancel") },
		},
		"input": {
			"save_draft": func(t *testing.T) {
				m, _ := editModel(t)
				press(m, "c")
				m.input.area.SetValue("draft")
				send(m, firstKey("input", "save_draft"))
				expect(t, m.input == nil && savedComment(t, m).Text == "draft", "save_draft")
			},
			"external_editor": func(t *testing.T) {
				t.Setenv("VISUAL", "true")
				b := newInputBox("Comment", "")
				_, cmd := b.update(firstKey("input", "external_editor"))
				runEditorTest(t, &Model{input: b}, cmd)
				expect(t, b.err == "", "external_editor")
			},
			"submit": func(t *testing.T) {
				res, _ := newInputBox("Comment", "").update(firstKey("input", "submit"))
				expect(t, res.done, "submit")
			},
			"cancel": func(t *testing.T) {
				res, _ := newInputBox("Comment", "").update(firstKey("input", "cancel"))
				expect(t, res.cancelled, "cancel")
			},
			"paste_image": func(t *testing.T) {
				b := newInputBox("Comment", "")
				b.images = true
				_, cmd := b.update(firstKey("input", "paste_image"))
				expect(t, cmd != nil, "paste_image")
			},
		},
		"builder": {
			"save": func(t *testing.T) {
				m, _ := testModel(t, config.Default())
				m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
				m.openActionBuilder()
				m.Update(key("enter"))
				m.Update(firstKey("builder", "save"))
				expect(t, m.builder.menu.err != "", "save did not validate the draft")
			},
		},
		"activity": {
			"next_mode": func(t *testing.T) {
				m := activityModel(t, 160)
				m.Update(firstKey("activity", "next_mode"))
				expect(t, m.activity.mode == "Mine", "next mode")
			},
			"prev_mode": func(t *testing.T) {
				m := activityModel(t, 160)
				m.Update(firstKey("activity", "prev_mode"))
				expect(t, m.activity.mode == "Related", "previous mode")
			},
			"period": func(t *testing.T) {
				m := activityModel(t, 160)
				m.Update(firstKey("activity", "period"))
				expect(t, m.modal != nil, "period picker")
			},
			"automated": func(t *testing.T) {
				m := activityModel(t, 160)
				m.Update(firstKey("activity", "automated"))
				expect(t, m.activity.automated, "automated toggle")
			},
			"expand": func(t *testing.T) {
				m := activityRunModel(t)
				m.Update(firstKey("activity", "expand"))
				expect(t, len(m.activity.rows) == 5, "expanded run")
			},
		},
		"notice": {
			"copy": func(t *testing.T) {
				m := splitModel(t)
				m.setStatus(statusError, "hello")
				_, cmd := m.Update(firstKey("notice", "copy"))
				expect(t, cmd != nil && len(m.notices) == 1, "copy without dismissing")
			},
			"dismiss": func(t *testing.T) {
				m := splitModel(t)
				m.setStatus(statusError, "hello")
				m.Update(firstKey("notice", "dismiss"))
				expect(t, len(m.notices) == 0, "dismiss")
			},
		},
	}
	for _, s := range keymap.Catalog() {
		if s.Name == "main" { // TestEveryMainBindingHasAHandler
			continue
		}
		for _, b := range s.Bindings {
			check, ok := checks[s.Name][b.Name]
			if !ok {
				t.Errorf("no check for keys.%s.%s", s.Name, b.Name)
				continue
			}
			t.Run(s.Name+"."+b.Name, check)
		}
	}
}

func expect(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Fatal(msg)
	}
}

// feedSources is feedModel with one ci item on ticket 2, the Feed panel
// focused, and the [keys.feed] bindings named pressed.
func feedSources(t *testing.T, names ...string) *Model {
	m := feedModel(t)
	m.Update(feedMsg{reply: feed.Reply{Source: "ci", Items: []feed.Item{{Time: testToday, Text: "run", TicketGID: "2"}}}})
	m.Update(key("3"))
	for _, n := range names {
		m.Update(firstKey("feed", n))
	}
	return m
}
