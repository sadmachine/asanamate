package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/sadmachine/asanamate/internal/form"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeActions writes each body to its named file in path's actions dir.
func writeActions(t *testing.T, path string, files map[string]string) {
	t.Helper()
	dir := ActionsDir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeFile(t, `workspace = "123"`)
	writeActions(t, path, map[string]string{"view.toml": `name = "View"
key = "v"
command = "less \"$ASANAMATE_TICKET_MD\""
`})
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dark" || cfg.Images.Mode != "auto" || cfg.Images.Inline || cfg.DefaultFilter != "is:open" || !cfg.ConfirmWrites {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.Actions[0].Mode != ModeForeground {
		t.Fatalf("mode = %q, want %q", cfg.Actions[0].Mode, ModeForeground)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown key":   "workspace = \"1\"\nworkspaec = \"2\"\n",
		"no workspace":  "theme = \"dark\"\n",
		"bad theme":     "workspace = \"1\"\ntheme = \"blue\"\n",
		"bad images":    "workspace = \"1\"\n[images]\nmode = \"sixel\"\n",
		"inline action": "workspace = \"1\"\n[[actions]]\nname = \"a\"\nkey = \"a\"\ncommand = \"true\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeFile(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadRejectsInvalidActions(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown key":   {"a.toml": "name = \"a\"\nkey = \"a\"\ncommand = \"true\"\nkye = \"b\"\n"},
		"bad mode":      {"a.toml": "name = \"a\"\nkey = \"a\"\nmode = \"later\"\ncommand = \"true\"\n"},
		"long key":      {"a.toml": "name = \"a\"\nkey = \"ab\"\ncommand = \"true\"\n"},
		"no command":    {"a.toml": "name = \"a\"\nkey = \"a\"\n"},
		"duplicate key": {"a.toml": "name = \"a\"\nkey = \"x\"\ncommand = \"true\"\n", "b.toml": "name = \"b\"\nkey = \"x\"\ncommand = \"true\"\n"},
		"bad context":   {"a.toml": "name = \"a\"\nkey = \"a\"\ncontext = \"subtask\"\ncommand = \"true\"\n"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, `workspace = "1"`)
			writeActions(t, path, files)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "a.toml") && !strings.Contains(err.Error(), "b.toml") {
				t.Fatalf("err = %v, want an error naming the action file", err)
			}
		})
	}
}

func TestLoadActionsInFileNameOrder(t *testing.T) {
	path := writeFile(t, `workspace = "1"`)
	writeActions(t, path, map[string]string{
		"20-b.toml":      "name = \"b\"\nkey = \"b\"\ncommand = \"true\"\n",
		"10-a.toml":      "name = \"a\"\nkey = \"a\"\ncommand = \"true\"\n",
		"c.toml.example": "not toml",
	})
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Actions) != 2 || cfg.Actions[0].Name != "a" || cfg.Actions[1].Name != "b" {
		t.Fatalf("actions = %+v", cfg.Actions)
	}
}

func TestLoadActionsAllowKeyReuseAcrossContexts(t *testing.T) {
	path := writeFile(t, `workspace = "1"`)
	writeActions(t, path, map[string]string{
		"a.toml": "name = \"a\"\nkey = \"r\"\ncommand = \"true\"\n",
		"b.toml": "name = \"b\"\nkey = \"r\"\ncontext = \"comment\"\ncommand = \"true\"\n",
	})
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Actions) != 2 || cfg.Actions[1].Context != ContextComment {
		t.Fatalf("actions = %+v", cfg.Actions)
	}
}

func TestConfirmWritesFor(t *testing.T) {
	off := false
	cfg := Config{ConfirmWrites: true}
	if !cfg.ConfirmWritesFor(Action{}) {
		t.Fatal("want the global default (true)")
	}
	if cfg.ConfirmWritesFor(Action{ConfirmWrites: &off}) {
		t.Fatal("want the action override (false)")
	}
}

func TestDirsHonorXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	p, _ := Path()
	s, _ := StateDir()
	if p != "/tmp/cfg/asanamate/config.toml" || s != "/tmp/state/asanamate" {
		t.Fatalf("got %s and %s", p, s)
	}
}

func TestDirsFallBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	p, _ := Path()
	s, _ := StateDir()
	if p != "/home/u/.config/asanamate/config.toml" || s != "/home/u/.local/state/asanamate" {
		t.Fatalf("got %s and %s", p, s)
	}
}

func TestToken(t *testing.T) {
	t.Setenv(TokenEnv, "  ")
	if _, err := Token(); err == nil || !strings.Contains(err.Error(), TokenEnv) {
		t.Fatalf("err = %v, want mention of %s", err, TokenEnv)
	}
	t.Setenv(TokenEnv, "abc")
	if tok, err := Token(); err != nil || tok != "abc" {
		t.Fatalf("token = %q, err = %v", tok, err)
	}
}

func TestListConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.List.Layout != LayoutSingle || !slices.Equal(cfg.List.Fields, []string{"section", "due"}) || cfg.Reader.View != ViewCards {
		t.Fatalf("defaults: %+v, err = %v", cfg.List, err)
	}
	if cfg.AccentColor != "4" || cfg.List.Header != (Header{Style: StyleRule}) || cfg.List.Pinned != (Pinned{Color: "208"}) || cfg.List.Selection != (Selection{Style: StyleMarker}) {
		t.Fatalf("style defaults: %+v", cfg.List)
	}
	for _, color := range []string{"0", "255", "#abc", "#A1b2C3"} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n[list.header]\ncolor = \""+color+"\"\n")); err != nil {
			t.Errorf("header.color %q: %v", color, err)
		}
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[list]\nlayout = \"multi\"\nfields = [\"status\", \"Branch Name\", \"due\"]\n"))
	if err != nil || cfg.List.Layout != LayoutMulti || len(cfg.List.Fields) != 3 {
		t.Fatalf("custom: %+v, err = %v", cfg.List, err)
	}
	for name, body := range map[string]string{
		"bad layout":    "[list]\nlayout = \"grid\"\n",
		"bad view":      "[reader]\nview = \"grid\"\n",
		"neg width":     "[reader]\nmax_text_width = -1\n",
		"title field":   "[list]\nfields = [\"Title\"]\n",
		"empty field":   "[list]\nfields = [\" \"]\n",
		"title group":   "[list]\ngroup_by = \"title\"\n",
		"bad header":    "[list.header]\nstyle = \"box\"\n",
		"int spacing":   "[list.header]\nspacing = 1\n",
		"bad color":     "[list.header]\ncolor = \"blue\"\n",
		"color range":   "[list.header]\ncolor = \"256\"\n",
		"bad hex":       "[list.header]\ncolor = \"#12345g\"\n",
		"bad pinned":    "[list.pinned]\ncolor = \"orange\"\n",
		"bad selection": "[list.selection]\nstyle = \"rule\"\n",
		"bad marker":    "[list.selection]\ncolor = \"red\"\n",
		"empty accent":  "accent_color = \"\"\n",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestRefreshIntervalConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.List.RefreshInterval != "30s" {
		t.Fatalf("default interval = %q, err = %v", cfg.List.RefreshInterval, err)
	}
	for _, value := range []string{"1s", "15s", "1m", "1m30s"} {
		t.Run(value, func(t *testing.T) {
			cfg, err := Load(writeFile(t, "workspace = \"1\"\n[list]\nrefresh_interval = \""+value+"\"\n"))
			if err != nil || cfg.List.RefreshInterval != value {
				t.Fatalf("interval = %q, err = %v", cfg.List.RefreshInterval, err)
			}
		})
	}
	for _, value := range []string{"", "15", "0s", "-1s", "500ms", "999999999999999999999h"} {
		t.Run("invalid "+value, func(t *testing.T) {
			_, err := Load(writeFile(t, "workspace = \"1\"\n[list]\nrefresh_interval = \""+value+"\"\n"))
			if err == nil || !strings.Contains(err.Error(), "list.refresh_interval") {
				t.Fatalf("want dotted config key in error, got %v", err)
			}
		})
	}
}

func TestAgentsOffByDefault(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.AgentsEnabled() || cfg.BranchField != "" {
		t.Fatalf("defaults: %+v, err = %v", cfg, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\nbranch_field = \"Branch Name\"\n[agents]\ncommand = \"ccmux show --json\"\n"))
	if err != nil || !cfg.AgentsEnabled() || cfg.BranchField != "Branch Name" {
		t.Fatalf("enabled: %+v, err = %v", cfg, err)
	}
}

func TestTimeTrackingConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.TimeTrackingEnabled() {
		t.Fatalf("default time tracking = %+v, err = %v", cfg.TimeTracking, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate time-provider hrvst\"\n"))
	if err != nil || !cfg.TimeTrackingEnabled() {
		t.Fatalf("configured time tracking = %+v, err = %v", cfg.TimeTracking, err)
	}
	for _, body := range []string{"id = \"hrvst\"", "command = \"provider\"", "id = \"hrvst\"\ncommand = \"  \""} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n[time_tracking]\n"+body+"\n")); err == nil || !strings.Contains(err.Error(), "time_tracking") {
			t.Fatalf("config %q error = %v", body, err)
		}
	}
}

func TestActionFormConfig(t *testing.T) {
	path := writeFile(t, `workspace = "1"`)
	body := `name = "Deploy"
key = "d"
command = "deploy"
[[form.fields]]
id = "target"
label = "Target"
type = "select"
remember = true
[[form.fields.options]]
id = "prod"
name = "Production"
`
	writeActions(t, path, map[string]string{"deploy.toml": body})
	cfg, err := Load(path)
	if err != nil || len(cfg.Actions) != 1 || len(cfg.Actions[0].Form.Fields) != 1 || cfg.Actions[0].Form.Fields[0].Options[0].ID != "prod" {
		t.Fatalf("action form = %+v, err = %v", cfg.Actions, err)
	}
	writeActions(t, path, map[string]string{"deploy.toml": strings.Replace(body, `type = "select"`, `type = "unknown"`, 1)})
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `action "Deploy" form`) {
		t.Fatalf("invalid action form error = %v", err)
	}
}

func TestAgentsPresetAndValidation(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n[agents]\npreset = \"ccmux\"\n[agents.states]\nworking = [\"busy\"]\n[agents.symbols]\nwaiting = \"!!\"\n"))
	if err != nil || !cfg.AgentsEnabled() || cfg.Agents.States["working"][0] != "busy" || cfg.Agents.Symbols["waiting"] != "!!" {
		t.Fatalf("cfg = %+v, err = %v", cfg.Agents, err)
	}
	for name, body := range map[string]string{
		"unknown preset":   "[agents]\npreset = \"tmux\"\n",
		"preset + command": "[agents]\npreset = \"ccmux\"\ncommand = \"x\"\n",
		"bad state":        "[agents]\ncommand = \"x\"\n[agents.states]\nsleeping = [\"z\"]\n",
		"bad symbol":       "[agents]\ncommand = \"x\"\n[agents.symbols]\nsleeping = \"z\"\n",
		"bad symbols set":  "symbols = \"emoji\"\n",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSymbolSet(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	cases := []struct {
		set  string
		vars map[string]string
		want string
	}{
		{"", map[string]string{"LANG": "en_US.UTF-8"}, "unicode"},
		{"", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, "ascii"},
		{"", map[string]string{"LC_CTYPE": "en_US.utf8"}, "unicode"},
		{"", nil, "ascii"},
		{"nerd", nil, "nerd"},
	}
	for _, c := range cases {
		if got := (Config{Symbols: c.set}).SymbolSet(env(c.vars)); got != c.want {
			t.Errorf("SymbolSet(%q, %v) = %q, want %q", c.set, c.vars, got, c.want)
		}
	}
}

func TestSaveActionFileRoundTripAndBackup(t *testing.T) {
	dir := t.TempDir()
	no := false
	original := ActionFile{Name: "20-deploy.toml", Action: Action{
		Name: "Deploy", Key: "D", Mode: ModeBackground, Repo: true, Agent: true,
		Context: ContextComment, Command: "printf '%s\\n' \"$ASANAMATE_INPUT_FILE\"\n# next line\n", Input: "Notes", ConfirmWrites: &no,
		Form: form.Spec{Fields: []form.Field{
			{ID: "target", Label: "Target", Type: form.Select, Remember: true, Options: []form.Option{{ID: "prod", Name: "Production"}}},
			{ID: "hours", Label: "Hours", Type: form.Hours},
		}},
	}}
	actions, err := SaveActionFile(dir, original)
	if err != nil || len(actions) != 1 || !reflect.DeepEqual(actions[0], original.Action) {
		t.Fatalf("round trip: %+v, %v", actions, err)
	}
	files, err := LoadActionFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	edited := files[0]
	edited.Action.Name = "Deploy edited"
	edited.Action.ConfirmWrites = nil
	if _, err := SaveActionFile(dir, edited); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(data, edited.Original) {
		t.Fatalf("backup: %q, %v", data, err)
	}
	files, err = LoadActionFiles(dir)
	if err != nil || files[0].Action.Name != "Deploy edited" || files[0].Action.ConfirmWrites != nil {
		t.Fatalf("edit: %+v, %v", files, err)
	}
	info, err := os.Stat(filepath.Join(dir, edited.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %v, %v", info, err)
	}
}

func TestSaveActionFileRejectsUnsafeWrites(t *testing.T) {
	dir := t.TempDir()
	file := ActionFile{Name: "20-existing.toml", Action: Action{Name: "Existing", Key: "e", Mode: ModeForeground, Command: "true"}}
	if _, err := SaveActionFile(dir, file); err != nil {
		t.Fatal(err)
	}
	files, err := LoadActionFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := files[0].Original
	for _, name := range []string{"../outside.toml", "nested/file.toml", "wrong.example", ".toml", ""} {
		bad := file
		bad.Name = name
		if _, err := SaveActionFile(dir, bad); err == nil {
			t.Fatalf("accepted filename %q", name)
		}
	}
	if _, err := SaveActionFile(dir, file); err == nil {
		t.Fatal("overwrote existing file")
	}
	duplicate := file
	duplicate.Name = "10-duplicate.toml"
	if _, err := SaveActionFile(dir, duplicate); err == nil {
		t.Fatal("accepted duplicate key")
	}
	duplicate.Action.Context = ContextComment
	actions, err := SaveActionFile(dir, duplicate)
	if err != nil || len(actions) != 2 || actions[0].Context != ContextComment {
		t.Fatalf("cross-context key / order: %+v, %v", actions, err)
	}
	invalid := file
	invalid.Name = "bad.toml"
	invalid.Action.Form.Fields = []form.Field{{ID: "hours", Label: "Hours", Type: form.Hours, Remember: true}}
	if _, err := SaveActionFile(dir, invalid); err == nil {
		t.Fatal("accepted invalid form")
	}
	edited := files[0]
	edited.Action.Name = "Changed"
	changed := append(append([]byte{}, original...), []byte("\n# External edit\n")...)
	if err := os.WriteFile(filepath.Join(dir, file.Name), changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveActionFile(dir, edited); err == nil {
		t.Fatal("overwrote external edit")
	}
	data, err := os.ReadFile(filepath.Join(dir, file.Name))
	if err != nil || !bytes.Equal(data, changed) {
		t.Fatal("failed save changed original")
	}
	if err := os.Remove(filepath.Join(dir, file.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, duplicate.Name), filepath.Join(dir, file.Name)); err != nil {
		t.Fatal(err)
	}
	edited.Action.Key = "x"
	if _, err := SaveActionFile(dir, edited); err == nil {
		t.Fatal("replaced symlink")
	}
}

func TestSaveActionFileCoordinatesBuilderSessions(t *testing.T) {
	dir := t.TempDir()
	file := ActionFile{Name: "action.toml", Action: Action{Name: "Action", Key: "a", Mode: ModeForeground, Command: "true"}}
	lock, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveActionFile(dir, file); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("concurrent save: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveActionFile(dir, file); err != nil {
		t.Fatal(err)
	}
	files, err := LoadActionFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, second := files[0], files[0]
	first.Action.Name = "First session"
	second.Action.Name = "Second session"
	if _, err := SaveActionFile(dir, first); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveActionFile(dir, second); err == nil || !strings.Contains(err.Error(), "changed outside") {
		t.Fatalf("stale session save: %v", err)
	}
	files, err = LoadActionFiles(dir)
	if err != nil || files[0].Action.Name != "First session" {
		t.Fatalf("first save lost: %+v, %v", files, err)
	}
}

func TestListSortConfig(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       Sort
		bad        string
	}{
		{name: "default", want: Sort{Direction: "asc"}},
		{name: "due", body: "[list.sort]\nby = \"due\"\n", want: Sort{By: "due", Direction: "asc"}},
		{name: "custom descending", body: "[list.sort]\nby = \"Priority\"\ndirection = \"desc\"\n", want: Sort{By: "Priority", Direction: "desc"}},
		{name: "bad direction", body: "[list.sort]\ndirection = \"sideways\"\n", bad: "list.sort.direction"},
		{name: "bad name", body: "[list.sort]\nby = \"due\\n\"\n", bad: "list.sort.by"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeFile(t, "workspace = \"1\"\n"+tc.body))
			if tc.bad != "" {
				if err == nil || !strings.Contains(err.Error(), tc.bad) {
					t.Fatalf("error = %v, want %s", err, tc.bad)
				}
			} else if err != nil || cfg.List.Sort != tc.want {
				t.Fatalf("sort = %v, err = %v, want %v", cfg.List.Sort, err, tc.want)
			}
		})
	}
}
