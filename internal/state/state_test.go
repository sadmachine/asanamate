package state

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Repos) != 0 || len(s.RecentProjects) != 0 || s.SavedViews == nil || len(s.SavedViews) != 0 || s.CurrentSavedViews == nil || len(s.RecentSavedViews) != 0 {
		t.Fatalf("want empty state, got %+v", s)
	}
}

func TestSaveRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	s, _ := Load(path)
	s.LinkRepo("123", "/code/web")
	s.TouchProject("123")
	s.SavedViews[`Today's "work"`] = View{Filter: `is:open -tag:blocked`, GroupBy: "section", Sort: config.Sort{By: "due", Direction: "desc"}}
	s.TouchSavedView(`Today's "work"`)
	s.CurrentSavedViews[""] = `Today's "work"`
	s.CurrentSavedViews["123"] = `Today's "work"`
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Repos["123"] != "/code/web" || again.RecentProjects[0] != "123" {
		t.Fatalf("round trip lost data: %+v", again)
	}
	if again.SavedViews[`Today's "work"`] != s.SavedViews[`Today's "work"`] {
		t.Fatalf("round trip lost saved views: %+v", again.SavedViews)
	}
	if !slices.Equal(again.RecentSavedViews, s.RecentSavedViews) || again.CurrentSavedViews[""] != `Today's "work"` || again.CurrentSavedViews["123"] != `Today's "work"` {
		t.Fatalf("round trip lost saved view metadata: %+v", again)
	}
}

func TestTouchSavedViewOrdersAndCaps(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	for i := 0; i < maxRecent+5; i++ {
		s.TouchSavedView(fmt.Sprint(i))
	}
	s.TouchSavedView("7")
	if len(s.RecentSavedViews) != maxRecent || s.RecentSavedViews[0] != "7" || s.RecentSavedViews[1] != fmt.Sprint(maxRecent+4) {
		t.Fatalf("history = %v", s.RecentSavedViews)
	}
	if slices.Contains(s.RecentSavedViews[1:], "7") {
		t.Fatalf("duplicate in history: %v", s.RecentSavedViews)
	}
}

func TestTouchProjectOrdersAndCaps(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	for i := 0; i < maxRecent+5; i++ {
		s.TouchProject(fmt.Sprint(i))
	}
	s.TouchProject("7")
	if len(s.RecentProjects) != maxRecent {
		t.Fatalf("len = %d, want %d", len(s.RecentProjects), maxRecent)
	}
	if s.RecentProjects[0] != "7" || s.RecentProjects[1] != fmt.Sprint(maxRecent+4) {
		t.Fatalf("order = %v", s.RecentProjects)
	}
	for i, gid := range s.RecentProjects[1:] {
		if gid == "7" {
			t.Fatalf("duplicate at %d: %v", i+1, s.RecentProjects)
		}
	}
}

func TestTouchTicketOrdersAndCaps(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	for _, gid := range []string{"a", "b", "c"} {
		s.TouchTicket(RecentTicket{GID: gid, Name: gid})
	}
	s.TouchTicket(RecentTicket{GID: "b", Name: "renamed"})
	if got := s.RecentTickets; len(got) != 3 || got[0] != (RecentTicket{GID: "b", Name: "renamed"}) || got[1].GID != "c" || got[2].GID != "a" {
		t.Fatalf("history = %v", got)
	}
	for i := 0; i < maxRecentTickets+5; i++ {
		s.TouchTicket(RecentTicket{GID: fmt.Sprint(i)})
	}
	if len(s.RecentTickets) != maxRecentTickets || s.RecentTickets[0].GID != fmt.Sprint(maxRecentTickets+4) {
		t.Fatalf("history = %v", s.RecentTickets)
	}
}

func TestUnlinkRepo(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	s.LinkRepo("1", "/code/web")
	s.LinkRepo("2", "/code/api")
	s.UnlinkRepo("1")
	if _, ok := s.Repos["1"]; ok || s.Repos["2"] != "/code/api" {
		t.Fatalf("repos = %v", s.Repos)
	}
}

func TestFormChoiceRecencyAndOwnerIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.TouchFormChoice("time:hrvst", "123", "project_id", "harvest-1")
	s.TouchFormChoice("time:hrvst", "123", "project_id", "harvest-2")
	s.TouchFormChoice("time:hrvst", "123", "project_id", "harvest-1")
	s.TouchFormChoice("action:x", "123", "project_id", "other-1")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.RecentFormChoices("time:hrvst", "123", "project_id")
	if len(got) != 2 || got[0] != "harvest-1" || got[1] != "harvest-2" || s.RecentFormChoices("action:x", "123", "project_id")[0] != "other-1" {
		t.Fatalf("form choices = %+v", s.FormChoices)
	}
}

func TestOldTimeChoiceStillLoads(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	s.TimeProjects["hrvst"] = map[string][]string{"123": {"old"}}
	if got := s.RecentFormChoices("time:hrvst", "123", "project_id"); len(got) != 1 || got[0] != "old" {
		t.Fatalf("legacy choice = %v", got)
	}
}

func TestLoadViewWithoutSort(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`[views.project]
group_by = "section"
filter = "is:open"
[saved_views.Work]
group_by = "due"
filter = "is:open"
`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := s.View("project")
	if !ok || view.GroupBy != "section" || view.Sort != (config.Sort{}) || s.SavedViews["Work"].Sort != (config.Sort{}) {
		t.Fatalf("legacy state = %+v", s)
	}
	if s.CurrentSavedViews == nil || len(s.CurrentSavedViews) != 0 || len(s.RecentSavedViews) != 0 {
		t.Fatalf("legacy saved view metadata = %+v", s)
	}
}
