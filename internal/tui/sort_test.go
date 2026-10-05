package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

func TestSortTasks(t *testing.T) {
	str := func(v string) *string { return &v }
	tasks := []asana.Task{
		{GID: "late", Name: "zebra", DueOn: str("2026-10-05"), Assignee: &asana.Ref{Name: "Bob"}},
		{GID: "missing", Name: "", DueOn: nil},
		{GID: "early", Name: "Alpha", DueOn: str("2026-09-28"), Assignee: &asana.Ref{Name: "alice"}},
		{GID: "tie", Name: "alpha", DueOn: str("2026-09-28"), Assignee: &asana.Ref{Name: "Alice"}},
		{GID: "invalid", Name: "Bravo", DueOn: str("bad")},
	}
	for _, tc := range []struct {
		by, direction string
		want          []string
	}{
		{"", "asc", []string{"late", "missing", "early", "tie", "invalid"}},
		{"due", "asc", []string{"early", "tie", "late", "missing", "invalid"}},
		{"due", "desc", []string{"late", "early", "tie", "missing", "invalid"}},
		{" assignee ", "asc", []string{"early", "tie", "late", "missing", "invalid"}},
		{"ASSIGNEE", "desc", []string{"late", "early", "tie", "missing", "invalid"}},
		{"title", "asc", []string{"early", "tie", "invalid", "late", "missing"}},
	} {
		t.Run(tc.by+tc.direction, func(t *testing.T) {
			got := slices.Clone(tasks)
			sortTasks(got, nil, config.Sort{By: tc.by, Direction: tc.direction}, rowContext{})
			var gids []string
			for _, task := range got {
				gids = append(gids, task.GID)
			}
			if !slices.Equal(gids, tc.want) {
				t.Fatalf("order = %v, want %v", gids, tc.want)
			}
		})
	}
}

func TestSortWithinGroupsPreservesGroupOrder(t *testing.T) {
	str := func(v string) *string { return &v }
	m, st := testModel(t, config.Config{List: config.List{GroupBy: "section"}})
	st.PinnedTasks = map[string][]string{"": {"pin2", "pin1"}}
	m.Update(tasksMsg{tasks: []asana.Task{
		{GID: "late", DueOn: str("2026-10-05"), AssigneeSection: &asana.Ref{Name: "Z"}},
		{GID: "other", DueOn: str("2026-09-01"), AssigneeSection: &asana.Ref{Name: "A"}},
		{GID: "early", DueOn: str("2026-09-28"), AssigneeSection: &asana.Ref{Name: "Z"}},
		{GID: "pin1", DueOn: str("2026-09-01")},
		{GID: "pin2", DueOn: str("2026-10-05")},
	}})
	m.moveTo(2) // late, after pins
	m.pickedSort(config.Sort{By: "due", Direction: "asc"})
	var gids []string
	for _, task := range m.visible {
		gids = append(gids, task.GID)
	}
	if want := []string{"pin2", "pin1", "early", "late", "other"}; !slices.Equal(gids, want) {
		t.Fatalf("order = %v, want %v", gids, want)
	}
	if want := []string{pinnedLabel, pinnedLabel, "Z", "Z", "A"}; !slices.Equal(m.groups, want) {
		t.Fatalf("groups = %v", m.groups)
	}
	if task, _ := m.selected(); task.GID != "late" {
		t.Fatalf("selection = %v", task)
	}
	if m.tasks[0].GID != "late" {
		t.Fatal("sort changed source order")
	}
	m.pickedSort(config.Sort{})
	if m.visible[2].GID != "late" {
		t.Fatal("None did not restore Asana order")
	}
}

func TestSortPickerAndProjectPersistence(t *testing.T) {
	m, st := testModel(t, config.Config{List: config.List{Sort: config.Sort{By: "due", Direction: "asc"}}})
	m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", CustomFields: []asana.CustomField{{Name: "Priority"}}}}})
	m.Update(key("B"))
	if m.modal == nil {
		t.Fatal("B did not open picker")
	}
	item := m.modal.items[m.modal.matches[m.modal.cursor]]
	if item.Label != "due asc" || item.Hint != "current" {
		t.Fatalf("current = %v", item)
	}
	var labels []string
	for _, item := range m.modal.items {
		labels = append(labels, item.Label)
	}
	for _, want := range []string{"None", "title asc", "assignee asc", "Priority desc"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("missing %s in %v", want, labels)
		}
	}
	m.modal.input.SetValue("assignee desc")
	m.modal.refilter()
	m.Update(key("enter"))
	want := config.Sort{By: "assignee", Direction: "desc"}
	if m.modal != nil || m.sortBy != want {
		t.Fatalf("sort = %v", m.sortBy)
	}
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	m.deps.State = loaded
	m.viewProject = &asana.Ref{GID: "other"}
	m.restoreView()
	if m.sortBy != m.deps.Config.List.Sort {
		t.Fatal("new project did not use default sort")
	}
	m.viewProject = nil
	m.restoreView()
	if m.sortBy != want {
		t.Fatalf("restored sort = %v", m.sortBy)
	}
}

func TestSortCustomFieldUsesActiveProject(t *testing.T) {
	a, z := "Alpha", "Zulu"
	tasks := []asana.Task{
		{GID: "1", CustomFields: []asana.CustomField{{GID: "other", Name: "Priority", DisplayValue: &a}, {GID: "active", Name: "Priority", DisplayValue: &z}}},
		{GID: "2", CustomFields: []asana.CustomField{{GID: "active", Name: "Priority", DisplayValue: &a}}},
	}
	sortTasks(tasks, nil, config.Sort{By: "Priority"}, rowContext{preferred: map[string]bool{"active": true}})
	if tasks[0].GID != "2" {
		t.Fatal("sort ignored active project's field")
	}
}

func TestViewsPanelSortAndScroll(t *testing.T) {
	m := wideModel(t, 200)
	keys(m, "0", "G")
	if body := ansi.Strip(m.navView(navW-panelFrame, 8)); !strings.Contains(body, "title") || !strings.Contains(body, "Sort by") {
		t.Fatalf("sort cursor not visible: %s", body)
	}
	keys(m, "k", "enter")
	if m.focusNav || m.sortBy.By != "assignee" {
		t.Fatalf("sort = %v, focus = %v", m.sortBy, m.focusNav)
	}
	if !strings.Contains(ansi.Strip(m.statusline()), "sort: assignee asc") {
		t.Fatal("statusline missing sort")
	}
}
