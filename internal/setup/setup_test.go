package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
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
	if cfg.Workspace != "w2" || cfg.RepoSource.Root != root || cfg.RepoSource.Command != "" || len(cfg.Actions) != 1 {
		t.Fatalf("cfg = %+v", cfg)
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

// Setup never overwrites action files, and the example works once renamed.
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

func TestWarnMissingPager(t *testing.T) {
	var out strings.Builder
	t.Setenv("PAGER", "no-such-pager-asanamate --flag")
	warnMissingPager(&out)
	if !strings.Contains(out.String(), `pager "no-such-pager-asanamate" not found`) {
		t.Errorf("missing pager warning:\n%s", out.String())
	}
	out.Reset()
	t.Setenv("PAGER", "sh -c")
	warnMissingPager(&out)
	if out.Len() != 0 {
		t.Errorf("warned about an installed pager:\n%s", out.String())
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
	// Uncomment every commented setting and table, but not prose. The
	// custom agents command is the documented alternative to the preset, so
	// it stays commented.
	setting := regexp.MustCompile(`^# (\[|[a-z_]+ +=)`)
	var body strings.Builder
	for _, line := range strings.Split(Render(asana.Ref{GID: "1", Name: "W"}, "/code"), "\n") {
		if setting.MatchString(line) && !strings.HasPrefix(line, "# command = '''my-agents") {
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
	if cfg.Symbols != "unicode" || cfg.ReducedMotion == nil || cfg.Agents.Preset != "ccmux" ||
		!slices.Contains(cfg.Agents.States["working"], "running") || cfg.Agents.Symbols["waiting"] == "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestMergeKeepsTimeTrackingProvider(t *testing.T) {
	updated, _, err := Merge("workspace = \"1\"\n[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate time-provider hrvst --task-id 456\"\n")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.TimeTracking.ID != "hrvst" || cfg.TimeTracking.Command != "asanamate time-provider hrvst --task-id 456" {
		t.Fatalf("merged provider = %+v, err = %v", cfg.TimeTracking, err)
	}
}

func TestOfferCodexHook(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "hooks.json")
	theirs := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other && x"}]}]},"extra":1}`
	os.WriteFile(path, []byte(theirs), 0o600)
	offer := func(input string) string {
		t.Helper()
		out := &strings.Builder{}
		o := Options{In: bufio.NewReader(strings.NewReader(input)), Out: out, CodexHome: home, Executable: "/bin/it's asanamate"}
		if err := OfferCodexHook(o); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if out := offer("n\n"); !strings.Contains(out, "inaccurate or wrong") || !strings.Contains(out, "setup hooks") {
		t.Fatalf("decline output = %q", out)
	}
	if data, _ := os.ReadFile(path); string(data) != theirs {
		t.Fatalf("declined but changed: %s", data)
	}

	offer("y\n")
	if data, _ := os.ReadFile(path + ".bak"); string(data) != theirs {
		t.Fatalf("backup = %s", data)
	}
	var got struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
		Extra int
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := `'/bin/it'\''s asanamate' hook codex`
	stop := got.Hooks["Stop"]
	if got.Extra != 1 || len(stop) != 2 || stop[0].Hooks[0].Command != "other && x" || stop[1].Hooks[0].Command != want {
		t.Fatalf("hooks.json = %s", data)
	}
	if len(got.Hooks["UserPromptSubmit"]) != 1 || len(got.Hooks["SessionEnd"]) != 1 {
		t.Fatalf("missing events: %s", data)
	}

	if out := offer(""); out != "" {
		t.Fatalf("installed hook offered again: %q", out)
	}
}

// A hook left at an old path, or missing from some events, is replaced in
// place: other hooks, even in the same group, stay, and nothing is doubled.
func TestOfferCodexHookRepairsStaleHook(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "hooks.json")
	stale := `{"hooks":{"Stop":[{"matcher":"x","hooks":[` +
		`{"type":"command","command":"other"},{"type":"command","command":"'/old/asanamate' hook codex"}]}],` +
		`"SessionEnd":[{"hooks":[{"type":"command","command":"'/new/asanamate' hook codex"}]}]}}`
	os.WriteFile(path, []byte(stale), 0o600)
	offer := func(input string) string {
		t.Helper()
		out := &strings.Builder{}
		o := Options{In: bufio.NewReader(strings.NewReader(input)), Out: out, CodexHome: home, Executable: "/new/asanamate"}
		if err := OfferCodexHook(o); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if state, err := CodexHook(home, "/new/asanamate"); err != nil || state != HookStale {
		t.Fatalf("state = %v, err = %v", state, err)
	}
	if out := offer("n\n"); !strings.Contains(out, "out of date") {
		t.Fatalf("decline output = %q", out)
	}
	if data, _ := os.ReadFile(path); string(data) != stale {
		t.Fatalf("declined but changed: %s", data)
	}

	if out := offer("y\n"); !strings.Contains(out, "Replaced the hook") {
		t.Fatalf("repair output = %q", out)
	}
	if data, _ := os.ReadFile(path + ".bak"); string(data) != stale {
		t.Fatalf("backup = %s", data)
	}
	var got struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Type, Command string }
		}
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	const want = `'/new/asanamate' hook codex`
	stop := got.Hooks["Stop"]
	if len(stop) != 2 || stop[0].Matcher != "x" || len(stop[0].Hooks) != 1 || stop[0].Hooks[0].Command != "other" || stop[1].Hooks[0].Command != want {
		t.Fatalf("hooks.json = %s", data)
	}
	for _, event := range agents.HookEvents() {
		ours := 0
		for _, g := range got.Hooks[event] {
			for _, h := range g.Hooks {
				if h.Command == want {
					ours++
				}
			}
		}
		if ours != 1 {
			t.Fatalf("%s has %d asanamate hooks: %s", event, ours, data)
		}
	}
	if state, _ := CodexHook(home, "/new/asanamate"); state != HookCurrent {
		t.Fatalf("state after repair = %v", state)
	}
	if out := offer(""); out != "" {
		t.Fatalf("repaired hook offered again: %q", out)
	}
}

func TestOfferCodexHookSkipsWithoutCodex(t *testing.T) {
	out := &strings.Builder{}
	o := Options{Out: out, CodexHome: filepath.Join(t.TempDir(), "missing"), Executable: "asanamate"}
	if err := OfferCodexHook(o); err != nil || out.Len() != 0 {
		t.Fatalf("err = %v, out = %q", err, out)
	}
	if state, err := CodexHook(o.CodexHome, o.Executable); err != nil || state != CodexAbsent {
		t.Fatalf("state = %v, err = %v", state, err)
	}
}

func TestMergeMovesReleasedKeys(t *testing.T) {
	updated, _, err := Merge("workspace = \"1\"\ntheme = \"light\"\naccent_color = \"2\"\n[list.header]\nstyle = \"bar\"\ncolor = \"3\"\n[list.pinned]\ncolor = \"208\"\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadString(t, updated)
	if cfg.Theme.Name != "light" || cfg.Colors.Accent.FG != "2" || cfg.Colors.Header.FG != "3" || cfg.Colors.Pinned.FG != "208" || cfg.List.Header.Style != "bar" {
		t.Fatalf("cfg = %+v\n%s", cfg, updated)
	}
	if strings.Contains(updated, "accent_color") || strings.Contains(updated, "[list.pinned]") {
		t.Fatalf("old keys left behind:\n%s", updated)
	}
}

func TestMergeMovesEmptyPinnedToAccent(t *testing.T) {
	updated, _, err := Merge("workspace = \"1\"\naccent_color = \"6\"\n[list.pinned]\ncolor = \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg := loadString(t, updated); cfg.Palette(true).Colors.Pinned.FG != "6" {
		t.Fatalf("pinned = %+v\n%s", cfg.Palette(true).Colors.Pinned, updated)
	}
}

func TestMergeKeepsStyleTables(t *testing.T) {
	updated, _, err := Merge("workspace = \"1\"\n[colors]\naccent = { fg = \"4\", bold = false }\nauthors = [\"1\", { fg = \"2\", italic = true }]\n")
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
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("expected config.toml and config.toml.bak only, got %v", entries)
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
	body, _ := os.ReadFile(path)
	body = []byte(strings.Replace(string(body), "name = \"auto\"", "name = \"mine\"", 1))
	os.WriteFile(path, body, 0o600)
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
func loadString(t *testing.T, body string) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	return cfg
}
