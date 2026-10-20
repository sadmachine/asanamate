package extensions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestContractReferencesAndVersions(t *testing.T) {
	docs, err := os.ReadFile("../../docs/extensions.md")
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{}
	for _, line := range strings.Split(string(docs), "\n") {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			anchors[strings.ReplaceAll(strings.ToLower(strings.TrimLeft(line, "# ")), " ", "-")] = true
		}
	}
	seen := map[string]bool{}
	for _, c := range Contracts() {
		if seen[c.ID] || !anchors[c.Anchor] || c.Transport == "" || c.Configuration == "" || c.Operations == "" {
			t.Errorf("duplicate/incomplete contract or missing documentation: %s", c.ID)
		}
		seen[c.ID] = true
		if c.Version == 0 && !strings.Contains(c.Label(), "unversioned") {
			t.Errorf("invented version for %s", c.ID)
		}
	}
	if len(seen) != 8 || !anchors["implementing-an-extension"] {
		t.Fatal("custom extension guide must cover every supported contract")
	}
	for _, e := range Catalog() {
		for _, capability := range e.Capabilities {
			if !seen[capability.ContractID] {
				t.Errorf("%s references unknown contract %q", e.ID, capability.ContractID)
			}
		}
	}
	for _, tc := range []struct{ version, revision string }{
		{"0.4.0", "v0.4.0"}, {"v0.4.0-rc.1", "v0.4.0-rc.1"}, {"dev", "main"},
		{"0.4.0-SNAPSHOT", "main"}, {"(devel)", "main"}, {"v0.4.0/other", "main"},
	} {
		got := DocumentURL(tc.version, "docs/extensions.md", "time-tracking")
		if want := repository + "/blob/" + tc.revision + "/docs/extensions.md#time-tracking"; got != want {
			t.Errorf("%q: %s, want %s", tc.version, got, want)
		}
	}
}

func TestImplementationGuidesAndCustomContractSlots(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	saved := Saved{Config: config.Default(), Version: "0.4.0"}
	saved.Config.TimeTracking.ID = "clockify"
	saved.Config.TimeTracking.Command = "asanamate-clockify"
	harvest, _ := Find("harvest")
	report := Inspect(harvest, saved)
	text := Detail(harvest, saved, report)
	for _, want := range []string{"Time tracking v1", "Feeds v1", "#time-tracking", "#feeds", "#implementing-an-extension", "Custom command contract: time-tracking", "Harvest API v2"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if report.Capabilities[0].Configured || !report.Capabilities[0].Custom || report.Capabilities[1].Configured {
		t.Fatal("custom tracker must not be mistaken for Harvest or enable a feed")
	}
	if saved.Config.TimeTracking.ID != "clockify" || saved.Config.TimeTracking.Command != "asanamate-clockify" {
		t.Fatal("inspecting an extension changed the configured provider")
	}
	actions, _ := Find("actions")
	if len(actions.Contracts()) != len(Contracts()) {
		t.Fatal("custom-command guide does not expose all contracts")
	}
	for _, e := range Catalog() {
		guide := Detail(e, saved, Inspect(e, saved))
		if !strings.Contains(guide, "/blob/v0.4.0/docs/extensions.md#implementing-an-extension") || strings.Contains(guide, "development build") {
			t.Errorf("%s guide does not match the installed release", e.ID)
		}
	}
}

func TestConversationDiscoveryAndChecksStayNonmutating(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	marker := filepath.Join(dir, "doctor-ran")
	t.Setenv("EXTENSION_MARKER", marker)
	tool(t, dir, "node", "exit 0")
	tool(t, dir, "asana-sessions", `test "$#" = 1 && test "$1" = doctor || exit 9
printf ran > "$EXTENSION_MARKER"
printf 'info: codex plugin: untrusted: optional\n'`)
	e, _ := Find("asana-sessions")
	cfg := config.Default()
	cfg.Conversations.Command, cfg.Conversations.Open = "asana-sessions asanamate list", "asana-sessions asanamate open"
	saved := Saved{Config: cfg}
	r := Inspect(e, saved)
	if len(r.Capabilities) != 2 || !r.Capabilities[0].Configured || !r.Capabilities[1].Configured {
		t.Fatal("discovery and foreground open must each expose their config status")
	}
	guide := Detail(e, saved, r)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("discovery executed helper diagnostics")
	}
	for _, want := range []string{"Conversation discovery v1", "Conversation open v1", "ASANAMATE_CONVERSATION_JSON", "Optional automatic linking: unchecked", "Terminal opening remains unverified", "conversations.command", "conversations.open"} {
		if !strings.Contains(guide, want) {
			t.Errorf("missing %q", want)
		}
	}
	if checked := Check(context.Background(), e, saved); checked.Failed {
		t.Fatalf("untrusted optional hook blocked metadata diagnostics: %s", checked.Check)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("explicit check did not run the documented doctor command")
	}
	for _, command := range []string{"asana-sessions asanamate list; echo extra", "asana-sessions asanamate open", "custom-history"} {
		if _, known := KnownCommand("conversations.command", command); known {
			t.Errorf("mistook custom/wrong operation for discovery: %s", command)
		}
	}
}
