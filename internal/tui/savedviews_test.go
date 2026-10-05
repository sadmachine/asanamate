package tui

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
			m.sortBy = config.Sort{By: "due", Direction: "desc"}
			m.applyFilter()
			want := state.View{Filter: "is:done", GroupBy: "section", Sort: m.sortBy}
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
			m.sortBy = config.Sort{}
			m.viewing = &openTask
			m.Update(key("V"))
			if m.modal == nil {
				t.Fatal("recall key did not open saved views")
			}
			m.modal.input.SetValue("finished")
			m.modal.refilter()
			m.Update(key("enter"))
			if m.modal != nil || m.viewProject.GID != "other" || m.viewing != nil || m.groupBy != want.GroupBy || m.sortBy != want.Sort || m.filterInput.Value() != want.Filter {
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
	st.RecentSavedViews = []string{"Work", "Other"}
	st.CurrentSavedViews[""] = "Work"
	st.CurrentSavedViews["project"] = "Work"
	st.SetView("", old)
	m.savedView = "Work"
	wantRecent := slices.Clone(st.RecentSavedViews)
	wantCurrent := maps.Clone(st.CurrentSavedViews)
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
	if !slices.Equal(st.RecentSavedViews, wantRecent) || !maps.Equal(st.CurrentSavedViews, wantCurrent) || st.Views[""] != old || m.savedView != "Work" {
		t.Fatalf("failed write changed metadata: %+v, name = %q", st, m.savedView)
	}
}

func TestSavedViewIdentityAndModifiedSettings(t *testing.T) {
	m, st := testModel(t, config.Config{})
	want := state.View{Filter: "is:open", GroupBy: "section", Sort: config.Sort{By: "due", Direction: "asc"}}
	st.SavedViews["Daily work"] = want
	if m.savedViewLabel() != "Custom" {
		t.Fatalf("unassociated view = %q", m.savedViewLabel())
	}
	m.applySavedView("Daily work")
	for _, change := range []func(){
		func() { m.filterInput.SetValue("is:done") },
		func() { m.groupBy = "due" },
		func() { m.sortBy.Direction = "desc" },
	} {
		change()
		if m.savedViewLabel() != "Daily work (modified)" || st.SavedViews["Daily work"] != want {
			t.Fatalf("modified label = %q, snapshot = %+v", m.savedViewLabel(), st.SavedViews["Daily work"])
		}
		m.filterInput.SetValue(want.Filter)
		m.groupBy, m.sortBy = want.GroupBy, want.Sort
		if m.savedViewLabel() != "Daily work" {
			t.Fatalf("reverted label = %q", m.savedViewLabel())
		}
	}
	title, _ := m.listTitle()
	if !strings.Contains(title, "My Tasks · Daily work") {
		t.Fatalf("list title = %q", title)
	}
	for _, noPreview := range []bool{false, true} {
		m.width, m.deps.NoPreview = 80, noPreview
		if !strings.Contains(ansi.Strip(m.statusline()), "Daily work") {
			t.Fatalf("single-pane status = %q", ansi.Strip(m.statusline()))
		}
	}
}

func TestSavedViewIdentitySurvivesRestartAndProjectSwitch(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.SavedViews["Work"] = state.View{Filter: "is:open"}
	st.SavedViews["Done"] = state.View{Filter: "is:done"}
	m.applySavedView("Work")
	m.filterInput.SetValue("is:open tag:urgent")
	m.saveView()
	m.pickedProject(&asana.Ref{GID: "project", Name: "Project"})
	if m.savedViewLabel() != "Custom" {
		t.Fatalf("new project inherited name: %q", m.savedViewLabel())
	}
	m.applySavedView("Done")
	m.pickedProject(nil)
	if m.savedViewLabel() != "Work (modified)" || m.filterInput.Value() != "is:open tag:urgent" {
		t.Fatalf("My Tasks lost identity: %q, filter = %q", m.savedViewLabel(), m.filterInput.Value())
	}
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	m.deps.State = loaded
	m.restoreView()
	if m.savedViewLabel() != "Work (modified)" || !slices.Equal(loaded.RecentSavedViews, []string{"Done", "Work"}) {
		t.Fatalf("restart lost identity or history: %q, %+v", m.savedViewLabel(), loaded)
	}
	m.pickedProject(&asana.Ref{GID: "project", Name: "Project"})
	if m.savedViewLabel() != "Done" || m.filterInput.Value() != "is:done" {
		t.Fatalf("project lost identity: %q", m.savedViewLabel())
	}
	m.storeSavedView("Done", nil)
	if m.savedViewLabel() != "Custom" || slices.Contains(loaded.RecentSavedViews, "Done") || loaded.CurrentSavedViews["project"] != "" {
		t.Fatalf("deletion kept identity or history: %q, %+v", m.savedViewLabel(), loaded)
	}
}

func TestSavedViewSaveAssociatesNameAndDeletionClearsAllScopes(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.filterInput.SetValue("is:open")
	v := m.currentView()
	m.storeSavedView("Work", &v)
	loaded, err := state.Load(st.Path())
	if err != nil || m.savedViewLabel() != "Work" || loaded.CurrentSavedViews[""] != "Work" || loaded.Views[""] != v || !slices.Equal(loaded.RecentSavedViews, []string{"Work"}) {
		t.Fatalf("save did not associate settings: label = %q, state = %+v, err = %v", m.savedViewLabel(), loaded, err)
	}
	st.CurrentSavedViews["project"] = "Work"
	m.storeSavedView("Work", nil)
	if m.savedView != "" || len(st.CurrentSavedViews) != 0 || len(st.RecentSavedViews) != 0 || m.currentView() != v {
		t.Fatalf("deletion lost settings or kept metadata: %+v", st)
	}
}

func TestSavedViewCycle(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.loading = false
	m.Update(key("]"))
	if m.status != "no saved views" {
		t.Fatalf("status = %q", m.status)
	}
	st.SavedViews["b"] = state.View{Filter: "is:done"}
	st.SavedViews["a"] = state.View{Filter: "is:open", GroupBy: "section"}
	for _, step := range []struct{ key, filter string }{{"]", "is:open"}, {"]", "is:done"}, {"]", "is:open"}, {"[", "is:done"}} {
		m.Update(key(step.key))
		if m.filterInput.Value() != step.filter {
			t.Fatalf("after %s filter = %q, want %q", step.key, m.filterInput.Value(), step.filter)
		}
	}
	if m.groupBy != "" {
		t.Fatalf("group = %q", m.groupBy)
	}
}
