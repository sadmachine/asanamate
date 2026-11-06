package safefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReplacesWithModeAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	entries, _ := os.ReadDir(dir)
	if string(data) != "new" || info.Mode().Perm() != 0o600 || len(entries) != 1 {
		t.Fatalf("data = %q, mode = %v, entries = %d", data, info.Mode().Perm(), len(entries))
	}
}

func TestWriteKeepsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "config.toml")
	os.MkdirAll(filepath.Dir(target), 0o700)
	os.WriteFile(target, []byte("old"), 0o600)
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Write(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	data, _ := os.ReadFile(target)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 || string(data) != "new" {
		t.Fatalf("link mode = %v, target = %q, err = %v", info.Mode(), data, err)
	}
	dangling := filepath.Join(dir, "dangling")
	os.Symlink("dotfiles/settings.toml", dangling)
	if err := Write(dangling, []byte("made"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "dotfiles", "settings.toml")); string(data) != "made" {
		t.Fatalf("dangling link target = %q", data)
	}
}

func TestCreateNeverReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "action.toml")
	if err := Create(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Create(path, []byte("two"), 0o600); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "one" {
		t.Fatalf("data = %q", data)
	}
}

func TestBackupsNeverOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	seen := map[string]bool{}
	for _, data := range []string{"a", "b", "c"} {
		backup, err := Backup(path, []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(backup)
		saved, _ := os.ReadFile(backup)
		if seen[backup] || !strings.HasPrefix(backup, path+".") || !strings.HasSuffix(backup, ".bak") ||
			string(saved) != data || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup %q holds %q with mode %v", backup, saved, info.Mode().Perm())
		}
		seen[backup] = true
	}
}

func TestModeFallsBackWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	if Mode(path, 0o600) != 0o600 {
		t.Fatal("missing file must use the fallback")
	}
	os.WriteFile(path, nil, 0o644)
	if Mode(path, 0o600) != 0o644 {
		t.Fatal("existing file must keep its mode")
	}
}
