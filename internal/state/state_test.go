package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Repos) != 0 || len(s.RecentProjects) != 0 {
		t.Fatalf("want empty state, got %+v", s)
	}
}

func TestSaveRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	s, _ := Load(path)
	s.LinkRepo("123", "/code/web")
	s.TouchProject("123")
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
