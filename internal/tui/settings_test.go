package tui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/state"
)

func TestSettingsMenusSeparateAndRebind(t *testing.T) {
	for _, rebound := range []bool{false, true} {
		for _, menu := range []struct {
			binding, key, title string
			otherKeys           []string
		}{
			{"settings", "s", "Visual settings", []string{"a", "i", "n"}},
			{"tools", ",", "Settings & tools", []string{"s", "h", "v", "l", "d", "t", "c"}},
		} {
			name := menu.binding
			if rebound {
				name += "/rebound"
			}
			t.Run(name, func(t *testing.T) {
				cfg := config.Default()
				openKey := menu.key
				if rebound {
					var err error
					cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"main": {"settings": {"S"}, "tools": {";"}}})
					if err != nil {
						t.Fatal(err)
					}
					openKey = cfg.Keymap.Keys("main", menu.binding)[0]
				}
				m, _ := testModel(t, cfg)
				m.loading = false
				if rebound {
					m.Update(key(menu.key))
					if m.modal != nil {
						t.Fatal("default key still opens the rebound menu")
					}
				}
				m.Update(key(openKey))
				if m.modal == nil || m.modal.title != menu.title {
					t.Fatalf("%s did not open %s", openKey, menu.title)
				}
				opened := m.modal
				for _, otherKey := range menu.otherKeys {
					m.Update(key(otherKey))
					if m.modal != opened || m.input != nil || m.builder != nil {
						t.Fatalf("%s handled another menu's key %s", menu.title, otherKey)
					}
				}
				m.Update(key("esc"))
				if m.modal != nil {
					t.Fatal("escape did not close the menu")
				}
			})
		}
	}
}

func TestSettingsTogglesPersist(t *testing.T) {
	m, st := testModel(t, config.Default())
	m.loading = false
	m.Update(key("s"))
	if m.modal == nil || m.modal.title != "Visual settings" {
		t.Fatal("s did not open visual settings")
	}
	m.Update(key("s"))
	m.Update(key("h"))
	m.Update(key("d"))
	m.Update(key("v"))
	if m.modal == nil || m.modal.cursor != 2 {
		t.Fatalf("toggle did not reopen settings on its row: %+v", m.modal)
	}
	m.Update(key("esc"))
	if m.modal != nil || !m.separator || !m.spacing || m.focusDim || m.readerView != config.ViewMarkdown {
		t.Fatalf("separator = %v, spacing = %v, dim = %v, reader = %q", m.separator, m.spacing, m.focusDim, m.readerView)
	}
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	next := New(Deps{Config: reloadSettings(t, m), State: loaded})
	if !next.separator || !next.spacing || next.focusDim || next.readerView != config.ViewMarkdown || next.refreshInterval != 30*time.Second {
		t.Fatalf("next session: separator = %v, spacing = %v, dim = %v, reader = %q, interval = %s", next.separator, next.spacing, next.focusDim, next.readerView, next.refreshInterval)
	}
	if !m.deps.Config.Layout.Focus.Dim {
		t.Fatal("dim toggle changed config defaults")
	}
}

func TestSelectionSettingsToggleAndPersist(t *testing.T) {
	for _, initial := range []string{config.StyleMarker, config.StyleBar} {
		t.Run(initial, func(t *testing.T) {
			cfg := config.Default()
			cfg.List.Selection.Style = initial
			cfg.List.Layout = config.LayoutSingle
			m, st := testModel(t, cfg)
			m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", Name: "A"}}})
			m.Update(key("s"))
			want := initial
			for range 2 {
				if want == config.StyleMarker {
					want = config.StyleBar
				} else {
					want = config.StyleMarker
				}
				m.Update(key("l"))
				if m.modal == nil || m.modal.cursor != 3 || m.modal.items[3].Hint != want {
					t.Fatalf("selection toggle did not reopen on its updated row: %+v", m.modal)
				}
				row := strings.Split(m.listView(20, 1), "\n")[0]
				if want == config.StyleMarker {
					if !strings.HasPrefix(row, m.markerStyle.Render(m.sym.cursor+" ")) || m.selectionBar() {
						t.Fatalf("marker did not apply immediately: %q", row)
					}
				} else if strings.Contains(ansi.Strip(row), m.sym.cursor) || !m.selectionBar() {
					t.Fatalf("bar did not apply immediately: %q", row)
				}
				// Saving another visual option must retain the selection style.
				m.Update(key("h"))
				loaded, err := state.Load(st.Path())
				if err != nil {
					t.Fatal(err)
				}
				next := New(Deps{Config: reloadSettings(t, m), State: loaded})
				if loaded.LegacyDisplay != (state.LegacyDisplay{}) || next.selectionStyle != want {
					t.Fatalf("selection style did not persist in settings: state = %+v, next = %q, want %q", loaded.LegacyDisplay, next.selectionStyle, want)
				}
				if m.deps.Config.List.Selection.Style != initial {
					t.Fatal("selection toggle changed config defaults")
				}
			}
		})
	}
}

func TestSelectionSettingsFallback(t *testing.T) {
	for _, style := range []string{config.StyleMarker, config.StyleBar} {
		for _, saved := range []string{"", "invalid", config.StyleBar, config.StyleMarker} {
			t.Run(style+"/"+saved, func(t *testing.T) {
				cfg := config.Default()
				cfg.List.Selection.Style = style
				_, st := testModel(t, cfg)
				st.LegacyDisplay.SelectionStyle = saved
				m := New(Deps{Config: cfg, State: st})
				if m.selectionStyle != style {
					t.Fatalf("saved %q: selection style = %q, want config default %q", saved, m.selectionStyle, style)
				}
			})
		}
	}
}

func reloadSettings(t *testing.T, m *Model) config.Config {
	t.Helper()
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSettingsLeaveStateUntouched(t *testing.T) {
	m, st := testModel(t, config.Default())
	m.loading = false
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, keyName := range []string{"s", "s", "h", "d", "v", "l", "esc", ",", "i"} {
		m.Update(key(keyName))
	}
	if m.input == nil {
		t.Fatal("settings interval picker did not open")
	}
	m.input.area.SetValue("20s")
	m.Update(key("ctrl+s"))
	after, err := os.ReadFile(st.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("settings changed state: %v", err)
	}
	cfg := reloadSettings(t, m)
	if cfg.List.RefreshInterval != "20s" || cfg.List.Selection.Style != config.StyleBar {
		t.Fatalf("settings = %+v", cfg.List)
	}
}

func TestSettingsSaveErrorKeepsSessionChoice(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.loading = false
	// A directory cannot be read or replaced as settings.toml.
	if err := os.Mkdir(config.SettingsPath(m.deps.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	m.Update(key("s"))
	m.Update(key("s"))
	if !m.separator || !strings.Contains(m.status, "saving settings:") {
		t.Fatalf("separator = %v, status = %q", m.separator, m.status)
	}
}

func TestReaderViewKeySavesSetting(t *testing.T) {
	m, st := testModel(t, config.Default())
	m.loading = false
	m.width, m.height = 120, 40
	m.Update(key("v"))
	if m.readerView != config.ViewMarkdown || reloadSettings(t, m).Reader.View != config.ViewMarkdown {
		t.Fatal("reader_view did not save settings")
	}
	if _, err := os.Stat(st.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reader_view wrote state: %v", err)
	}
}
