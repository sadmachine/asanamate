package setup

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/config"
)

// outdated is a released config that config update must rewrite.
const outdated = "workspace = \"1\"\naccent_color = \"2\"\n"

func TestUpdateDeclinedChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(outdated), 0o600)
	o := Options{In: bufio.NewReader(strings.NewReader("n\n")), Out: &strings.Builder{}, ConfigPath: path}
	if err := Update(o, false); err == nil || !strings.Contains(err.Error(), "config unchanged") {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if data, _ := os.ReadFile(path); string(data) != outdated || len(entries) != 1 {
		t.Fatalf("config = %q, files = %d", data, len(entries))
	}
}

func TestUpdateWritesOnlyMissingSettings(t *testing.T) {
	path := runSetup(t)
	settings := config.SettingsPath(path)
	os.Remove(settings)
	before, _ := os.ReadFile(path)
	var out strings.Builder
	if err := Update(Options{ConfigPath: path, Out: &out}, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	backups, _ := filepath.Glob(path + ".*.bak")
	if data, err := os.ReadFile(settings); err != nil || !bytes.Equal(data, config.DefaultSettings()) {
		t.Fatalf("settings = %q, err = %v", data, err)
	}
	if !bytes.Equal(before, after) || len(backups) != 0 || !strings.Contains(out.String(), "Updated "+settings) {
		t.Fatalf("config changed = %v, backups = %v, output:\n%s", !bytes.Equal(before, after), backups, out.String())
	}
}

func TestUpdateRollsBackSettingsWhenConfigWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	for _, hadSettings := range []bool{false, true} {
		dir := t.TempDir()
		// The config links into a read-only directory, so its replacement
		// fails after settings.toml, beside the link, was written.
		locked := filepath.Join(dir, "locked")
		os.Mkdir(locked, 0o700)
		os.WriteFile(filepath.Join(locked, "config.toml"), []byte(outdated), 0o600)
		os.Chmod(locked, 0o500)
		t.Cleanup(func() { os.Chmod(locked, 0o700) })
		path := filepath.Join(dir, "config.toml")
		os.Symlink(filepath.Join(locked, "config.toml"), path)
		settings := config.SettingsPath(path)
		oldSettings := []byte("reader.view = \"markdown\"\n")
		if hadSettings {
			os.WriteFile(settings, oldSettings, 0o600)
		}
		if err := Update(Options{ConfigPath: path, Out: &strings.Builder{}}, true); err == nil {
			t.Fatal("want the config write to fail")
		}
		data, err := os.ReadFile(settings)
		switch {
		case hadSettings && !bytes.Equal(data, oldSettings):
			t.Fatalf("settings not restored: %q, %v", data, err)
		case !hadSettings && !os.IsNotExist(err):
			t.Fatalf("new settings not removed: %q, %v", data, err)
		}
		if data, _ := os.ReadFile(path); string(data) != outdated {
			t.Fatalf("config = %q", data)
		}
	}
}

func TestUpdateKeepsBackupsWithTakenNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(outdated), 0o600)
	// Cover the clock boundary without sleeping.
	now := time.Now()
	for _, when := range []time.Time{now, now.Add(time.Second)} {
		os.WriteFile(path+"."+when.Format("2006-01-02--15-04-05")+".bak", []byte("earlier"), 0o600)
	}
	if err := Update(Options{ConfigPath: path, Out: &strings.Builder{}}, true); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(path + ".*.bak")
	kept := 0
	for _, b := range backups {
		switch data, _ := os.ReadFile(b); string(data) {
		case "earlier":
			kept++
		case outdated:
		default:
			t.Fatalf("backup %s = %q", b, data)
		}
	}
	if kept != 2 || len(backups) != 3 {
		t.Fatalf("backups = %v", backups)
	}
}
