package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

func TestSavedViewSaveAndRecall(t *testing.T) {
	for _, noPreview := range []bool{false, true} {
		t.Run(map[bool]string{false: "narrow", true: "list-only"}[noPreview], func(t *testing.T) {
			m, st := testModel(t, config.Config{})
			m.deps.NoPreview, m.loading = noPreview, false
			m.tasks = []asana.Task{openTask, doneTask}
			m.filterInput.SetValue("is:done")
			m.groupBy = "section"
			m.applyFilter()
			want := state.View{Filter: "is:done", GroupBy: "section"}
			m.Update(key("ctrl+s"))
			if m.input == nil {
				t.Fatal("save key did not open the name input")
			}
			m.input.area.SetValue("  Finished work  ")
			m.Update(key("ctrl+s"))
			if m.input != nil || st.SavedViews["Finished work"] != want {
				t.Fatalf("input = %v, saved views = %v", m.input, st.SavedViews)
			}
			loaded, err := state.Load(st.Path())
			if err != nil || loaded.SavedViews["Finished work"] != want {
				t.Fatalf("saved view did not persist: state = %v, err = %v", loaded, err)
			}
			m.deps.State = loaded
			m.viewProject = &asana.Ref{GID: "other", Name: "Other project"}
			m.filterInput.SetValue("is:open")
			m.groupBy = ""
			m.pinned = &openTask
			m.Update(key("V"))
			if m.modal == nil {
				t.Fatal("recall key did not open saved views")
			}
			m.modal.input.SetValue("finished")
			m.modal.refilter()
			m.Update(key("enter"))
			if m.modal != nil || m.viewProject.GID != "other" || m.pinned != nil || m.groupBy != want.GroupBy || m.filterInput.Value() != want.Filter {
				t.Fatalf("recall did not apply to current project: project = %v, filter = %q, group = %q", m.viewProject, m.filterInput.Value(), m.groupBy)
			}
			if len(m.visible) != 1 || m.visible[0].GID != doneTask.GID {
				t.Fatalf("recalled filter shows %v", m.visible)
			}
			if v, ok := loaded.View("other"); !ok || v != want {
				t.Fatalf("current project's last view = %v, exists = %v", v, ok)
			}
			m.filterInput.SetValue("is:open")
			m.saveView()
			if loaded.SavedViews["Finished work"] != want {
				t.Fatal("editing current filter changed the saved snapshot")
			}
		})
	}
}

func TestSavedViewReplaceAndDeleteConfirmation(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.loading = false
	old := state.View{Filter: "is:open"}
	st.SavedViews["Work"] = old
	m.filterInput.SetValue("is:done")
	m.openSaveView()
	m.input.area.SetValue("Work")
	m.Update(key("ctrl+s"))
	if st.SavedViews["Work"] != old || m.modal == nil {
		t.Fatal("replacement did not wait for confirmation")
	}
	m.Update(key("enter")) // default is Cancel
	if st.SavedViews["Work"] != old {
		t.Fatal("cancel replaced the view")
	}
	m.openSaveView()
	m.input.area.SetValue("Work")
	m.Update(key("ctrl+s"))
	m.Update(key("down"))
	m.Update(key("enter"))
	if st.SavedViews["Work"].Filter != "is:done" {
		t.Fatal("confirmed replacement did not save")
	}
	m.openDeleteView()
	m.Update(key("enter"))
	m.Update(key("esc"))
	if _, exists := st.SavedViews["Work"]; !exists {
		t.Fatal("cancel deleted the view")
	}
	m.openDeleteView()
	m.Update(key("enter"))
	m.Update(key("down"))
	m.Update(key("enter"))
	loaded, err := state.Load(st.Path())
	if err != nil || len(st.SavedViews) != 0 || len(loaded.SavedViews) != 0 {
		t.Fatalf("delete did not persist: memory = %v, loaded = %v, err = %v", st.SavedViews, loaded, err)
	}
	if m.filterInput.Value() != "is:done" {
		t.Fatal("deleting a preset changed the current filter")
	}
}

func TestSavedViewRejectsInvalidName(t *testing.T) {
	m, st := testModel(t, config.Config{})
	for _, name := range []string{"", "   ", "two\nlines", "two\tcolumns", "escape\x1b"} {
		m.openSaveView()
		m.input.onSubmit(name)
		if m.input == nil || m.input.err == "" || len(st.SavedViews) != 0 {
			t.Fatalf("invalid name %q accepted", name)
		}
	}
}

func TestSavedViewWriteFailureRollsBack(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	parent := filepath.Join(t.TempDir(), "blocked")
	st, err := state.Load(filepath.Join(parent, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.deps.State = st
	old := state.View{Filter: "is:open"}
	newView := state.View{Filter: "is:done"}
	st.SavedViews["Work"] = old
	m.storeSavedView("Work", &newView)
	if st.SavedViews["Work"] != old || !strings.HasPrefix(m.status, "saving state:") {
		t.Fatalf("failed replacement did not roll back: views = %v, status = %q", st.SavedViews, m.status)
	}
	m.storeSavedView("Work", nil)
	if st.SavedViews["Work"] != old {
		t.Fatal("failed deletion did not roll back")
	}
	m.storeSavedView("New", &newView)
	if _, exists := st.SavedViews["New"]; exists {
		t.Fatal("failed creation remained in memory")
	}
}
