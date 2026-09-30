package setup

import (
	"bufio"
	"context"
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
	root := t.TempDir()
	o, out := options(t, "2\n/definitely/missing\n"+root+"\n")
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.Workspace != "w2" || !strings.Contains(cfg.RepoSource.Command, root) || len(cfg.Actions) != 1 {
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
