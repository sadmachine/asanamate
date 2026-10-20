package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionsWithoutCoreSetupOrToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ASANA_ACCESS_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{nil, {"harvest"}, {"ccmux"}, {"hub"}, {"asana-sessions"}, {"actions"}} {
		var out strings.Builder
		if err := runExtensions(args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out.String(), "core setup required") {
			t.Fatal(out.String())
		}
		if len(args) == 0 {
			for _, name := range []string{"Harvest", "ccmux", "imarc Hub", "Conversation history", "Actions and custom commands"} {
				if !strings.Contains(out.String(), name) {
					t.Errorf("missing %s: %s", name, out.String())
				}
			}
		}
	}
	for _, args := range [][]string{{"unknown"}, {"check", "unknown"}, {"check"}, {"harvest", "extra"}} {
		var out strings.Builder
		err := runExtensions(args, &out)
		if err == nil {
			t.Errorf("accepted %v", args)
		} else if args[len(args)-1] == "unknown" && !strings.Contains(err.Error(), "available IDs: harvest, ccmux, hub, asana-sessions, actions") {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	if err := runExtensions([]string{"check", "harvest"}, &out); err == nil || !strings.Contains(out.String(), "check failed") {
		t.Fatalf("failed requested check: %s, %v", out.String(), err)
	}
}

func TestExtensionCommandAndCompatibilityAlias(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ASANA_ACCESS_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	var previous string
	for _, command := range []string{"extensions", "addons"} {
		out, err := captureStdout(t, func() error {
			if code := run([]string{command, "asana-sessions"}); code != 0 {
				t.Errorf("%s exited %d", command, code)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Conversation discovery v1", "Conversation open v1", "#discovery", "#foreground-opening", "Optional automatic linking", "Build your own extension"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s guide missing %q", command, want)
			}
		}
		if previous != "" && out != previous {
			t.Fatal("compatibility alias returned a different guide")
		}
		previous = out
	}
}

func TestExtensionGuidesSurviveInvalidConfigWithoutRepair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "asanamate", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "workspace = \"1\"\nunknown_key = true\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"hub"}} {
		var out strings.Builder
		if err := runExtensions(args, &out); err != nil || !strings.Contains(out.String(), "unknown_key") || !strings.Contains(out.String(), "Hub") {
			t.Fatalf("guide: %s; %v", out.String(), err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("discovery changed config: %q, %v", data, err)
	}
}

func TestDoctorExtensionFailuresRemainOptionalAndKnownFeedsStayLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("workspace = \"1\"\n[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate-hrvst --task-id 123\"\n[feed]\ncommands = [\"asanamate-hrvst\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var out strings.Builder
	checkExtensions(context.Background(), &out, path)
	if !strings.Contains(out.String(), "warning:") || !strings.Contains(out.String(), "Time tracking: configured") || !strings.Contains(out.String(), "Today's feed: configured") {
		t.Fatal(out.String())
	}
	out.Reset()
	checkFeeds(context.Background(), &out, []string{"asanamate-hrvst"})
	if !strings.Contains(out.String(), "connection unverified") || strings.Contains(out.String(), "failed:") {
		t.Fatal(out.String())
	}
}
