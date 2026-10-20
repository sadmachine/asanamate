package setup

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
)

func fakeClient(t *testing.T) *asana.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"gid":"u","name":"Ann","workspaces":[{"gid":"w1","name":"Home"},{"gid":"w2","name":"Work"}]}}`)
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return c
}

func options(t *testing.T, input string) (Options, *strings.Builder) {
	out := &strings.Builder{}
	dir := t.TempDir()
	return Options{
		In: bufio.NewReader(strings.NewReader(input)), Out: out, Client: fakeClient(t),
		ConfigPath: filepath.Join(dir, "cfg", "config.toml"), StatePath: filepath.Join(dir, "state.toml"),
	}, out
}

func TestRunWritesLoadableConfig(t *testing.T) {
	// Quotes, spaces, and Unicode must survive TOML encoding as data.
	root := filepath.Join(t.TempDir(), `it's "my" code ☃`)
	os.Mkdir(root, 0o700)
	o, out := options(t, "2\n/definitely/missing\n"+root+"\n")
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.Workspace != "w2" || cfg.RepoSource.Root != root || cfg.RepoSource.Command != "" || len(cfg.Actions) != 0 {
		t.Fatalf("cfg = %+v", cfg)
	}
	// The pager action is an example, so a fresh install needs no pager.
	actions := config.ActionsDir(o.ConfigPath)
	if err := os.Rename(filepath.Join(actions, "pager.toml.example"), filepath.Join(actions, "pager.toml")); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(o.ConfigPath); err != nil || len(cfg.Actions) != 1 || cfg.Actions[0].Key != "v" {
		t.Fatalf("enabled pager example: actions = %+v, err = %v", cfg.Actions, err)
	}
	info, _ := os.Stat(o.ConfigPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	for _, want := range []string{"Authenticated as Ann", "is not a directory", "Wrote " + o.ConfigPath} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Setup never overwrites action files, skips the example of an enabled one,
// and the example works once renamed.
func TestRunKeepsActionsAndExampleLoads(t *testing.T) {
	root := t.TempDir()
	o, _ := options(t, "2\n"+root+"\n")
	dir := config.ActionsDir(o.ConfigPath)
	os.MkdirAll(dir, 0o700)
	mine := "name = \"Mine\"\nkey = \"v\"\ncommand = \"true\"\n"
	os.WriteFile(filepath.Join(dir, "pager.toml"), []byte(mine), 0o600)
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "pager.toml")); string(data) != mine {
		t.Fatalf("action overwritten: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "pager.toml.example")); err == nil {
		t.Fatal("wrote pager.toml.example next to an enabled pager.toml")
	}
	if err := os.Rename(filepath.Join(dir, "claude-tmux.toml.example"), filepath.Join(dir, "claude-tmux.toml")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath)
	if err != nil {
		t.Fatalf("enabled example does not load: %v", err)
	}
	if len(cfg.Actions) != 2 || !cfg.Actions[0].Repo || cfg.Actions[1].Name != "Mine" {
		t.Fatalf("actions = %+v", cfg.Actions)
	}
}

func TestRunSkipsRepoRoot(t *testing.T) {
	o, out := options(t, "2\n-\n")
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.RepoSource != (config.RepoSource{}) {
		t.Fatalf("repo_source = %+v, want empty", cfg.RepoSource)
	}
	if !strings.Contains(out.String(), "type a repo path when an action asks") {
		t.Errorf("output does not explain the skipped repo picker:\n%s", out)
	}
}

func TestRunKeepsExistingConfigWhenDeclined(t *testing.T) {
	o, _ := options(t, "n\n")
	os.MkdirAll(filepath.Dir(o.ConfigPath), 0o700)
	os.WriteFile(o.ConfigPath, []byte("keep"), 0o600)
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected cancellation error")
	}
	if data, _ := os.ReadFile(o.ConfigPath); string(data) != "keep" {
		t.Fatalf("config overwritten: %q", data)
	}
}

func TestOutputStripsControlCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"gid":"u","name":"Ann\u001b[2J","workspaces":[{"gid":"w1","name":"Evil\u001b]0;x\u0007"}]}}`)
	}))
	defer srv.Close()
	o, out := options(t, t.TempDir()+"\n")
	o.Client = asana.New("tok")
	o.Client.BaseURL = srv.URL
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Fatalf("control characters printed: %q", out.String())
	}
}

// The template documents the defaults, so its values must equal Default().
// Only the values setup fills in or adds as examples may differ.
func TestTemplateMatchesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(Render(asana.Ref{GID: "1", Name: "W"}, "/code")), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Default()
	want.Workspace, want.RepoSource = got.Workspace, got.RepoSource
	if !reflect.DeepEqual(got, want) {
		t.Errorf("template values differ from config.Default():\ngot  %+v\nwant %+v", got, want)
	}
}

func TestTemplateOptionsStayTopLevel(t *testing.T) {
	// Uncomment every commented setting and table, but not prose.
	setting := regexp.MustCompile(`^# (\[|[a-z_]+ +=)`)
	var body strings.Builder
	for _, line := range strings.Split(Render(asana.Ref{GID: "1", Name: "W"}, "/code"), "\n") {
		if setting.MatchString(line) {
			line = strings.TrimPrefix(line, "# ")
		}
		body.WriteString(line + "\n")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(body.String()), 0o600)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config with every commented option enabled does not load: %v", err)
	}
	if cfg.Symbols != "unicode" || cfg.ReducedMotion == nil || cfg.Agents.Command != "asanamate-ccmux" ||
		!slices.Contains(cfg.Agents.States["working"], "running") || cfg.Agents.Symbols["waiting"] == "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// The Harvest provider moved out of asanamate in v0.2.0: Merge rewrites the
// old command to asanamate-hrvst, keeping its flags and every other value.
func TestMergeMovesHarvestProvider(t *testing.T) {
	updated, _, _, err := Merge("workspace = \"1\"\nsymbols = \"nerd\"\n[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate time-provider hrvst --task-id 456\"\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated)
	if cfg.TimeTracking.ID != "hrvst" || cfg.TimeTracking.Command != "asanamate-hrvst --task-id 456" || cfg.Symbols != "nerd" {
		t.Fatalf("merged = %+v, symbols = %q\n%s", cfg.TimeTracking, cfg.Symbols, updated)
	}
	if strings.Contains(updated, "time-provider") {
		t.Fatalf("old command left behind:\n%s", updated)
	}
	again, _, _, err := Merge(updated)
	if err != nil || again != updated {
		t.Fatalf("second merge changed config: err = %v", err)
	}
}

func TestMergeMovesReleasedKeys(t *testing.T) {
	updated, _, settings, err := Merge("workspace = \"1\"\ntheme = \"light\"\naccent_color = \"2\"\n[list.header]\nstyle = \"bar\"\ncolor = \"3\"\n[list.pinned]\ncolor = \"208\"\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated, settings)
	if cfg.Theme.Name != "ansi-light" || cfg.Colors.Accent.FG != "2" || cfg.Colors.Header.FG != "3" || cfg.Colors.Pinned.FG != "208" || cfg.List.Header.Style != "bar" {
		t.Fatalf("cfg = %+v\n%s", cfg, updated)
	}
	if strings.Contains(updated, "accent_color") || strings.Contains(updated, "[list.pinned]") {
		t.Fatalf("old keys left behind:\n%s", updated)
	}
}

func TestMergeMovesEmptyPinnedToAccent(t *testing.T) {
	updated, _, _, err := Merge("workspace = \"1\"\naccent_color = \"6\"\n[list.pinned]\ncolor = \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg := loadString(t, updated); cfg.Palette(true).Colors.Pinned.FG != "6" {
		t.Fatalf("pinned = %+v\n%s", cfg.Palette(true).Colors.Pinned, updated)
	}
}

func TestMergeMovesDisplayFilterAndRepo(t *testing.T) {
	updated, _, _, err := Merge(`workspace = "1"
[display]
symbols = "nerd"
reduced_motion = false
[list]
filter = "is:open project:Work"
fields = ["section", "Branch Name", "due"]
[repo]
root = ""
command = "sesh list -z"
branch_field = "Branch Name"
`)
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated)
	if cfg.Symbols != "nerd" || cfg.ReducedMotion == nil || *cfg.ReducedMotion || cfg.List.DefaultView.Filter != "is:open project:Work" || cfg.RepoSource.Root != "" || cfg.RepoSource.Command != "sesh list -z" || cfg.BranchField != "Branch Name" || !reflect.DeepEqual(cfg.List.Fields, []string{"section", "Branch Name", "due"}) {
		t.Fatalf("cfg = %+v\n%s", cfg, updated)
	}
	for _, old := range []string{"[display]", "[repo]", "default_filter = "} {
		if strings.Contains(updated, old) {
			t.Fatalf("old key %q left behind:\n%s", old, updated)
		}
	}
	again, _, _, err := Merge(updated)
	if err != nil || again != updated {
		t.Fatalf("second merge changed config: err = %v", err)
	}
}

// The ccmux preset became the asanamate-ccmux adapter; the rest of [agents]
// stays as set.
func TestMergeRewritesAgentsPreset(t *testing.T) {
	updated, _, _, err := Merge("workspace = \"1\"\n[agents]\npreset = \"ccmux\"\n[agents.states]\nworking = [\"busy\"]\n[agents.symbols]\nwaiting = \"!!\"\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated)
	if cfg.Agents.Command != "asanamate-ccmux" || !reflect.DeepEqual(cfg.Agents.States, map[string][]string{"working": {"busy"}}) || cfg.Agents.Symbols["waiting"] != "!!" {
		t.Fatalf("agents = %+v\n%s", cfg.Agents, updated)
	}
	if strings.Contains(updated, "preset") {
		t.Fatalf("agents.preset left behind:\n%s", updated)
	}
	again, _, _, err := Merge(updated)
	if err != nil || again != updated {
		t.Fatalf("second merge changed config: err = %v", err)
	}
	for body, want := range map[string]string{
		"[agents]\npreset = \"ccmux\"\ncommand = \"x\"\n": "agents.preset and agents.command are both set",
		"[agents]\npreset = \"tmux\"\n":                   `agents.preset "tmux" has no replacement`,
	} {
		if _, _, _, err := Merge("workspace = \"1\"\n" + body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}

func TestMergeKeepsStyleTables(t *testing.T) {
	updated, _, _, err := Merge("workspace = \"1\"\n[colors]\naccent = { fg = \"4\", bold = false }\nauthors = [\"1\", { fg = \"2\", italic = true }]\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`accent = { bold = false, fg = "4" }`, `authors = ["1", { fg = "2", italic = true }]`} {
		if !strings.Contains(updated, want) {
			t.Errorf("missing %s in:\n%s", want, updated)
		}
	}
	if cfg := loadString(t, updated); *cfg.Colors.Accent.Bold || !*cfg.Colors.Authors[1].Italic {
		t.Fatalf("cfg = %+v", cfg.Colors)
	}
}

func TestMergeConversationsConfig(t *testing.T) {
	updated, _, _, err := Merge("workspace = \"1\"\n[conversations]\ncommand = \"custom list\"\nopen = \"custom open\"\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated)
	if cfg.Conversations.Command != "custom list" || cfg.Conversations.Open != "custom open" {
		t.Fatalf("commands not preserved: %+v", cfg.Conversations)
	}
	again, _, _, err := Merge(updated)
	if err != nil || again != updated {
		t.Fatalf("merge not idempotent: %v", err)
	}
	updated, _, _, err = Merge("workspace = \"1\"\n")
	if err != nil || loadString(t, updated).ConversationsEnabled() || !strings.Contains(updated, "[conversations]") {
		t.Fatalf("old config upgrade = %s, error = %v", updated, err)
	}
}

func TestUpdateMigratesV010Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("workspace = \"1\"\naccent_color = \"2\"\n"), 0o600)
	o := Options{In: bufio.NewReader(strings.NewReader("")), Out: &strings.Builder{}, ConfigPath: path}
	if err := Update(o, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Colors.Accent.FG != "2" {
		t.Fatalf("cfg = %+v, err = %v", cfg.Colors.Accent, err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".config-*")); len(leftovers) != 0 {
		t.Fatalf("temporary check file left behind: %v", leftovers)
	}
}

func TestRunWritesThemeExample(t *testing.T) {
	path := runSetup(t)
	example := filepath.Join(config.ThemesDir(path), "example.toml.example")
	data, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(config.ThemesDir(path), "mine.toml"), data, 0o600)
	if err := config.SaveSetting(path, "theme.name", "mine"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Palette(true).Colors.Accent.FG != "#7aa2f7" {
		t.Fatalf("accent = %+v, err = %v", cfg.Palette(true).Colors.Accent, err)
	}
}

// runSetup runs setup without a repo directory and returns the config path.
func runSetup(t *testing.T) string {
	t.Helper()
	o, _ := options(t, "2\n-\n")
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return o.ConfigPath
}

// loadString loads body as a config file.
func loadString(t *testing.T, body string, settings ...map[string]any) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(settings) > 0 {
		data, err := config.EncodeSettings(settings[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := config.WriteSettings(path, data); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	return cfg
}

func TestTemplateListsEveryBinding(t *testing.T) {
	tmpl := Render(asana.Ref{GID: "1", Name: "W"}, "")
	for _, s := range keymap.Catalog() {
		if !strings.Contains(tmpl, "\n[keys."+s.Name+"]\n") {
			t.Errorf("template lacks [keys.%s]", s.Name)
		}
		for _, b := range s.Bindings {
			if !strings.Contains(tmpl, "\n# "+b.Name+" = ") {
				t.Errorf("template lacks keys.%s.%s", s.Name, b.Name)
			}
		}
	}
}

func TestMergeKeepsKeyOverrides(t *testing.T) {
	updated, added, _, err := Merge("workspace = \"1\"\n[keys.main]\nquit = [\"Q\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg := loadString(t, updated); cfg.Keymap.Name("main", "Q") != "quit" {
		t.Fatalf("override lost:\n%s", updated)
	}
	if !slices.Contains(added, "picker.type_first") {
		t.Errorf("added = %v", added)
	}
}

func TestUpdateWritesThemeExample(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("workspace = \"1\"\n"), 0o600)
	o := Options{In: bufio.NewReader(strings.NewReader("")), Out: &strings.Builder{}, ConfigPath: path}
	if err := Update(o, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config.ThemesDir(path), "example.toml.example")); err != nil {
		t.Fatalf("config update must add the theme example: %v", err)
	}
}

func TestThemeInstallerKeepsEditsAndAddsMissingFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config home")
	t.Setenv("XDG_CONFIG_HOME", root)
	script, err := filepath.Abs(filepath.Join("..", "..", "install-themes.sh"))
	if err != nil {
		t.Fatal(err)
	}
	workingDir := t.TempDir()
	install := func() {
		t.Helper()
		cmd := exec.Command("sh", script)
		cmd.Dir = workingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("install themes: %v\n%s", err, out)
		}
	}
	install()
	path := filepath.Join(root, "asanamate", "config.toml")
	dir := config.ThemesDir(path)
	// Theme files and the upstream licenses beside them install alike.
	glob := func(from string) []string {
		t.Helper()
		themes, err := filepath.Glob(filepath.Join(from, "*.toml"))
		if err != nil {
			t.Fatal(err)
		}
		licenses, err := filepath.Glob(filepath.Join(from, "LICENSE*"))
		if err != nil {
			t.Fatal(err)
		}
		return append(themes, licenses...)
	}
	sources := glob(filepath.Join("..", "..", "themes"))
	if len(sources) == 0 {
		t.Fatal("no source themes")
	}
	files := glob(dir)
	if len(files) != len(sources) {
		t.Fatalf("installed files = %v; want %d", files, len(sources))
	}
	for _, file := range files {
		want, err := os.ReadFile(filepath.Join("..", "..", "themes", filepath.Base(file)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(file)
		if err != nil || string(got) != string(want) {
			t.Fatalf("installed theme differs: %s, err = %v", file, err)
		}
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("theme permissions: %s, err = %v", file, err)
		}
	}
	custom := "base = \"dark\"\n[colors]\naccent = \"#123456\"\n"
	if err := os.WriteFile(filepath.Join(dir, "one-dark.toml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "one-light.toml")); err != nil {
		t.Fatal(err)
	}
	install()
	if got, err := os.ReadFile(filepath.Join(dir, "one-dark.toml")); err != nil || string(got) != custom {
		t.Fatalf("reinstall must keep theme edits: %q, err = %v", got, err)
	}
	if err := os.WriteFile(path, []byte("workspace = \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSettings(path, []byte("[theme]\nname = \"auto\"\n[theme.auto]\ndark = \"one-dark\"\nlight = \"one-light\"\n")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Palette(true).Colors.Accent.FG != "#123456" || cfg.Palette(false).Colors.Accent.FG != "#4078f2" {
		t.Fatal("installed themes must load local edits and restore missing files")
	}
}

func TestHarvestTemplateUsesNativeSetup(t *testing.T) {
	template := Render(asana.Ref{GID: "1", Name: "W"}, "")
	for _, want := range []string{"# Run asanamate-hrvst setup first.", "# command = \"asanamate-hrvst\"", "--task-id ID remains an explicit override"} {
		if !strings.Contains(template, want) {
			t.Errorf("template missing %q", want)
		}
	}
	if strings.Contains(template, "YOUR_TASK_ID") {
		t.Fatal("template requires manual task ID")
	}
}
