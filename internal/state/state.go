// Package state stores data asanamate learns while running: repo links, recent
// projects, and each project's last list view.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/sadmachine/asanamate/internal/asana"
)

// FileName is the state file's name inside the state directory.
const FileName = "state.toml"

const maxRecent = 20

// State is the contents of state.toml.
type State struct {
	// Repos maps an Asana project gid to a local git repository path.
	Repos map[string]string `toml:"repos"`
	// RecentProjects holds project gids, most recently opened first.
	RecentProjects []string `toml:"recent_projects"`
	// Views maps a project gid ("" for My Tasks) to the list view last used there.
	Views map[string]View `toml:"views"`

	path string
}

// View is a project's list grouping and filter; "" GroupBy is ungrouped.
type View struct {
	GroupBy string `toml:"group_by"`
	Filter  string `toml:"filter"`
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
	if s.Views == nil {
		s.Views = map[string]View{}
	}
	return s, nil
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

// LinkRepo remembers the repository used for a project.
func (s *State) LinkRepo(projectGID, path string) {
	s.Repos[projectGID] = path
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

// LinkedRepos returns the repos linked to t's projects, in membership order.
func (s *State) LinkedRepos(t asana.Task) []string {
	var repos []string
	for _, m := range t.Memberships {
		if path, ok := s.Repos[m.Project.GID]; ok {
			repos = append(repos, path)
		}
	}
	return repos
}
