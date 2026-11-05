package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

type unreadableInput struct{ t *testing.T }

func (r unreadableInput) Read([]byte) (int, error) {
	r.t.Fatal("noninteractive startup read input")
	return 0, io.EOF
}

func TestLoadOrMigrate(t *testing.T) {
	for _, tc := range []struct {
		name, answer                   string
		interactive, updated, conflict bool
	}{
		{name: "yes", answer: "yes\n", interactive: true, updated: true},
		{name: "no", answer: "no\n", interactive: true},
		{name: "not interactive"},
		{name: "conflict", answer: "y\n", interactive: true, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			body := "workspace = \"1\"\naccent_color = \"3\"\n[theme]\nname = \"dark\"\n"
			if tc.conflict {
				body += "[colors]\naccent = \"4\"\n"
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			var in io.Reader = unreadableInput{t}
			if tc.interactive {
				in = strings.NewReader(tc.answer)
			}
			var out strings.Builder
			cfg, err := loadOrMigrate(path, in, &out, tc.interactive)
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			backups, globErr := filepath.Glob(path + ".*.bak")
			if globErr != nil {
				t.Fatal(globErr)
			}
			if tc.updated {
				if err != nil || cfg.Theme.Name != "ansi-dark" || cfg.Colors.Accent.FG != "3" || string(data) == body || len(backups) != 1 {
					t.Fatalf("config = %+v, backups = %v, err = %v", cfg, backups, err)
				}
				backup, err := os.ReadFile(backups[0])
				if err != nil || string(backup) != body {
					t.Fatalf("backup = %q, err = %v", backup, err)
				}
				if _, err := config.Load(path); err != nil {
					t.Fatal(err)
				}
			} else {
				var pending *config.UpdateNeeded
				if tc.conflict {
					if err == nil || !strings.Contains(err.Error(), "both set") {
						t.Fatalf("err = %v", err)
					}
				} else if !errors.As(err, &pending) {
					t.Fatalf("err = %v, want UpdateNeeded", err)
				}
				if string(data) != body || len(backups) != 0 {
					t.Fatal("declined or failed migration changed config")
				}
				if _, err := os.Stat(config.SettingsPath(path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("settings created: %v", err)
				}
			}
			if tc.interactive && (!strings.Contains(out.String(), "accent_color moved to colors.accent") || !strings.Contains(out.String(), "resets comments") || !strings.Contains(out.String(), ".bak")) {
				t.Fatalf("prompt = %q", out.String())
			}
			if !tc.interactive && out.Len() != 0 {
				t.Fatalf("noninteractive prompt = %q", out.String())
			}
		})
	}
}

func TestLoadOrMigrateCurrentConfigDoesNotPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("workspace = \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := loadOrMigrate(path, unreadableInput{t}, &out, true); err != nil || out.Len() != 0 {
		t.Fatalf("out = %q, err = %v", out.String(), err)
	}
}

func TestMigrateDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("workspace = \"1\"\n[list]\nseparator = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrMigrate(path, strings.NewReader("y\n"), io.Discard, true); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(filepath.Join(t.TempDir(), state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	st.LegacyDisplay = state.LegacyDisplay{
		Separator: &yes, HeaderSpacing: &yes, FocusDim: &no,
		SelectionStyle: "bar", ReaderView: "markdown", RefreshInterval: "15s", Theme: "dark",
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	cfg, err := migrateDisplay(path, st)
	if err != nil || !cfg.List.Separator || !cfg.List.Header.Spacing || cfg.Layout.Focus.Dim || cfg.List.Selection.Style != "bar" || cfg.Reader.View != "markdown" || cfg.List.RefreshInterval != "15s" || cfg.Theme.Name != "ansi-dark" {
		t.Fatalf("config = %+v, err = %v", cfg, err)
	}
	loaded, err := state.Load(st.Path())
	if err != nil || loaded.LegacyDisplay != (state.LegacyDisplay{}) {
		t.Fatalf("legacy state = %+v, err = %v", loaded.LegacyDisplay, err)
	}
	if err := config.SaveSetting(path, "list.separator", false); err != nil {
		t.Fatal(err)
	}
	// Once cleared, legacy state must never override a later hand or TUI edit.
	if cfg, err := migrateDisplay(path, loaded); err != nil || cfg.List.Separator {
		t.Fatalf("migration repeated: separator = %v, err = %v", cfg.List.Separator, err)
	}
}

func TestMigrateDisplayFailureKeepsLegacyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("workspace = \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(filepath.Join(t.TempDir(), state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	st.LegacyDisplay.SelectionStyle = "invalid"
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrateDisplay(path, st); err == nil {
		t.Fatal("invalid legacy setting accepted")
	}
	after, err := os.ReadFile(st.Path())
	if err != nil || !bytes.Equal(before, after) || st.LegacyDisplay.SelectionStyle != "invalid" {
		t.Fatalf("legacy state lost: %v", err)
	}
}

func TestHidePreviewFlagParses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := runTUI([]string{"--hide-preview"}); !errors.Is(err, config.ErrNotConfigured) {
		t.Fatalf("hide-preview error = %v", err)
	}
}
