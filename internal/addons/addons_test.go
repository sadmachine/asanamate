package addons

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/config"
)

func tool(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryDoesNotExecuteCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	marker := filepath.Join(dir, "executed")
	t.Setenv("ADDON_MARKER", marker)
	for _, name := range []string{"asanamate-hrvst", "hrvst", "node", "npm", "asanamate-ccmux", "ccmux", "tmux", "asanamate-hub"} {
		tool(t, dir, name, `printf ran > "$ADDON_MARKER"; exit 1`)
	}
	cfg := config.Default()
	cfg.TimeTracking.ID, cfg.TimeTracking.Command = "hrvst", "asanamate-hrvst --task-id 123"
	cfg.Feed.Commands = []string{"asanamate-hrvst"}
	saved := Saved{Path: filepath.Join(dir, "config.toml"), Config: cfg}
	for _, e := range Catalog() {
		r := Inspect(e, saved)
		guide := Detail(e, saved, r)
		for _, want := range []string{e.Benefit, e.Guide, "connection unverified", "Installation", "Checks and first use", "Disable, uninstall", saved.Path, "restarting asanamate"} {
			if !strings.Contains(guide, want) {
				t.Errorf("%s guide missing %q", e.ID, want)
			}
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("discovery ran a command: %v", err)
	}
}

func TestHarvestCapabilitiesIndependentAndCustomSafe(t *testing.T) {
	e, _ := Find("harvest")
	cfg := config.Default()
	cfg.TimeTracking.ID, cfg.TimeTracking.Command = "existing-id", "asanamate-hrvst --task-id 123"
	r := Inspect(e, Saved{Config: cfg})
	if !r.Capabilities[0].Configured || r.Capabilities[1].Configured || r.Capabilities[1].Status != "not configured" {
		t.Fatalf("tracker-only statuses: %+v", r.Capabilities)
	}
	cfg.TimeTracking.Command = "asanamate-hrvst"
	r = Inspect(e, Saved{Config: cfg})
	if !r.Capabilities[0].Configured || len(e.Dependencies) != 0 || strings.Join(e.Setup, " ") != "asanamate-hrvst setup" || !strings.Contains(e.Verify, "check --online") {
		t.Fatalf("native catalog: %+v", r)
	}
	cfg.TimeTracking.Command = "custom-tracker"
	cfg.Feed.Commands = []string{"asanamate-hrvst"}
	r = Inspect(e, Saved{Config: cfg})
	if r.Capabilities[0].Configured || !strings.Contains(r.Capabilities[0].Status, "custom command") || !r.Capabilities[1].Configured {
		t.Fatalf("feed-only statuses: %+v", r.Capabilities)
	}
	for _, command := range []string{"echo asanamate-hrvst", "asanamate-hrvst; touch /tmp/x", "asanamate-hrvst --task-id $(echo 1)", "/tmp/asanamate-hrvst", "env asanamate-hrvst", "asanamate-hrvst --task-id 1 && true"} {
		if _, known := KnownCommand("time_tracking.command", command); known {
			t.Errorf("mistook custom command for known provider: %s", command)
		}
	}
}

func TestMissingAndInvalidConfigKeepGuides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	saved := Load(path)
	if !strings.Contains(saved.Status(), "core setup required") {
		t.Fatal(saved.Status())
	}
	if err := os.WriteFile(path, []byte("workspace = \"1\"\nunknown_addon_key = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved = Load(path)
	if !strings.Contains(saved.Status(), "unknown_addon_key") {
		t.Fatal(saved.Status())
	}
	for _, e := range Catalog() {
		if text := Detail(e, saved, Inspect(e, saved)); !strings.Contains(text, e.Snippet) || !strings.Contains(text, "unknown_addon_key") {
			t.Fatalf("guide hidden for %s: %s", e.ID, text)
		}
	}
}

func TestChecksBoundedAndDiagnosticsWithheld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, name := range []string{"hrvst", "node", "npm"} {
		tool(t, dir, name, "exit 0")
	}
	e, _ := Find("harvest")
	saved := Saved{Config: config.Default()}
	t.Run("success remains unverified", func(t *testing.T) {
		tool(t, dir, "asanamate-hrvst", `test "$#" = 1 && test "$1" = check || exit 9; printf SECRET`)
		r := Check(context.Background(), e, saved)
		if r.Failed || !strings.Contains(r.Check, "local check passed") || !strings.Contains(r.Check, "connection unverified") || strings.Contains(r.Check, "SECRET") {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("failure hides credentials and controls", func(t *testing.T) {
		tool(t, dir, "asanamate-hrvst", `printf '\033]52;c;SECRET\007TOKEN' >&2; exit 1`)
		r := Check(context.Background(), e, saved)
		if !r.Failed || strings.Contains(r.Check, "SECRET") || strings.Contains(r.Check, "TOKEN") || strings.ContainsRune(r.Check, '\033') {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("output bound", func(t *testing.T) {
		tool(t, dir, "asanamate-hrvst", `while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done`)
		r := Check(context.Background(), e, saved)
		if !r.Failed || !strings.Contains(r.Check, "output limit") {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		tool(t, dir, "asanamate-hrvst", "while :; do :; done")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		start := time.Now()
		r := Check(ctx, e, saved)
		if !r.Failed || time.Since(start) > time.Second || !strings.Contains(r.Check, "cancelled") {
			t.Fatalf("%+v", r)
		}
	})
}

func TestMissingToolsAndSessionValidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	e, _ := Find("ccmux")
	r := Check(context.Background(), e, Saved{Config: config.Default()})
	if !r.Failed || !strings.Contains(r.Check, "adapter missing") {
		t.Fatalf("%+v", r)
	}
	tool(t, dir, "asanamate-ccmux", "exit 0")
	r = Check(context.Background(), e, Saved{Config: config.Default()})
	if !r.Failed || !strings.Contains(r.Check, "dependency missing") {
		t.Fatalf("%+v", r)
	}
	tool(t, dir, "tmux", "exit 0")
	for _, reply := range []string{"[]", "null", "{}", "not-json"} {
		tool(t, dir, "ccmux", "printf '%s' '"+reply+"'")
		r = Check(context.Background(), e, Saved{Config: config.Default()})
		if r.Failed != (reply != "[]") {
			t.Errorf("reply %q: %+v", reply, r)
		}
	}
}

func TestHubLocalCredentialCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HUB_CLIENT_ID", "dummy-id")
	t.Setenv("HUB_CLIENT_SECRET", "dummy-secret")
	tool(t, dir, "asanamate-hub", `test "$#" = 1 && test "$1" = check || exit 9
test -n "$HUB_CLIENT_ID" && test -n "$HUB_CLIENT_SECRET"`)
	e, _ := Find("hub")
	saved := Saved{Config: config.Default()}
	saved.Config.ProjectLinks.Command = "asanamate-hub"
	r := Check(context.Background(), e, saved)
	if r.Failed || !strings.Contains(r.Check, "local check passed: asanamate-hub check") || !strings.Contains(r.Check, "connection unverified") {
		t.Fatalf("%+v", r)
	}
	t.Setenv("HUB_CLIENT_SECRET", "")
	r = Check(context.Background(), e, saved)
	if !r.Failed || !strings.Contains(r.Next, "HUB_CLIENT_SECRET") || strings.Contains(r.Check, "dummy-id") {
		t.Fatalf("%+v", r)
	}
}
