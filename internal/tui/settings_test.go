package tui

import (
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

func TestSettingsTogglesPersist(t *testing.T) {
	m, st := testModel(t, config.Default())
	m.loading = false
	m.Update(key("s"))
	if m.modal == nil || m.modal.title != "Settings" {
		t.Fatal("s did not open settings")
	}
	m.Update(key("s"))
	m.Update(key("h"))
	m.Update(key("v"))
	if m.modal == nil || m.modal.cursor != 2 {
		t.Fatalf("toggle did not reopen settings on its row: %+v", m.modal)
	}
	m.Update(key("esc"))
	if m.modal != nil || !m.separator || !m.spacing || m.readerView != config.ViewMarkdown {
		t.Fatalf("separator = %v, spacing = %v, reader = %q", m.separator, m.spacing, m.readerView)
	}
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	next := New(Deps{Config: m.deps.Config, State: loaded})
	if !next.separator || !next.spacing || next.readerView != config.ViewMarkdown || next.refreshInterval != 30*time.Second {
		t.Fatalf("next session: separator = %v, spacing = %v, reader = %q, interval = %s", next.separator, next.spacing, next.readerView, next.refreshInterval)
	}
}
