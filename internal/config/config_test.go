package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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
	if cfg.Theme != (Theme{Name: ThemeAuto, Auto: AutoThemes{Dark: "ansi-dark", Light: "ansi-light"}}) || cfg.Images.Mode != "auto" || cfg.Images.Inline || cfg.List.DefaultView.Filter != "is:open" || !cfg.ConfirmWrites {
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

func TestConversationsConfig(t *testing.T) {
	if cfg := Default(); cfg.ConversationsEnabled() || cfg.Conversations != (Conversations{}) {
		t.Fatal("conversations enabled by default")
	}
	for _, body := range []string{"", "[conversations]\ncommand = \"\"\nopen = \"\"", "[conversations]\ncommand = \"fixture list\"\nopen = \"fixture open\""} {
		cfg, err := Load(writeFile(t, "workspace = \"1\"\n"+body))
		if err != nil || cfg.ConversationsEnabled() != strings.Contains(body, "fixture") {
			t.Fatalf("config = %+v, error = %v", cfg.Conversations, err)
		}
	}
	for body, want := range map[string]string{
		"command = \"list\"":                "conversations.open",
		"open = \"open\"":                   "conversations.command",
		"command = \"  \"\nopen = \"open\"": "conversations.command",
		"command = \"list\"\nopen = \"  \"": "conversations.open",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n[conversations]\n"+body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %s", err, want)
		}
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown key":   "workspace = \"1\"\nworkspaec = \"2\"\n",
		"no workspace":  "[theme]\nname = \"dark\"\n",
		"bad theme":     "workspace = \"1\"\ntheme = \"blue\"\n",
		"bad images":    "workspace = \"1\"\n[images]\nmode = \"sixel\"\n",
		"relative root": "workspace = \"1\"\n[repo_source]\nroot = \"code\"\n",
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

func TestRepoSourceRootAcceptsHomeAndAbsolutePaths(t *testing.T) {
	for _, root := range []string{"", "~", "~/code", "/srv/code"} {
		cfg, err := Load(writeFile(t, "workspace = \"1\"\n[repo_source]\nroot = \""+root+"\"\n"))
		if err != nil || cfg.RepoSource.Root != root {
			t.Errorf("root %q: cfg = %+v, err = %v", root, cfg.RepoSource, err)
		}
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
	if !reflect.DeepEqual(cfg.Colors, Colors{}) || cfg.List.Header != (Header{Style: StyleRule}) || cfg.List.Selection != (Selection{Style: StyleMarker}) {
		t.Fatalf("style defaults: %+v", cfg.List)
	}
	if !reflect.DeepEqual(cfg.Feed, Feed{Expand: ExpandAuto, Height: 12, RefreshInterval: "1m", Commands: []string{}}) {
		t.Fatalf("feed defaults: %+v", cfg.Feed)
	}
	if cfg.Reader.Comments != (Comments{Latest: 3, PreviewLines: 2}) {
		t.Fatalf("comment defaults: %+v", cfg.Reader.Comments)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[reader.comments]\nlatest = 0\npreview_lines = 5\n"))
	if err != nil || cfg.Reader.Comments != (Comments{Latest: 0, PreviewLines: 5}) {
		t.Fatalf("comments: %+v, err = %v", cfg.Reader.Comments, err)
	}
	for _, color := range []string{"0", "255", "#abc", "#A1b2C3"} {
		cfg, err := Load(writeFile(t, "workspace = \"1\"\n[colors]\nviewing = \""+color+"\"\n"))
		if err != nil || cfg.Colors.Viewing.FG != color {
			t.Errorf("colors.viewing %q: got %q, err = %v", color, cfg.Colors.Viewing.FG, err)
		}
	}
	for _, color := range []string{"magenta", "-1", "256", "#12345g"} {
		_, err := Load(writeFile(t, "workspace = \"1\"\n[colors]\nviewing = \""+color+"\"\n"))
		if err == nil || !strings.Contains(err.Error(), "colors.viewing") {
			t.Errorf("colors.viewing %q: expected error naming colors.viewing, got %v", color, err)
		}
	}
	if cfg.List.New.Section {
		t.Fatal("list.new.section must default to false")
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[list.new]\nsection = true\n"))
	if err != nil || !cfg.List.New.Section {
		t.Fatalf("list.new.section: %+v, err = %v", cfg.List.New, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[list]\nlayout = \"multi\"\nfields = [\"status\", \"Branch Name\", \"due\"]\n"))
	if err != nil || cfg.List.Layout != LayoutMulti || len(cfg.List.Fields) != 3 {
		t.Fatalf("custom: %+v, err = %v", cfg.List, err)
	}
	for name, body := range map[string]string{
		"bad layout":  "[list]\nlayout = \"grid\"\n",
		"neg width":   "[reader]\nmax_text_width = -1\n",
		"neg latest":  "[reader.comments]\nlatest = -1\n",
		"neg preview": "[reader.comments]\npreview_lines = -1\n",
		"title field": "[list]\nfields = [\"Title\"]\n",
		"empty field": "[list]\nfields = [\" \"]\n",
		"title group": "[list.default_view]\ngroup_by = \"title\"\n",
		"bad header":  "[list.header]\nstyle = \"box\"\n",
		"int new":     "[list.new]\nsection = 1\n",
		"bad accent":  "[colors]\naccent = \"blue\"\n",
		"bad more":    "[colors]\nmore = { bg = \"cyan\" }\n",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestActivityRefreshIntervalConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if d, _ := cfg.Activity.Interval(); err != nil || cfg.Activity.RefreshInterval != "2m" || d != 2*time.Minute {
		t.Fatalf("default interval = %q, err = %v", cfg.Activity.RefreshInterval, err)
	}
	for value, want := range map[string]time.Duration{"0": 0, "0s": 0, "1s": time.Second, "5m": 5 * time.Minute} {
		t.Run(value, func(t *testing.T) {
			cfg, err := Load(writeFile(t, "workspace = \"1\"\n[activity]\nrefresh_interval = \""+value+"\"\n"))
			if d, _ := cfg.Activity.Interval(); err != nil || d != want {
				t.Fatalf("interval = %v, err = %v", d, err)
			}
		})
	}
	for _, value := range []string{"", "2", "-1s", "500ms"} {
		t.Run("invalid "+value, func(t *testing.T) {
			_, err := Load(writeFile(t, "workspace = \"1\"\n[activity]\nrefresh_interval = \""+value+"\"\n"))
			if err == nil || !strings.Contains(err.Error(), "activity.refresh_interval") {
				t.Fatalf("want dotted config key in error, got %v", err)
			}
		})
	}
}

func TestRefreshIntervalConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.List.RefreshInterval != "30s" {
		t.Fatalf("default interval = %q, err = %v", cfg.List.RefreshInterval, err)
	}
	for _, value := range []string{"1s", "15s", "1m", "1m30s"} {
		t.Run(value, func(t *testing.T) {
			cfg, err := Load(writeSettingsFixture(t, "workspace = \"1\"\n", "[list]\nrefresh_interval = \""+value+"\"\n"))
			if err != nil || cfg.List.RefreshInterval != value {
				t.Fatalf("interval = %q, err = %v", cfg.List.RefreshInterval, err)
			}
		})
	}
	for _, value := range []string{"", "15", "0s", "-1s", "500ms", "999999999999999999999h"} {
		t.Run("invalid "+value, func(t *testing.T) {
			_, err := Load(writeSettingsFixture(t, "workspace = \"1\"\n", "[list]\nrefresh_interval = \""+value+"\"\n"))
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
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate-hrvst --task-id 456\"\n"))
	if err != nil || !cfg.TimeTrackingEnabled() {
		t.Fatalf("configured time tracking = %+v, err = %v", cfg.TimeTracking, err)
	}
	for _, body := range []string{"id = \"hrvst\"", "command = \"provider\"", "id = \"hrvst\"\ncommand = \"  \""} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n[time_tracking]\n"+body+"\n")); err == nil || !strings.Contains(err.Error(), "time_tracking") {
			t.Fatalf("config %q error = %v", body, err)
		}
	}
}

func TestProjectLinksConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.ProjectLinksEnabled() {
		t.Fatalf("default project links = %+v, err = %v", cfg.ProjectLinks, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[project_links]\ncommand = \"asanamate links-provider hub\"\n"))
	if err != nil || !cfg.ProjectLinksEnabled() || cfg.ProjectLinks.Command != "asanamate links-provider hub" {
		t.Fatalf("configured project links = %+v, err = %v", cfg.ProjectLinks, err)
	}
	if _, err := Load(writeFile(t, "workspace = \"1\"\n[project_links]\ncommand = \"  \"\n")); err == nil || !strings.Contains(err.Error(), "project_links.command") {
		t.Fatalf("blank command error = %v", err)
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

func TestAgentsValidation(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n[agents]\ncommand = \"asanamate-ccmux\"\nfocus = \"switch\"\n[agents.states]\nworking = [\"busy\"]\n[agents.symbols]\nwaiting = \"!!\"\n"))
	if err != nil || !cfg.AgentsEnabled() || cfg.Agents.Focus != "switch" || cfg.Agents.States["working"][0] != "busy" || cfg.Agents.Symbols["waiting"] != "!!" {
		t.Fatalf("cfg = %+v, err = %v", cfg.Agents, err)
	}
	// The removed preset names its replacement, like the moved keys.
	_, err = Load(writeFile(t, "workspace = \"1\"\n[agents]\npreset = \"ccmux\"\n"))
	if err == nil || !strings.Contains(err.Error(), "agents.preset was replaced by agents.command") || !strings.Contains(err.Error(), "asanamate config update") {
		t.Fatalf("preset err = %v", err)
	}
	_, err = Load(writeFile(t, "workspace = \"1\"\n[agents]\nfocus = \"switch\"\n"))
	if err == nil || !strings.Contains(err.Error(), "agents.command is required when agents.focus is set") {
		t.Fatalf("focus without command err = %v", err)
	}
	for name, body := range map[string]string{
		"bad state":       "[agents]\ncommand = \"x\"\n[agents.states]\nsleeping = [\"z\"]\n",
		"bad symbol":      "[agents]\ncommand = \"x\"\n[agents.symbols]\nsleeping = \"z\"\n",
		"bad symbols set": "symbols = \"emoji\"\n",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAgentFocusConfig(t *testing.T) {
	if cfg := Default(); cfg.Agents.Focus != "" {
		t.Fatal("agent focus enabled by default")
	}
	for _, body := range []string{
		"",
		"[agents]\nfocus = \"\"\n",
		"[agents]\ncommand = \"fixture agents\"\nfocus = 'fixture focus \"$ASANAMATE_AGENT_TARGET\"'\n",
	} {
		if cfg, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err != nil || strings.Contains(body, "fixture focus") && cfg.Agents.Focus != `fixture focus "$ASANAMATE_AGENT_TARGET"` {
			t.Fatalf("focus config rejected: %v", err)
		}
	}
	for _, command := range []string{"", "  "} {
		body := "workspace = \"1\"\n[agents]\ncommand = " + strconv.Quote(command) + "\nfocus = \"fixture focus\"\n"
		if _, err := Load(writeFile(t, body)); err == nil || !strings.Contains(err.Error(), "agents.command is required when agents.focus is set") {
			t.Fatalf("focus without source: %v", err)
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
		Form: ActionForm{Spec: form.Spec{Fields: []form.Field{
			{ID: "target", Label: "Target", Type: form.Select, Remember: true, Options: []form.Option{{ID: "prod", Name: "Production"}}},
			{ID: "hours", Label: "Hours", Type: form.Hours},
		}}},
	}}
	actions, err := SaveActionFile(dir, original)
	loaded := original.Action
	loaded.File = "20-deploy"
	if err != nil || len(actions) != 1 || !reflect.DeepEqual(actions[0], loaded) {
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

func TestActionDynamicFormRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.toml")
	const source = `name = "Resume"
key = "r"
command = "resume-agent"
[form]
command = "resume-agent form"
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := LoadActionFiles(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("load dynamic action form: %+v, %v", files, err)
	}
	if _, err := SaveActionFile(dir, files[0]); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `command = "resume-agent form"`) {
		t.Fatalf("form command lost on save: %s, %v", data, err)
	}
}

func TestActionDynamicFormRejectsConflictingFields(t *testing.T) {
	for _, body := range []string{
		`[form]
command = "   "`,
		`[form]
command = "load-options"
[[form.fields]]
id = "target"
label = "Target"
type = "string"`,
	} {
		dir := t.TempDir()
		data := "name = 'Run'\nkey = 'r'\ncommand = 'run'\n" + body
		if err := os.WriteFile(filepath.Join(dir, "run.toml"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadActionFiles(dir); err == nil || !strings.Contains(err.Error(), "form.command") {
			t.Fatalf("expected form.command validation error, got %v", err)
		}
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
		{name: "due", body: "[list.default_view.sort]\nby = \"due\"\n", want: Sort{By: "due", Direction: "asc"}},
		{name: "custom descending", body: "[list.default_view.sort]\nby = \"Priority\"\ndirection = \"desc\"\n", want: Sort{By: "Priority", Direction: "desc"}},
		{name: "bad direction", body: "[list.default_view.sort]\ndirection = \"sideways\"\n", bad: "list.default_view.sort.direction"},
		{name: "bad name", body: "[list.default_view.sort]\nby = \"due\\n\"\n", bad: "list.default_view.sort.by"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeFile(t, "workspace = \"1\"\n"+tc.body))
			if tc.bad != "" {
				if err == nil || !strings.Contains(err.Error(), tc.bad) {
					t.Fatalf("error = %v, want %s", err, tc.bad)
				}
			} else if err != nil || cfg.List.DefaultView.Sort != tc.want {
				t.Fatalf("sort = %v, err = %v, want %v", cfg.List.DefaultView.Sort, err, tc.want)
			}
		})
	}
}

func TestKeysConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n[keys.main]\nquit = [\"Q\"]\n[keys.picker]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keymap.Name("main", "Q") != "quit" || cfg.Keymap.Name("main", "q") != "" {
		t.Fatalf("keymap main = %v", cfg.Keymap["main"])
	}
	if len(cfg.Keys) != 1 {
		t.Fatalf("empty scopes must be dropped: %v", cfg.Keys)
	}
	if cfg.Keymap.Name("input", "ctrl+d") != "save_draft" {
		t.Fatal("comment draft key missing from defaults")
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[keys.input]\nsave_draft = [\"alt+s\"]\n"))
	if err != nil || cfg.Keymap.Name("input", "alt+s") != "save_draft" || cfg.Keymap.Name("input", "ctrl+d") != "" {
		t.Fatalf("draft key override = %v, %v", cfg.Keymap, err)
	}

	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[keys.main]\nactivity = [\"w\"]\n[keys.activity]\nautomated = [\"A\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keymap.Name("main", "w") != "activity" || cfg.Keymap.Name("activity", "A") != "automated" || cfg.Keymap.Name("activity", "!") != "" {
		t.Fatalf("activity keymap = %v", cfg.Keymap)
	}
	_, err = Load(writeFile(t, "workspace = \"1\"\n[keys.main]\ndwon = [\"j\"]\n"))
	if err == nil || !strings.Contains(err.Error(), "keys.main.dwon: unknown binding") {
		t.Fatalf("err = %v", err)
	}
}

func TestPickerConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.Picker.TypeFirst {
		t.Fatalf("default: %+v, err = %v", cfg.Picker, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[picker]\ntype_first = true\n"))
	if err != nil || !cfg.Picker.TypeFirst {
		t.Fatalf("set: %+v, err = %v", cfg.Picker, err)
	}
}

func TestMouseConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || !cfg.Mouse.Enabled {
		t.Fatalf("default: %+v, err = %v", cfg.Mouse, err)
	}
	cfg, err = Load(writeFile(t, "workspace = \"1\"\n[mouse]\nenabled = false\n"))
	if err != nil || cfg.Mouse.Enabled {
		t.Fatalf("set: %+v, err = %v", cfg.Mouse, err)
	}
}

func TestLayoutConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n"))
	if err != nil || cfg.Layout != (Layout{Reader: true, Focus: Focus{Dim: true, Resize: true}}) {
		t.Fatalf("default: %+v, err = %v", cfg.Layout, err)
	}
	cfg, err = Load(writeSettingsFixture(t, "workspace = \"1\"\n[layout]\nreader = false\nlist_width = 60\n[layout.focus]\nresize = false\n", "layout.focus.dim = false\n"))
	if err != nil || cfg.Layout != (Layout{ListWidth: 60}) {
		t.Fatalf("set: %+v, err = %v", cfg.Layout, err)
	}
	_, err = Load(writeFile(t, "workspace = \"1\"\n[layout]\nlist_width = -1\n"))
	if err == nil || !strings.Contains(err.Error(), "layout.list_width") {
		t.Fatalf("negative list_width: err = %v", err)
	}
}

func TestFeedConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, "workspace = \"1\"\n[feed]\nexpand = \"collapsed\"\nheight = 5\nrefresh_interval = \"0\"\ncommands = [\"asanamate-hrvst\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := cfg.Feed.Interval(); cfg.Feed.Expand != ExpandCollapsed || cfg.Feed.Height != 5 || d != 0 || !slices.Equal(cfg.Feed.Commands, []string{"asanamate-hrvst"}) {
		t.Fatalf("feed = %+v", cfg.Feed)
	}
	for body, key := range map[string]string{
		"[feed]\nexpand = \"open\"\n":           "feed.expand",
		"[feed]\nheight = 2\n":                  "feed.height",
		"[feed]\nrefresh_interval = \"10ms\"\n": "feed.refresh_interval",
		"[feed]\ncommands = [\" \"]\n":          "feed.commands",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%q: error %v, want one naming %s", body, err, key)
		}
	}
}

const hookTimeTracking = "[time_tracking]\nid = \"hrvst\"\ncommand = \"p\"\n"

func TestHooksLoad(t *testing.T) {
	path := writeFile(t, "workspace = \"1\"\n"+hookTimeTracking+`
[[hooks]]
on = "ticket_open"
when = "is:open branch:none"
once = true
run = "edit:branch"

[[hooks]]
on = "complete"
run = "log_time"

[[hooks]]
on = "new_activity"
run = "action:tmux-open"
`)
	writeActions(t, path, map[string]string{"tmux-open.toml": "name = \"Open\"\nkey = \"o\"\nmode = \"background\"\ncommand = \"true\"\n"})
	cfg, err := Load(path)
	want := []Hook{
		{On: HookTicketOpen, When: "is:open branch:none", Once: true, Run: "edit:branch"},
		{On: HookComplete, Run: HookLogTime},
		{On: HookNewActivity, Run: "action:tmux-open"},
	}
	if err != nil || !reflect.DeepEqual(cfg.Hooks, want) {
		t.Fatalf("hooks = %+v, err = %v", cfg.Hooks, err)
	}
	if a, ok := cfg.HookAction(cfg.Hooks[2]); !ok || a.Name != "Open" || a.File != "tmux-open" {
		t.Fatalf("hook action = %+v, %v", a, ok)
	}
	if kind, name := cfg.Hooks[0].Target(); kind != "edit" || name != "branch" {
		t.Fatalf("target = %q, %q", kind, name)
	}
}

func TestHooksValidate(t *testing.T) {
	background := "name = \"Bg\"\nkey = \"b\"\nmode = \"background\"\ncommand = \"true\"\n"
	foreground := "name = \"Fg\"\nkey = \"f\"\ncommand = \"true\"\n"
	withForm := strings.Replace(background, `key = "b"`, `key = "m"`, 1) + "[form]\ncommand = \"form-cmd\"\n"
	for _, tc := range []struct {
		name, hooks, want string
	}{
		{"unknown event", "[[hooks]]\non = \"complete\"\nrun = \"log_time\"\n[[hooks]]\non = \"refresh\"\nrun = \"log_time\"\n", `hooks[2].on: "refresh" must be one of ticket_open, assign, complete, new_activity`},
		{"unknown target", "[[hooks]]\non = \"complete\"\nrun = \"edit:complete\"\n", `hooks[1].run: "edit:complete" must be`},
		{"missing run", "[[hooks]]\non = \"complete\"\n", `hooks[1].run: "" must be`},
		{"log time without tracking", "[[hooks]]\non = \"complete\"\nrun = \"log_time\"\n", "hooks[1].run: log_time needs [time_tracking]"},
		{"missing action", "[[hooks]]\non = \"assign\"\nrun = \"action:nope\"\n", "hooks[1].run: no action file actions/nope.toml"},
		{"activity prompt", "[[hooks]]\non = \"new_activity\"\nrun = \"edit:branch\"\n", "hooks[1].run: new_activity hooks run background actions only"},
		{"activity foreground action", "[[hooks]]\non = \"new_activity\"\nrun = \"action:fg\"\n", "hooks[1].run: actions/fg.toml must be a background action"},
		{"activity form action", "[[hooks]]\non = \"new_activity\"\nrun = \"action:form\"\n", "hooks[1].run: actions/form.toml must be a background action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracking := hookTimeTracking
			if tc.name == "log time without tracking" {
				tracking = ""
			}
			path := writeFile(t, "workspace = \"1\"\n"+tracking+tc.hooks)
			writeActions(t, path, map[string]string{"bg.toml": background, "fg.toml": foreground, "form.toml": withForm})
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	path := writeFile(t, "workspace = \"1\"\n[[hooks]]\non = \"ticket_open\"\nrun = \"action:fg\"\n")
	writeActions(t, path, map[string]string{"fg.toml": foreground})
	if _, err := Load(path); err != nil {
		t.Fatalf("interactive action on ticket_open: %v", err)
	}
}

func TestPromptAfterBecomesHooks(t *testing.T) {
	body := "workspace = \"1\"\n" + hookTimeTracking + "prompt_after = [\"assign\", \"complete\"]\n[[hooks]]\non = \"ticket_open\"\nrun = \"edit:branch\"\n"
	path := writeFile(t, body)
	_, err := Load(path)
	var pending *UpdateNeeded
	if !errors.As(err, &pending) || !strings.Contains(err.Error(), `time_tracking.prompt_after was replaced by [[hooks]] with run = "log_time"`) {
		t.Fatalf("err = %v", err)
	}
	raw := decode(t, body)
	if _, err := ApplyUpdates(raw); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"on": "ticket_open", "run": "edit:branch"},
		{"on": "assign", "when": "-assignee:@me -assignee:none", "run": "log_time"},
		{"on": "complete", "run": "log_time"},
	}
	if !reflect.DeepEqual(raw["hooks"], want) {
		t.Fatalf("hooks = %#v", raw["hooks"])
	}
	if _, ok := getPath(raw, "time_tracking.prompt_after"); ok {
		t.Fatal("prompt_after left in config")
	}
	raw = decode(t, "workspace = \"1\"\n"+hookTimeTracking+"prompt_after = []\n")
	if _, err := ApplyUpdates(raw); err != nil || raw["hooks"] != nil {
		t.Fatalf("empty prompt_after: hooks = %v, err = %v", raw["hooks"], err)
	}
}
