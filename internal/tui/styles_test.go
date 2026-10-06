package tui

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestLipStyleMapsEveryField(t *testing.T) {
	yes := true
	s := lipStyle(config.Style{FG: "4", BG: "#000000", Bold: &yes, Italic: &yes, Underline: &yes, Faint: &yes, Reverse: &yes})
	if s.GetForeground() != lipgloss.Color("4") || s.GetBackground() != lipgloss.Color("#000000") ||
		!s.GetBold() || !s.GetItalic() || !s.GetUnderline() || !s.GetFaint() || !s.GetReverse() {
		t.Fatalf("style = %v", s)
	}
}

func TestPillReversesOnlyWithoutBackground(t *testing.T) {
	if !pillStyle(config.Style{FG: "6"}).GetReverse() {
		t.Fatal("a pill without bg must be reversed")
	}
	if pillStyle(config.Style{FG: "0", BG: "6"}).GetReverse() {
		t.Fatal("a pill with bg must not be reversed")
	}
	if !pillStyle(config.Style{FG: "6"}).GetBold() {
		t.Fatal("pills are bold unless the style says otherwise")
	}
}

func TestNewAppliesConfiguredColors(t *testing.T) {
	cfg := config.Config{Colors: config.Colors{Accent: config.Style{FG: "#7aa2f7"}, OK: config.Style{FG: "10"}, Mode: config.ModeColors{Read: config.Style{FG: "13"}}}}
	m, _ := testModel(t, cfg)
	m.loading, m.focusReader = false, true
	if m.accentStyle.GetForeground() != lipgloss.Color("#7aa2f7") || !m.accentStyle.GetBold() {
		t.Fatalf("accent = %v", m.accentStyle)
	}
	if m.markerStyle.GetForeground() != lipgloss.Color("#7aa2f7") || m.headerStyle.GetForeground() != lipgloss.Color("#7aa2f7") {
		t.Fatal("marker and header must fall back to accent")
	}
	if okStyle.GetForeground() != lipgloss.Color("10") {
		t.Fatalf("ok = %v", okStyle)
	}
	if m.pinnedStyle.GetForeground() != lipgloss.Color("208") {
		t.Fatalf("pinned = %v, want the dark theme's", m.pinnedStyle)
	}
	if _, pill, _ := m.mode(); pill.GetForeground() != lipgloss.Color("13") {
		t.Fatalf("read pill = %v", pill)
	}
}
