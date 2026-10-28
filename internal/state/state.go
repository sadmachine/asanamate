// Package state stores data asanamate learns while running: repo links, recent
// projects and tickets, the project list, explicit pins, saved views, each
// project's last list view, and the My Tasks tickets already seen.
package state

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
)

// FileName is the state file's name inside the state directory.
const FileName = "state.toml"

const maxRecent = 20

// maxRecentTickets caps the ticket history.
const maxRecentTickets = 10

// State is the contents of state.toml.
type State struct {
	// Repos maps an Asana project gid to a local git repository path.
	Repos map[string]string `toml:"repos"`
	// TaskRepos maps an Asana task gid to a repository path used for that task
	// only, in place of its projects' links.
	TaskRepos map[string]string `toml:"task_repos"`
	// TaskBranches maps an Asana task gid to a git branch typed for that task,
	// used in place of its branch field and the fallback.
	TaskBranches map[string]string `toml:"task_branches"`
	// HooksRun maps a once hook's key (config.Hook.Key) to the task gids it
	// has run for, so it never runs for them again.
	HooksRun map[string][]string `toml:"hooks_run,omitempty"`
	// Projects caches the projects the user is a member of, from the last
	// load, so pickers open before a fresh list arrives.
	Projects []Project `toml:"projects,omitempty"`
	// RecentProjects holds project gids, most recently opened first.
	RecentProjects []string `toml:"recent_projects"`
	// RecentTickets holds the tickets last focused in the reader, most recent first.
	RecentTickets []RecentTicket `toml:"recent_tickets"`
	// PinnedTasks maps a project gid ("" for My Tasks) to explicitly pinned task gids.
	PinnedTasks map[string][]string `toml:"pinned_tasks"`
	// Views maps a project gid ("" for My Tasks) to the list view last used there.
	Views map[string]View `toml:"views"`
	// Columns maps a project gid ("" for My Tasks) to ordered list columns.
	// An absent entry uses list.fields; an empty entry shows only the title.
	// Columns belong to the project, independently of named views.
	Columns map[string][]Column `toml:"columns,omitempty"`
	// SavedViews maps a user-friendly name to a reusable grouping and filter.
	SavedViews map[string]View `toml:"saved_views"`
	// RecentSavedViews holds saved view names, most recently applied or saved first.
	RecentSavedViews []string `toml:"recent_saved_views,omitempty"`
	// CurrentSavedViews maps a project gid ("" for My Tasks) to its named view.
	// The project's settings may have changed since that view was applied.
	CurrentSavedViews map[string]string `toml:"current_saved_views,omitempty"`
	// SeenTasks maps a task gid to its modified_at when last shown in the reader.
	// It covers the My Tasks list; nil means no My Tasks list has loaded yet.
	SeenTasks map[string]string `toml:"seen_tasks"`
	// TimeProjects holds recent tracker project IDs by provider ID and Asana project GID.
	// Kept so choices saved by earlier builds can seed the new form field.
	TimeProjects map[string]map[string][]string `toml:"time_projects"`
	// FormChoices holds recent select IDs by form owner, Asana project, and field.
	FormChoices map[string]map[string]map[string][]string `toml:"form_choices"`
	// LegacyDisplay migrates pre-v0.3 settings. Remove in v0.5.0.
	LegacyDisplay LegacyDisplay `toml:"display,omitempty"`

	path string
}

// LegacyDisplay decodes settings saved before settings.toml existed.
// Remove in the release after next (v0.5.0).
type LegacyDisplay struct {
	Separator       *bool  `toml:"separator,omitempty"`
	HeaderSpacing   *bool  `toml:"header_spacing,omitempty"`
	SelectionStyle  string `toml:"selection_style,omitempty"`
	ReaderView      string `toml:"reader_view,omitempty"`
	RefreshInterval string `toml:"refresh_interval,omitempty"`
	FocusDim        *bool  `toml:"focus_dim,omitempty"`
	// Theme is the theme picked in the TUI, over theme.name.
	Theme string `toml:"theme,omitempty"`
}

// View is a list grouping, sort, and filter; "" GroupBy is ungrouped.
type View struct {
	GroupBy string      `toml:"group_by"`
	Sort    config.Sort `toml:"sort"`
	Filter  string      `toml:"filter"`
}

// Column identifies a list value. Field is a built-in or legacy custom field
// name. GID, when set, identifies a custom field regardless of its name;
// Field then keeps its last known name for the picker.
type Column struct {
	Field string `toml:"field"`
	GID   string `toml:"gid,omitempty"`
}

// Project is a cached project's gid and name.
type Project struct {
	GID  string `toml:"gid"`
	Name string `toml:"name"`
}

// RecentTicket is a ticket in the history, with the names its picker row shows.
type RecentTicket struct {
	GID     string `toml:"gid"`
	Name    string `toml:"name"`
	Project string `toml:"project"`
}

// Load reads the state file. A missing file yields empty state.
func Load(path string) (*State, error) {
	s := &State{path: path}
	if _, err := toml.DecodeFile(path, s); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Repos == nil {
		s.Repos = map[string]string{}
	}
	if s.TaskRepos == nil {
		s.TaskRepos = map[string]string{}
	}
	if s.TaskBranches == nil {
		s.TaskBranches = map[string]string{}
	}
	if s.Views == nil {
		s.Views = map[string]View{}
	}
	if s.Columns == nil {
		s.Columns = map[string][]Column{}
	}
	if s.SavedViews == nil {
		s.SavedViews = map[string]View{}
	}
	if s.CurrentSavedViews == nil {
		s.CurrentSavedViews = map[string]string{}
	}
	if s.TimeProjects == nil {
		s.TimeProjects = map[string]map[string][]string{}
	}
	if s.FormChoices == nil {
		s.FormChoices = map[string]map[string]map[string][]string{}
	}
	return s, nil
}

// RecentFormChoices returns saved choices for a select field. The previous
// time-project state remains readable until the first new successful log.
func (s *State) RecentFormChoices(owner, projectGID, fieldID string) []string {
	if recent := s.FormChoices[owner][projectGID][fieldID]; len(recent) > 0 {
		return recent
	}
	if fieldID == "project_id" {
		if providerID, ok := strings.CutPrefix(owner, "time:"); ok {
			return s.TimeProjects[providerID][projectGID]
		}
	}
	return nil
}

// TouchFormChoice makes a chosen select value the most recent one.
func (s *State) TouchFormChoice(owner, projectGID, fieldID, choiceID string) {
	if s.FormChoices == nil {
		s.FormChoices = map[string]map[string]map[string][]string{}
	}
	if s.FormChoices[owner] == nil {
		s.FormChoices[owner] = map[string]map[string][]string{}
	}
	if s.FormChoices[owner][projectGID] == nil {
		s.FormChoices[owner][projectGID] = map[string][]string{}
	}
	recent := slices.DeleteFunc(s.FormChoices[owner][projectGID][fieldID], func(id string) bool { return id == choiceID })
	recent = append([]string{choiceID}, recent...)
	if len(recent) > maxRecent {
		recent = recent[:maxRecent]
	}
	s.FormChoices[owner][projectGID][fieldID] = recent
}

// Save writes the state atomically with owner-only permissions.
func (s *State) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// TouchProject moves gid to the front of the recent projects list.
func (s *State) TouchProject(gid string) {
	s.RecentProjects = slices.DeleteFunc(s.RecentProjects, func(g string) bool { return g == gid })
	s.RecentProjects = append([]string{gid}, s.RecentProjects...)
	if len(s.RecentProjects) > maxRecent {
		s.RecentProjects = s.RecentProjects[:maxRecent]
	}
}

// TouchTicket moves t to the front of the ticket history.
func (s *State) TouchTicket(t RecentTicket) {
	s.RecentTickets = slices.DeleteFunc(s.RecentTickets, func(r RecentTicket) bool { return r.GID == t.GID })
	s.RecentTickets = append([]RecentTicket{t}, s.RecentTickets...)
	if len(s.RecentTickets) > maxRecentTickets {
		s.RecentTickets = s.RecentTickets[:maxRecentTickets]
	}
}

// TouchSavedView moves name to the front of the saved view history.
func (s *State) TouchSavedView(name string) {
	s.RecentSavedViews = slices.DeleteFunc(s.RecentSavedViews, func(n string) bool { return n == name })
	s.RecentSavedViews = append([]string{name}, s.RecentSavedViews...)
	if len(s.RecentSavedViews) > maxRecent {
		s.RecentSavedViews = s.RecentSavedViews[:maxRecent]
	}
}

// LinkRepo remembers the repository used for a project.
func (s *State) LinkRepo(projectGID, path string) {
	s.Repos[projectGID] = path
}

// UnlinkRepo forgets the repository linked to a project.
func (s *State) UnlinkRepo(projectGID string) {
	delete(s.Repos, projectGID)
}

// LinkTaskRepo remembers the repository used for one task only.
func (s *State) LinkTaskRepo(taskGID, path string) {
	s.TaskRepos[taskGID] = path
}

// UnlinkTaskRepo forgets a task's own repository, so its projects' links apply.
func (s *State) UnlinkTaskRepo(taskGID string) {
	delete(s.TaskRepos, taskGID)
}

// HookRan reports whether the once hook key has run for the task.
func (s *State) HookRan(key, taskGID string) bool {
	return slices.Contains(s.HooksRun[key], taskGID)
}

// MarkHookRun records that the once hook key ran for the task.
func (s *State) MarkHookRun(key, taskGID string) {
	if s.HookRan(key, taskGID) {
		return
	}
	if s.HooksRun == nil {
		s.HooksRun = map[string][]string{}
	}
	s.HooksRun[key] = append(s.HooksRun[key], taskGID)
}

// SetTaskBranch remembers the git branch typed for a task; "" forgets it.
func (s *State) SetTaskBranch(taskGID, branch string) {
	if branch == "" {
		delete(s.TaskBranches, taskGID)
		return
	}
	s.TaskBranches[taskGID] = branch
}

// SyncSeen drops seen tasks that are no longer in the My Tasks list, tasks.
// The first sync records every task as seen, so an existing list starts
// unmarked. It reports whether the state changed.
func (s *State) SyncSeen(tasks []asana.Task) bool {
	if s.SeenTasks == nil {
		s.SeenTasks = make(map[string]string, len(tasks))
		for _, t := range tasks {
			s.SeenTasks[t.GID] = t.ModifiedAt
		}
		return true
	}
	listed := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		listed[t.GID] = true
	}
	n := len(s.SeenTasks)
	maps.DeleteFunc(s.SeenTasks, func(gid, _ string) bool { return !listed[gid] })
	return len(s.SeenTasks) != n
}

// MarkSeen records t as seen at its modified_at, once a My Tasks list has
// loaded. It reports whether the state changed.
func (s *State) MarkSeen(t asana.Task) bool {
	if s.SeenTasks == nil || t.ModifiedAt == "" || s.SeenTasks[t.GID] == t.ModifiedAt {
		return false
	}
	s.SeenTasks[t.GID] = t.ModifiedAt
	return true
}

// Changed reports whether t was modified since it was last seen, or was never
// seen, once a My Tasks list has loaded.
func (s *State) Changed(t asana.Task) bool {
	if s.SeenTasks == nil || t.ModifiedAt == "" {
		return false
	}
	seen, ok := s.SeenTasks[t.GID]
	return !ok || seen != t.ModifiedAt
}

// SetView remembers the list view used for a project.
func (s *State) SetView(projectGID string, v View) {
	s.Views[projectGID] = v
}

// View returns the list view last used for a project, if any.
func (s *State) View(projectGID string) (View, bool) {
	v, ok := s.Views[projectGID]
	return v, ok
}

// Path returns the file the state is saved to.
func (s *State) Path() string { return s.path }

// LinkedRepos returns t's own repo when set, otherwise the repos linked to its
// projects, in membership order.
func (s *State) LinkedRepos(t asana.Task) []string {
	if path, ok := s.TaskRepos[t.GID]; ok {
		return []string{path}
	}
	var repos []string
	for _, m := range t.Memberships {
		if path, ok := s.Repos[m.Project.GID]; ok {
			repos = append(repos, path)
		}
	}
	return repos
}
