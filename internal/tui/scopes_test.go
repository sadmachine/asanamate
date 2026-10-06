package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
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
			"down":   func(t *testing.T) { expect(t, views(t, "down").navCursor == 1, "cursor did not move down") },
			"up":     func(t *testing.T) { expect(t, views(t, "down", "up").navCursor == 0, "cursor did not move up") },
			"top":    func(t *testing.T) { expect(t, views(t, "down", "down", "top").navCursor == 0, "cursor did not go to the top") },
			"bottom": func(t *testing.T) { expect(t, views(t, "bottom").navCursor > 1, "cursor did not go to the bottom") },
			"open":   func(t *testing.T) { expect(t, !views(t, "down", "open").focusNav, "open left the panel focused") },
		},
		"filter": {
			"done":   func(t *testing.T) { m := filtering(t, "done"); expect(t, !m.filtering && m.filterInput.Value() != "", "done") },
			"cancel": func(t *testing.T) { m := filtering(t, "cancel"); expect(t, !m.filtering && m.filterInput.Value() == "", "cancel") },
			"help":   func(t *testing.T) { expect(t, filtering(t, "help").help, "help did not open") },
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
			"bottom": func(t *testing.T) { p := abc(); p.update(firstKey("picker", "bottom")); expect(t, p.cursor == 2, "bottom") },
			"choose": func(t *testing.T) {
				res, _ := abc().update(firstKey("picker", "choose"))
				expect(t, res.done && res.item != nil && res.item.Label == "A", "choose")
			},
			"toggle": func(t *testing.T) {
				p := newMultiPicker(nil, "Pick", items("A"), map[int]bool{})
				p.update(firstKey("picker", "toggle"))
				expect(t, p.checked[0], "toggle")
			},
			"use_typed": func(t *testing.T) {
				p := abc()
				p.allowFree = true
				search(p, "x")
				res, _ := p.update(firstKey("picker", "use_typed"))
				expect(t, res.done && res.free == "x", "use_typed")
			},
			"search": func(t *testing.T) { p := abc(); p.update(firstKey("picker", "search")); expect(t, p.searching, "search") },
			"cancel": func(t *testing.T) {
				res, _ := abc().update(firstKey("picker", "cancel"))
				expect(t, res.cancelled, "cancel")
			},
		},
		"form": {
			"next": func(t *testing.T) { f := twoFields(); f.update(firstKey("form", "next")); expect(t, f.cursor == 1, "next") },
			"prev": func(t *testing.T) { f := twoFields(); f.update(firstKey("form", "prev")); expect(t, f.cursor == 1, "prev wraps") },
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
			"submit": func(t *testing.T) {
				res, _ := newInputBox("Comment", "").update(firstKey("input", "submit"))
				expect(t, res.done, "submit")
			},
			"cancel": func(t *testing.T) {
				res, _ := newInputBox("Comment", "").update(firstKey("input", "cancel"))
				expect(t, res.cancelled, "cancel")
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
		"notice": {
			"dismiss": func(t *testing.T) {
				m := splitModel(t)
				m.showNotice("hello")
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
