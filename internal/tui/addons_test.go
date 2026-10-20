package tui

import (
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/addons"
	"github.com/sadmachine/asanamate/internal/config"
)

func TestDisabledAddonsDiscoverableFromSettings(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.loading = false
	t.Setenv("PATH", t.TempDir())
	m.Update(key("s"))
	m.Update(key("o"))
	if m.modal == nil || m.modal.title != "Addons" || len(m.modal.items) != 4 {
		t.Fatalf("catalog missing: %+v", m.modal)
	}
	for _, it := range m.modal.items {
		if it.Disabled || it.Help == "" || it.Hint == "" {
			t.Fatalf("unavailable entry hidden or lacks status: %+v", it)
		}
	}
	m.Update(key("/"))
	m.Update(key("h"))
	m.Update(key("u"))
	m.Update(key("b"))
	if len(m.modal.matches) != 1 || !strings.Contains(m.modal.items[m.modal.matches[0]].Label, "imarc Hub") {
		t.Fatalf("addon search failed: %+v", m.modal.matches)
	}
	m.Update(key("enter"))
	if m.modal != nil || m.addon == nil || m.addon.entry.ID != "hub" {
		t.Fatal("detail did not open")
	}
	if view := m.addon.view(40, 14); !strings.Contains(view, "Next:") || !strings.Contains(view, "actions") || lipgloss.Height(view) > 14 {
		t.Fatalf("narrow detail overflow or missing navigation:\n%s", view)
	}
	m.Update(key("j"))
	if m.addon.reader.YOffset() == 0 {
		t.Fatal("details did not scroll")
	}
	m.Update(key("esc"))
	if m.addon != nil || m.modal == nil || m.modal.title != "Addons" {
		t.Fatal("escape did not return to catalog")
	}
}

func TestAddonDetailsWrapAndKeepNavigation(t *testing.T) {
	m, _ := testModel(t, config.Default())
	for _, e := range addons.Catalog() {
		m.openAddon(e)
		for _, width := range []int{20, 40, 76} {
			view := m.addon.view(width, 12)
			if lipgloss.Height(view) > 12 || !strings.Contains(view, "Next:") || !strings.Contains(view, "back") {
				t.Fatalf("%s width %d: overflow/navigation\n%s", e.ID, width, view)
			}
			for _, line := range strings.Split(ansi.Strip(view), "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s width %d: %q", e.ID, width, line)
				}
			}
		}
	}
}

func TestAddonSetupCancellationAndSavedConfigIsolation(t *testing.T) {
	m, _ := testModel(t, config.Default())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(dir+"/hrvst", []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := addons.Find("harvest")
	m.openAddon(e)
	m.confirmAddonSetup()
	if m.modal == nil || !strings.Contains(m.modal.notice, dir+"/hrvst") || !strings.Contains(m.modal.items[0].Label, "hrvst login") {
		t.Fatal("setup confirmation must show command and resolved path")
	}
	m.Update(key("esc"))
	after, err := os.ReadFile(m.deps.ConfigPath)
	if err != nil || string(after) != string(before) || m.addon == nil || m.modal != nil {
		t.Fatalf("cancellation changed config or left details: %v", err)
	}
	if err := os.WriteFile(m.deps.ConfigPath, []byte("workspace = \"1\"\n[agents]\ncommand = \"asanamate-ccmux\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, _ = addons.Find("ccmux")
	m.openAddon(e)
	if !m.addon.report.Configured() || m.deps.Config.Agents.Command != "" {
		t.Fatal("saved config must be inspected without changing active config")
	}
	old := m.addon
	m.openAddons()
	m.Update(addonCheckedMsg{detail: old, report: addons.Report{Check: "stale"}})
	if m.addon != nil || m.modal == nil {
		t.Fatal("late check reopened closed detail")
	}
}
