package tui

import (
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/extensions"
)

func TestDisabledExtensionsDiscoverableFromSettings(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.loading = false
	t.Setenv("PATH", t.TempDir())
	m.Update(key("s"))
	m.Update(key("o"))
	if m.modal == nil || m.modal.title != "Extensions" || len(m.modal.items) != len(extensions.Catalog()) {
		t.Fatal("extension catalog missing or incomplete")
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
		t.Fatalf("extension search failed: %+v", m.modal.matches)
	}
	m.Update(key("enter"))
	if m.modal != nil || m.extension == nil || m.extension.entry.ID != "hub" {
		t.Fatal("detail did not open")
	}
	if view := m.extension.view(40, 14); !strings.Contains(view, "Next:") || !strings.Contains(view, "actions") || lipgloss.Height(view) > 14 {
		t.Fatalf("narrow detail overflow or missing navigation:\n%s", view)
	}
	m.Update(key("j"))
	if m.extension.reader.YOffset() == 0 {
		t.Fatal("details did not scroll")
	}
	m.Update(key("esc"))
	if m.extension != nil || m.modal == nil || m.modal.title != "Extensions" {
		t.Fatal("escape did not return to catalog")
	}
}

func TestConversationContractActionsAndOptionalSetup(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.deps.Version = "0.4.0"
	e, err := extensions.Find("asana-sessions")
	if err != nil {
		t.Fatal(err)
	}
	m.openExtension(e)
	m.extensionActions()
	labels := map[string]string{}
	for _, item := range m.modal.items {
		labels[item.Label] = item.Hint
	}
	for _, label := range []string{"Read Conversation discovery v1 contract", "Read Conversation open v1 contract", "Build your own extension"} {
		if !strings.Contains(labels[label], "/blob/v0.4.0/docs/extensions.md#") {
			t.Errorf("%s missing release documentation: %q", label, labels[label])
		}
	}
	if labels["Set up automatic linking (optional)…"] != "asana-sessions setup" || labels["Run detailed diagnostics…"] != "asana-sessions doctor" {
		t.Fatal("history diagnostics and optional linking setup must stay separate")
	}
	if _, exists := labels["Run provider setup…"]; exists {
		t.Fatal("conversation history must not present linking setup as a prerequisite")
	}
}

func TestExtensionDetailsWrapAndKeepNavigation(t *testing.T) {
	m, _ := testModel(t, config.Default())
	for _, e := range extensions.Catalog() {
		m.openExtension(e)
		for _, width := range []int{20, 40, 76} {
			view := m.extension.view(width, 12)
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

func TestExtensionSetupCancellationAndSavedConfigIsolation(t *testing.T) {
	m, _ := testModel(t, config.Default())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(dir+"/asanamate-hrvst", []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := extensions.Find("harvest")
	m.openExtension(e)
	m.confirmExtensionSetup()
	if m.modal == nil || !strings.Contains(m.modal.notice, dir+"/asanamate-hrvst") || !strings.Contains(m.modal.items[0].Label, "asanamate-hrvst setup") {
		t.Fatal("setup confirmation must show command and resolved path")
	}
	m.Update(key("esc"))
	after, err := os.ReadFile(m.deps.ConfigPath)
	if err != nil || string(after) != string(before) || m.extension == nil || m.modal != nil {
		t.Fatalf("cancellation changed config or left details: %v", err)
	}
	if err := os.WriteFile(m.deps.ConfigPath, []byte("workspace = \"1\"\n[agents]\ncommand = \"asanamate-ccmux\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, _ = extensions.Find("ccmux")
	m.openExtension(e)
	if !m.extension.report.Configured() || m.deps.Config.Agents.Command != "" {
		t.Fatal("saved config must be inspected without changing active config")
	}
	old := m.extension
	m.openExtensions()
	m.Update(extensionCheckedMsg{detail: old, report: extensions.Report{Check: "stale"}})
	if m.extension != nil || m.modal == nil {
		t.Fatal("late check reopened closed detail")
	}
}
