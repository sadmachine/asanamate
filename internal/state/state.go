// Package state stores data asanamate learns while running: repo links, recent
// projects and tickets, and each project's last list view.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sadmachine/asanamate/internal/asana"
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
	// RecentProjects holds project gids, most recently opened first.
	RecentProjects []string `toml:"recent_projects"`
	// RecentTickets holds the tickets last focused in the reader, most recent first.
	RecentTickets []RecentTicket `toml:"recent_tickets"`
	// Views maps a project gid ("" for My Tasks) to the list view last used there.
	Views map[string]View `toml:"views"`
	// TimeProjects holds recent tracker project IDs by provider ID and Asana project GID.
	// Kept so choices saved by earlier builds can seed the new form field.
	TimeProjects map[string]map[string][]string `toml:"time_projects"`
	// FormChoices holds recent select IDs by form owner, Asana project, and field.
	FormChoices map[string]map[string]map[string][]string `toml:"form_choices"`

	path string
}

// View is a project's list grouping and filter; "" GroupBy is ungrouped.
type View struct {
	GroupBy string `toml:"group_by"`
	Filter  string `toml:"filter"`
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

// SetTaskBranch remembers the git branch typed for a task; "" forgets it.
func (s *State) SetTaskBranch(taskGID, branch string) {
	if branch == "" {
		delete(s.TaskBranches, taskGID)
		return
	}
	s.TaskBranches[taskGID] = branch
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
