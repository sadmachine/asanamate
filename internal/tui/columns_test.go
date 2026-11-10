package tui

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

func TestColumnSettingsSelectOrderCancelAndReset(t *testing.T) {
	cfg := config.Default()
	cfg.List.Fields = []string{"due", "assignee"}
	m, st := testModel(t, cfg)
	m.Update(tasksMsg{tasks: []asana.Task{fieldTask}})
	m.Update(key("s"))
	m.Update(key("c"))
	if m.modal == nil || !m.modal.multi || m.modal.title != "Columns · My Tasks" {
		t.Fatal("settings did not open column selection")
	}
	// Move due behind assignee, then turn due off. The checked state must
	// follow the moved item, and saving must keep the remaining order.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl})
	m.Update(key("tab"))
	search(m.modal, "tags")
	m.Update(key("tab"))
	m.Update(key("enter"))
	want := []state.Column{{Field: "assignee"}, {Field: "tags"}}
	if m.modal != nil || !slices.Equal(st.Columns[""], want) {
		t.Fatalf("saved columns = %v, want %v", st.Columns[""], want)
	}
	loaded, err := state.Load(st.Path())
	if err != nil || !slices.Equal(loaded.Columns[""], want) {
		t.Fatalf("persisted columns = %v, err = %v", loaded, err)
	}
	if !slices.Equal(m.deps.Config.List.Fields, cfg.List.Fields) {
		t.Fatal("column choice changed config defaults")
	}
	m.openColumns()
	m.Update(key("tab"))
	m.Update(key("esc"))
	if !slices.Equal(st.Columns[""], want) {
		t.Fatal("cancel changed saved columns")
	}
	m.openColumns()
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	loaded, err = state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := loaded.Columns[""]; exists {
		t.Fatal("reset copied defaults instead of removing the override")
	}
	m.deps.Config.List.Fields = []string{"tags"}
	if want := []state.Column{{Field: "tags"}}; !slices.Equal(m.listColumns(), want) {
		t.Fatalf("reset did not follow new defaults: %v", m.listColumns())
	}
}

func TestColumnsStayWithProjectAndPersistAcrossViews(t *testing.T) {
	cfg := config.Default()
	m, st := testModel(t, cfg)
	myColumns := []state.Column{{Field: "assignee"}, {Field: "Priority", GID: "f1"}}
	st.Columns[""] = myColumns
	st.Columns["p"] = []state.Column{}
	m.Update(tasksMsg{tasks: []asana.Task{fieldTask}})
	// Rendering the retained rows before a reload completes must use the
	// new project's widths, including when the column count changes.
	m.pickedProject(&asana.Ref{GID: "p", Name: "Project"})
	listLines(m, 80, 5)
	m.Update(tasksMsg{project: m.viewProject, tasks: []asana.Task{fieldTask}, catalog: []asana.Ref{}})
	if row, tail := m.listRow(0, m.cols); len(row) != 1 || tail != "" {
		t.Fatalf("empty project override shows fields: row = %v, tail = %q", row, tail)
	}
	st.SavedViews["Work"] = state.View{Filter: "is:open", GroupBy: "section"}
	m.applySavedView("Work")
	if len(m.listColumns()) != 0 {
		t.Fatal("named view replaced the project's columns")
	}
	m.pickedProject(&asana.Ref{GID: "other", Name: "Other"})
	listLines(m, 80, 5)
	wantDefault := []state.Column{{Field: "section"}, {Field: "due"}}
	if !slices.Equal(m.listColumns(), wantDefault) {
		t.Fatalf("unconfigured project did not fall back: %v", m.listColumns())
	}
	m.deps.Config.List.Fields = []string{"tags"}
	if !slices.Equal(m.listColumns(), []state.Column{{Field: "tags"}}) {
		t.Fatal("unconfigured project did not follow config changes")
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	next := New(Deps{Config: m.deps.Config, State: loaded})
	if !slices.Equal(next.listColumns(), myColumns) {
		t.Fatalf("restart lost My Tasks override: %v", next.listColumns())
	}
	next.pickedProject(&asana.Ref{GID: "p"})
	if columns, exists := loaded.Columns["p"]; !exists || len(columns) != 0 || len(next.listColumns()) != 0 {
		t.Fatal("restart lost explicit empty project override")
	}
}

func TestColumnCustomIDsRenderInBothLayouts(t *testing.T) {
	task := asana.Task{GID: "1", Name: "Ticket", DueOn: strp("2026-10-01"), CustomFields: []asana.CustomField{
		{GID: "other", Name: "Status", DisplayValue: strp("wrong value")},
		{GID: "selected", Name: "Renamed", DisplayValue: strp("right\tvalue\x1b")},
		{GID: "custom-due", Name: "due", DisplayValue: strp("custom date")},
	}}
	for _, layout := range []string{config.LayoutSingle, config.LayoutMulti} {
		t.Run(layout, func(t *testing.T) {
			m, st := testModel(t, config.Config{List: config.List{Layout: layout}})
			st.Columns[""] = []state.Column{{Field: "Status", GID: "selected"}, {Field: "due", GID: "custom-due"}}
			m.Update(tasksMsg{tasks: []asana.Task{task}})
			view := strings.Join(listLines(m, 80, 5), "\n")
			if !strings.Contains(view, "right value") || !strings.Contains(view, "custom date") || strings.Contains(view, "wrong value") || strings.Contains(view, "Thu") {
				t.Fatalf("wrong ID resolution in %s: %q", layout, view)
			}
			if value, _ := columnValue(task, st.Columns[""][0], m.rowContext(), testToday); value != "right value" {
				t.Fatalf("custom value was not sanitized: %q", value)
			}
			st.Columns[""] = []state.Column{{Field: "Status", GID: "removed"}}
			m.measureColumns()
			view = strings.Join(listLines(m, 80, 5), "\n")
			if strings.Contains(view, "wrong value") || strings.Contains(view, "right value") {
				t.Fatalf("missing ID fell back to same-named field: %q", view)
			}
		})
	}
}

func TestColumnPickerUsesProjectCatalogAndRetainsMissingSelections(t *testing.T) {
	m, st := testModel(t, config.Default())
	m.viewProject = &asana.Ref{GID: "p", Name: "Project"}
	st.Columns["p"] = []state.Column{{Field: "Old name", GID: "f1"}, {Field: "Removed", GID: "gone"}}
	m.cacheProjectFields("p", []asana.Ref{{GID: "f1", Name: "Status"}, {GID: "f2", Name: "Status"}, {GID: "f3", Name: "Unset"}})
	// Fields attached to the project are selectable even with no tasks.
	m.openColumns()
	p := m.modal
	if p.items[0].Label != "Status" || !strings.Contains(p.items[0].Hint, "f1") || !p.checked[0] || p.items[1].Hint != "unavailable" || !p.checked[1] {
		t.Fatalf("renamed or removed selection lost: %v", p.items[:2])
	}
	for _, gid := range []string{"f2", "f3"} {
		if !slices.ContainsFunc(p.items, func(item pickItem) bool { return item.Value.(state.Column).GID == gid }) {
			t.Fatalf("empty project missing field %q", gid)
		}
	}
	m.tasks = []asana.Task{{CustomFields: []asana.CustomField{{GID: "foreign", Name: "Other project's field"}, {GID: "f1", Name: "Status"}}}}
	if slices.ContainsFunc(m.availableColumns(), func(column state.Column) bool { return column.GID == "foreign" }) {
		t.Fatal("project picker offered a multi-homed task's foreign field")
	}
	m.viewProject = nil
	m.pinExtras = m.tasks
	columns := m.availableColumns()
	for _, gid := range []string{"f1", "foreign"} {
		count := 0
		for _, column := range columns {
			if column.GID == gid {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("My Tasks field %q offered %d times", gid, count)
		}
	}
}

func TestColumnPickerRetriesMetadataAndIgnoresOldProject(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.viewProject = &asana.Ref{GID: "p"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/p/custom_field_settings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(server.Close)
	m.deps.Client = asana.New("token")
	m.deps.Client.BaseURL = server.URL
	cmd := m.openColumns()
	if cmd == nil || m.modal != nil || m.waiting != "Loading project fields…" {
		t.Fatal("missing metadata did not start a request behind a loading modal")
	}
	var msg projectFieldsMsg
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if fields, ok := c().(projectFieldsMsg); ok {
			msg = fields
		}
	}
	m.Update(projectFieldsMsg{gid: "p", columns: true, err: errors.New("offline")})
	if m.modal != nil || m.waiting != "" || !strings.Contains(m.status, "offline") {
		t.Fatal("failed metadata silently offered incomplete choices")
	}
	m.Update(msg)
	if m.modal != nil {
		t.Fatal("a reply after the load ended opened a picker")
	}
	m.openColumns()
	m.viewProject = &asana.Ref{GID: "other"}
	m.Update(msg)
	if m.modal != nil {
		t.Fatal("old metadata request opened a picker in another project")
	}
	m.viewProject = &asana.Ref{GID: "p"}
	m.Update(msg)
	if m.modal == nil || len(m.projectCatalogs["p"]) != 0 {
		t.Fatal("successful empty catalog did not open common field choices")
	}
}

func TestColumnSaveResolvesLegacyNamesToProjectIDs(t *testing.T) {
	cfg := config.Default()
	cfg.List.Fields = []string{" STATUS ", "due"}
	m, st := testModel(t, cfg)
	m.viewProject = &asana.Ref{GID: "p"}
	m.Update(tasksMsg{project: m.viewProject, catalog: []asana.Ref{{GID: "f1", Name: "Status"}, {GID: "f2", Name: "due"}}})
	m.openColumns()
	m.Update(key("enter"))
	want := []state.Column{{Field: "Status", GID: "f1"}, {Field: "due"}}
	if !slices.Equal(st.Columns["p"], want) {
		t.Fatalf("name defaults did not resolve to stable IDs with native precedence: %v", st.Columns["p"])
	}
	if !slices.Equal(m.deps.Config.List.Fields, cfg.List.Fields) {
		t.Fatal("resolving names changed config")
	}
}

func TestColumnSaveFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"create", "replace", "reset"} {
		t.Run(operation, func(t *testing.T) {
			m, _ := testModel(t, config.Default())
			parent := filepath.Join(t.TempDir(), "blocked")
			st, err := state.Load(filepath.Join(parent, state.FileName))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			m.deps.State, m.loading = st, false
			if operation != "create" {
				st.Columns[""] = []state.Column{{Field: "assignee"}}
			}
			old, exists := st.Columns[""]
			m.openColumns()
			if operation == "reset" {
				m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
			} else {
				m.Update(key("tab"))
				m.Update(key("enter"))
			}
			got, remains := st.Columns[""]
			if remains != exists || !reflect.DeepEqual(got, old) || m.modal == nil || !strings.HasPrefix(m.modal.err, "Saving columns:") {
				t.Fatalf("failed %s did not roll back: columns = %v, picker = %+v", operation, st.Columns, m.modal)
			}
		})
	}
}
