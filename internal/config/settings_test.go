package config

import (
	"bytes"
	"errors"
	"github.com/BurntSushi/toml"
	"os"
	"reflect"
	"strings"
	"testing"
)

func writeSettingsFixture(t *testing.T, configBody, settingsBody string) string {
	t.Helper()
	path := writeFile(t, configBody)
	if err := os.WriteFile(SettingsPath(path), []byte(settingsBody), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSettingsLoadAndValidation(t *testing.T) {
	path := writeSettingsFixture(t, "workspace = \"1\"\n", string(DefaultSettings()))
	cfg, err := Load(path)
	want := Default()
	want.Workspace = "1"
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("defaults = %+v, err = %v", cfg, err)
	}
	for _, tc := range []struct{ body, key string }{
		{"workspace = \"2\"\n", "workspace"},
		{"[list]\nlayout = \"multi\"\n", "list.layout"},
		{"[unknown]\n", "unknown"},
		{"list.selection.style = \"invalid\"\n", "list.selection.style"},
		{"list.header.spacing = 1\n", "list.header.spacing"},
		{"reader.view = \"grid\"\n", "reader.view"},
		{"reader.since.show = \"yes\"\n", "reader.since.show"},
		{"list.refresh_interval = \"0s\"\n", "list.refresh_interval"},
		{"theme.name = \"dark\"\n", `theme.name: "dark" is now "ansi-dark"`},
		{"theme.auto.light = \"light\"\n", `theme.auto.light: "light" is now "ansi-light"`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			path := writeSettingsFixture(t, "workspace = \"1\"\n", tc.body)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), SettingsPath(path)) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSaveSettingPreservesHandEditsAndRejectsInvalidWrites(t *testing.T) {
	path := writeSettingsFixture(t, "workspace = \"1\"\n", "reader.view = \"markdown\"\nlist.separator = true\n")
	if err := SaveSetting(path, "list.selection.style", StyleBar); err != nil {
		t.Fatal(err)
	}
	// Another editor changes a different setting while the app is running.
	f, err := os.OpenFile(SettingsPath(path), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("layout.focus.dim = false\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(errors.Join(writeErr, closeErr))
	}
	if err := SaveSetting(path, "list.header.spacing", true); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(path, "reader.since.show", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Reader.View != ViewMarkdown || !cfg.List.Separator || !cfg.List.Header.Spacing || cfg.List.Selection.Style != StyleBar || cfg.Layout.Focus.Dim || cfg.Reader.Since.Show {
		t.Fatalf("settings = %+v, err = %v", cfg, err)
	}
	before, err := os.ReadFile(SettingsPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(before, []byte(settingsHeader)) {
		t.Fatalf("header missing: %s", before)
	}
	previous := -1
	for _, key := range SettingKeys {
		if index := strings.Index(string(before), key+" = "); index >= 0 {
			if index <= previous {
				t.Fatalf("keys out of order: %s", before)
			}
			previous = index
		}
	}
	info, err := os.Stat(SettingsPath(path))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %v, err = %v", info, err)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"workspace", "2"}, {"list.selection.style", "invalid"},
		{"list.refresh_interval", "0s"}, {"list.separator", "true"},
		{"theme.name", "dark"}, {"theme.name", "missing-theme"},
	} {
		if err := SaveSetting(path, tc.key, tc.value); err == nil {
			t.Fatalf("accepted %s = %v", tc.key, tc.value)
		}
		after, err := os.ReadFile(SettingsPath(path))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed save changed settings: %v", err)
		}
	}
}

func TestLoadReportsAllPendingUpdates(t *testing.T) {
	body := "workspace = \"1\"\naccent_color = \"3\"\n[display]\nsymbols = \"nerd\"\n[agents]\npreset = \"ccmux\"\n"
	path := writeFile(t, body)
	_, err := Load(path)
	var pending *UpdateNeeded
	if !errors.As(err, &pending) || pending.Path != path || len(pending.Changes) != 3 {
		t.Fatalf("pending = %+v, err = %v", pending, err)
	}
	for _, change := range []string{"accent_color moved to colors.accent", "display.symbols moved to symbols", "agents.preset was replaced by agents.command"} {
		if !strings.Contains(err.Error(), change) {
			t.Errorf("missing %q: %v", change, err)
		}
	}
	raw := decode(t, body)
	if !reflect.DeepEqual(PendingUpdates(raw), pending.Changes) || !reflect.DeepEqual(raw, decode(t, body)) {
		t.Fatal("detection changed input or disagrees with Load")
	}
	settings, err := ApplyUpdates(raw)
	if err != nil || len(settings) != 0 || len(PendingUpdates(raw)) != 0 {
		t.Fatalf("migration = %v, settings = %v, err = %v", raw, settings, err)
	}
}

func TestLoadReportsEverySettingAndDefaultViewMove(t *testing.T) {
	defaults := decode(t, string(DefaultSettings()))
	for _, key := range SettingKeys {
		t.Run(key, func(t *testing.T) {
			value, ok := getPath(defaults, key)
			if !ok {
				t.Fatalf("default missing for %s", key)
			}
			raw := map[string]any{"workspace": "1"}
			setPath(raw, key, value)
			changes := PendingUpdates(raw)
			if !strings.Contains(strings.Join(changes, "\n"), key+" moved to settings.toml") {
				t.Fatalf("changes = %v", changes)
			}
			settings, err := ApplyUpdates(raw)
			got, ok := getPath(settings, key)
			if err != nil || !ok || !reflect.DeepEqual(got, value) {
				t.Fatalf("settings = %v, err = %v", settings, err)
			}
			if _, ok := getPath(raw, key); ok {
				t.Fatalf("%s left in config", key)
			}
		})
	}
	for _, mv := range Moves {
		t.Run(mv.From, func(t *testing.T) {
			raw := map[string]any{"workspace": "1"}
			setPath(raw, mv.From, "old")
			var b bytes.Buffer
			// Use a decoded raw map to cover every registered move with LoadData.
			if err := toml.NewEncoder(&b).Encode(raw); err != nil {
				t.Fatal(err)
			}
			_, err := LoadData("config.toml", b.Bytes(), nil)
			var pending *UpdateNeeded
			if !errors.As(err, &pending) || !strings.Contains(err.Error(), mv.From+" moved to "+mv.To) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
