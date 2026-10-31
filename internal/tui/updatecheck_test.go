package tui

import (
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestUpdateAvailableNotice(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(updateAvailableMsg{version: "v0.10.0"})
	if m.toast == nil || m.statusLevel != statusInfo || len(m.notices) != 0 {
		t.Fatal("update must show an informational toast without taking focus")
	}
	for _, want := range []string{"v0.10.0", "https://github.com/sadmachine/asanamate#upgrading"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("notice %q missing %q", m.status, want)
		}
	}
	if len(m.statusHistory) != 1 || m.statusHistory[0].text != m.status {
		t.Fatal("update notice missing from status history")
	}
}

func TestEmptyUpdateCheckLeavesStatusAlone(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.setStatus(statusInfo, "Saved")
	m.Update(updateAvailableMsg{})
	if m.status != "Saved" || len(m.statusHistory) != 1 {
		t.Fatal("unsuccessful or up-to-date check changed status")
	}
}

func TestDevelopmentUpdateCheckIsSilent(t *testing.T) {
	msg := checkForUpdate("dev")().(updateAvailableMsg)
	if msg.version != "" {
		t.Fatalf("development check = %+v", msg)
	}
}
