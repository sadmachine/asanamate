package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	if cfg.Theme != "dark" || cfg.Images != "auto" || cfg.DefaultFilter != "is:open" || !cfg.ConfirmWrites {
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
		"bad images":    "workspace = \"1\"\nimages = \"sixel\"\n",
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
	if cfg.AccentColor != "4" || cfg.List.Header != (Header{Style: StyleRule}) || cfg.List.Selection != (Selection{Style: StyleMarker}) {
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
		"bad selection": "[list.selection]\nstyle = \"rule\"\n",
		"bad marker":    "[list.selection]\ncolor = \"red\"\n",
		"empty accent":  "accent_color = \"\"\n",
	} {
		if _, err := Load(writeFile(t, "workspace = \"1\"\n"+body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
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
