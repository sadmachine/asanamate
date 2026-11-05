package tui

import (
	"slices"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
)

func TestRuleGroupsTakeTheFirstMatchingSection(t *testing.T) {
	g := config.Grouping{Name: "review", Sections: []config.GroupingSection{
		{Label: "Needs me", Match: []string{"agent:waiting", "git:dirty"}},
		{Label: "Ready", Match: []string{"is:open"}},
	}}
	tasks := []asana.Task{{GID: "done", Completed: true}, {GID: "open"}, {GID: "dirty"}, {GID: "asks"}}
	env := filter.Env{Lookup: map[string]func(asana.Task) []string{
		filter.KeyAgent: func(t asana.Task) []string {
			if t.GID == "asks" {
				return []string{"waiting"}
			}
			return nil
		},
		filter.KeyGit: func(t asana.Task) []string {
			if t.GID == "dirty" {
				return []string{"dirty"}
			}
			return nil
		},
	}}
	got, labels := ruleGroups(tasks, g, env)
	var gids []string
	for _, task := range got {
		gids = append(gids, task.GID)
	}
	if !slices.Equal(gids, []string{"dirty", "asks", "open", "done"}) || !slices.Equal(labels, []string{"Needs me", "Needs me", "Ready", config.OtherSection}) {
		t.Fatalf("grouped %q as %q", gids, labels)
	}
}

func TestStateGroupingRegroupsOnGitChecks(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{GroupBy: "state"}}})
	blocked := asana.Task{GID: "b", Name: "Blocked", Dependencies: []asana.Ref{{GID: "x"}}}
	m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", Name: "Fix"}, blocked, {GID: "d", Name: "Done", Completed: true}}})
	if !slices.Equal(m.groups, []string{"Blocked", "Ready", config.OtherSection}) {
		t.Fatalf("state groups = %q", m.groups)
	}
	m.Update(gitMsg{seq: m.feed.gitSeq, states: map[string][]string{"1": {gitDirty}}})
	if m.visible[0].GID != "1" || m.groups[0] != "Active" {
		t.Fatalf("a dirty branch must move its ticket to Active: %q", m.groups)
	}
}
