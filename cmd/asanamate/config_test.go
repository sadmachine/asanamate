package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	previous := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = previous }()
	runErr := run()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func cliConfig(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListUsesDefaultViewFilter(t *testing.T) {
	cliConfig(t, "workspace = \"1\"\n[list.default_view]\nfilter = \"is:done\"\n")
	t.Setenv(config.TokenEnv, "test-token")
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/1.0/projects/1/tasks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"gid":"2","name":"Open","completed":false},{"gid":"3","name":"Done","completed":true}]}`))}, nil
	})
	for _, tc := range []struct {
		args []string
		open bool
	}{
		{[]string{"--project", "1"}, false},
		{[]string{"--project", "1", "--filter", ""}, true},
	} {
		out, err := captureStdout(t, func() error { return runList(tc.args) })
		if err != nil || !strings.Contains(out, "Done") || strings.Contains(out, "Open") != tc.open {
			t.Fatalf("args = %v, out = %q, err = %v", tc.args, out, err)
		}
	}
}

func TestDoctorReportsSettingsAndNoLegacyHook(t *testing.T) {
	path := cliConfig(t, "workspace = \"1\"\n")
	t.Setenv(config.TokenEnv, "")
	for _, exists := range []bool{false, true} {
		if exists {
			if err := config.WriteSettings(path, config.DefaultSettings()); err != nil {
				t.Fatal(err)
			}
		}
		out, err := captureStdout(t, func() error { return runDoctor(nil) })
		want := "defaults (no file)"
		if exists {
			want = config.SettingsPath(path)
		}
		if !errors.Is(err, errDoctorBlocking) || !strings.Contains(out, "settings") || !strings.Contains(out, want) || strings.Contains(out, "codex_hook") {
			t.Fatalf("doctor = %q, err = %v", out, err)
		}
	}
}

func TestNewWriterResolvesConfirmation(t *testing.T) {
	t.Setenv(config.TokenEnv, "test-token")
	for _, tc := range []struct {
		config, env string
		yes, want   bool
		fails       bool
	}{
		{config: "", env: "", want: true},
		{config: "confirm_writes = false\n", env: "", want: false},
		{config: "confirm_writes = false\n", env: "true", want: true},
		{config: "", env: "0", want: false},
		{config: "", env: "1", yes: true, want: false},
		{config: "confirm_writes = false\n", env: "sometimes", fails: true},
	} {
		cliConfig(t, "workspace = \"1\"\n"+tc.config)
		t.Setenv("ASANAMATE_CONFIRM_WRITES", tc.env)
		svc, err := newWriter(tc.yes)
		if tc.fails {
			if err == nil {
				t.Errorf("%+v: want an error", tc)
			}
			continue
		}
		if err != nil || (svc.Confirm != nil) != tc.want || svc.Workspace != "1" {
			t.Errorf("%+v: confirm = %v, workspace = %q, err = %v", tc, svc.Confirm != nil, svc.Workspace, err)
		}
	}
}

func TestConfigUpdateRewritesMovedKeys(t *testing.T) {
	path := cliConfig(t, "workspace = \"1\"\naccent_color = \"2\"\n")
	if err := runConfig([]string{"update", "--yes"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Colors.Accent.FG != "2" {
		t.Fatalf("accent = %+v, err = %v", cfg.Colors.Accent, err)
	}
	if err := runConfig([]string{"update", "extra"}); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("extra argument: %v", err)
	}
}
