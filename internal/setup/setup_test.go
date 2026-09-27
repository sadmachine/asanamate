package setup

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
