package tui

import (
	"slices"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

// paletteLabels opens the command palette and returns its rows' labels.
func paletteLabels(t *testing.T, m *Model) []string {
	t.Helper()
	press(m, ":")
	if m.modal == nil || m.modal.title != "Commands" || !m.modal.searching {
		t.Fatalf("':' must open the command palette searching, got %+v", m.modal)
	}
	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.Label)
	}
	return labels
}

func TestPaletteListsCommandsAndActions(t *testing.T) {
	m, _ := editModel(t)
	m.deps.Config.Actions = []config.Action{goAction}
	labels := paletteLabels(t, m)
	for _, want := range []string{"add comment / return to it", "set due date", "copy link", "Action: Go"} {
		if !slices.Contains(labels, want) {
			t.Errorf("palette lacks %q: %q", want, labels)
		}
	}
	for _, skip := range []string{"down / next card target", "command palette"} {
		if slices.Contains(labels, skip) {
			t.Errorf("palette lists %q", skip)
		}
	}
	i := slices.Index(labels, "set due date")
	if hint := m.modal.items[i].Hint; hint != "d" {
		t.Fatalf("set due date hint = %q, want its key", hint)
	}
}

func TestPaletteRunsTheCommandLikeItsKey(t *testing.T) {
	m, _ := editModel(t)
	press(m, ":")
	typeText(m.modal, "due date")
	press(m, "enter")
	if m.modal != nil || m.input == nil || m.input.title != "Due date" {
		t.Fatalf("picking set due date must open the due date input, as d does: modal %v, input %+v", m.modal, m.input)
	}
}

func TestPaletteRunsUnboundCommands(t *testing.T) {
	m, _ := editModel(t)
	press(m, ":")
	typeText(m.modal, "copy link")
	press(m, "enter")
	if m.modal != nil || m.status != "ticket has no link" {
		t.Fatalf("copy link, which has no key, must run: modal %v, status %q", m.modal, m.status)
	}
}

func TestPaletteLeavesOutCommandsTheLayoutLacks(t *testing.T) {
	m := ticketOnlyModel(t)
	if labels := paletteLabels(t, m); slices.Contains(labels, "filter") || !slices.Contains(labels, "set due date") {
		t.Fatalf("ticket-only palette = %q, want ticket commands without list ones", labels)
	}
}
