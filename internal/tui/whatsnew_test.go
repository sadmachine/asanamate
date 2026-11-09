package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/whatsnew"
)

var whatsNewFixture = []whatsnew.Section{
	{Version: "v0.5.0", Body: "### New\n\n- **Five.** five"},
	{Version: "v0.4.1", Body: "### New\n\n- **Four-one.** four-one"},
	{Version: "v0.4.0", Body: "### New\n\n- **Four.** four"},
}

// whatsNewModel builds a model whose state file holds stateBody; an empty
// body leaves no file, as on a fresh install.
func whatsNewModel(t *testing.T, version, stateBody string, cfg config.Config, ticket string) (*Model, *state.State) {
	t.Helper()
	path := filepath.Join(t.TempDir(), state.FileName)
	if stateBody != "" {
		if err := os.WriteFile(path, []byte(stateBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return newWhatsNewModel(t, version, st, cfg, ticket), st
}

func newWhatsNewModel(t *testing.T, version string, st *state.State, cfg config.Config, ticket string) *Model {
	t.Helper()
	m, _ := testModel(t, cfg)
	configPath := m.deps.ConfigPath
	return New(Deps{ConfigPath: configPath, Config: cfg, State: st, StateDir: t.TempDir(), Version: version, WhatsNew: whatsNewFixture, Ticket: ticket})
}

// whatsNewRows lists the modal's rows, version headers and entry titles.
func whatsNewRows(m *Model) string {
	var rows []string
	for _, it := range m.whatsNew.list.items {
		rows = append(rows, it.Label)
	}
	return strings.Join(rows, ",")
}

func lastSeen(t *testing.T, st *state.State) string {
	t.Helper()
	loaded, err := state.Load(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	return loaded.LastSeenVersion
}

func TestWhatsNewOpensOnceAfterUpgrade(t *testing.T) {
	m, st := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "")
	if m.whatsNew == nil {
		t.Fatal("modal did not open after an upgrade")
	}
	if got := whatsNewRows(m); got != "v0.5.0,Five,v0.4.1,Four-one" {
		t.Fatalf("rows = %q", got)
	}
	m.Update(key("esc"))
	if m.whatsNew != nil {
		t.Fatal("esc did not close the modal")
	}
	if got := lastSeen(t, st); got != "v0.5.0" {
		t.Fatalf("last seen = %q", got)
	}
	loaded, _ := state.Load(st.Path())
	if again := newWhatsNewModel(t, "v0.5.0", loaded, config.Default(), ""); again.whatsNew != nil {
		t.Fatal("modal opened twice for one release")
	}
}

func TestWhatsNewExistingUserFirstRelease(t *testing.T) {
	m, _ := whatsNewModel(t, "v0.5.0", "recent_projects = []\n", config.Default(), "")
	if m.whatsNew == nil || whatsNewRows(m) != "v0.5.0,Five" {
		t.Fatalf("modal = %+v", m.whatsNew)
	}
}

func TestWhatsNewFreshInstallRecordsSilently(t *testing.T) {
	m, st := whatsNewModel(t, "v0.5.0", "", config.Default(), "")
	if m.whatsNew != nil {
		t.Fatal("modal opened on a fresh install")
	}
	if got := lastSeen(t, st); got != "v0.5.0" {
		t.Fatalf("last seen = %q", got)
	}
}

func TestWhatsNewSkipsDevAndDowngrade(t *testing.T) {
	for _, version := range []string{"", "dev", "v0.4.0"} {
		t.Run(version, func(t *testing.T) {
			m, st := whatsNewModel(t, version, "last_seen_version = \"v0.5.0\"\n", config.Default(), "")
			if m.whatsNew != nil || lastSeen(t, st) != "v0.5.0" {
				t.Fatalf("modal = %v, last seen = %q", m.whatsNew != nil, lastSeen(t, st))
			}
		})
	}
}

func TestWhatsNewSettingOffRecordsSilently(t *testing.T) {
	cfg := config.Default()
	cfg.WhatsNew.OnStartup = false
	m, st := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", cfg, "")
	if m.whatsNew != nil || lastSeen(t, st) != "v0.5.0" {
		t.Fatalf("modal = %v, last seen = %q", m.whatsNew != nil, lastSeen(t, st))
	}
}

func TestWhatsNewNoEntriesRecordsSilently(t *testing.T) {
	m, st := whatsNewModel(t, "v0.4.2", "last_seen_version = \"v0.4.1\"\n", config.Default(), "")
	if m.whatsNew != nil || lastSeen(t, st) != "v0.4.2" {
		t.Fatalf("modal = %v, last seen = %q", m.whatsNew != nil, lastSeen(t, st))
	}
}

func TestWhatsNewDontShowTurnsOffSetting(t *testing.T) {
	m, st := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "")
	m.Update(key("d"))
	if m.whatsNew != nil || m.deps.Config.WhatsNew.OnStartup {
		t.Fatalf("modal = %v, on_startup = %v", m.whatsNew != nil, m.deps.Config.WhatsNew.OnStartup)
	}
	if reloadSettings(t, m).WhatsNew.OnStartup {
		t.Fatal("on_startup not saved")
	}
	if got := lastSeen(t, st); got != "v0.5.0" {
		t.Fatalf("last seen = %q", got)
	}
}

func TestWhatsNewReadModeSkips(t *testing.T) {
	m, st := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "1")
	if m.whatsNew != nil || lastSeen(t, st) != "v0.4.0" {
		t.Fatalf("modal = %v, last seen = %q", m.whatsNew != nil, lastSeen(t, st))
	}
}

func TestWhatsNewToolsMenu(t *testing.T) {
	m, _ := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.5.0\"\n", config.Default(), "")
	m.loading = false
	m.Update(key(","))
	m.Update(key("e"))
	if m.whatsNew == nil || !strings.HasSuffix(whatsNewRows(m), "v0.4.0,Four") {
		t.Fatalf("modal = %+v", m.whatsNew)
	}
	m.Update(key("esc"))
	m.Update(key(","))
	m.Update(key("u"))
	if m.deps.Config.WhatsNew.OnStartup || reloadSettings(t, m).WhatsNew.OnStartup {
		t.Fatal("u did not turn off on_startup")
	}
}

func TestWhatsNewSaveErrorStillCloses(t *testing.T) {
	dir := t.TempDir()
	st, err := state.Load(filepath.Join(dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	st.LastSeenVersion = "v0.4.0"
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	m, _ := testModel(t, config.Default())
	m.deps.State, m.deps.Version, m.deps.WhatsNew = st, "v0.5.0", whatsNewFixture
	m.openWhatsNew(whatsnew.Since(whatsNewFixture, "v0.5.0", "v0.4.0"))
	m.Update(key("esc"))
	if m.whatsNew != nil {
		t.Fatal("modal stayed open after a save error")
	}
	if m.statusLevel != statusError || !strings.Contains(m.status, "state") {
		t.Fatalf("status = %q (%v)", m.status, m.statusLevel)
	}
}

func TestWhatsNewTinyPane(t *testing.T) {
	m, _ := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "")
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "▸ Five") {
		t.Fatalf("view = %q", view)
	}
}

func TestWhatsNewListsTitlesBesideTheHighlightedEntry(t *testing.T) {
	m, _ := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openWhatsNew([]whatsnew.Section{
		{Version: "v0.5.0", Body: "### Changed\n\n- **Wrapped.** one two\n  three four\n- **Next.** five"},
		{Version: "v0.4.0", Body: "### New\n\n- **Old.** six"},
	})
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"v0.5.0", "v0.4.0", "▸ Wrapped", "Next", "Old", "v0.5.0 · Changed", "one two three four", "esc got it"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	for _, markup := range []string{"##", "**", "five"} {
		if strings.Contains(view, markup) {
			t.Fatalf("view shows %q:\n%s", markup, view)
		}
	}
	m.Update(key("j"))
	m.Update(key("j"))
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "▸ Old") || !strings.Contains(view, "six") {
		t.Fatalf("j did not skip the header to Old:\n%s", view)
	}
}

func TestWhatsNewSearchTypesD(t *testing.T) {
	m, _ := whatsNewModel(t, "v0.5.0", "last_seen_version = \"v0.4.0\"\n", config.Default(), "")
	m.Update(key("/"))
	m.Update(key("d"))
	if m.whatsNew == nil || !m.deps.Config.WhatsNew.OnStartup || m.whatsNew.list.input.Value() != "d" {
		t.Fatalf("d while searching must type: modal = %v, on_startup = %v", m.whatsNew != nil, m.deps.Config.WhatsNew.OnStartup)
	}
}

func TestSplitWhatsNewEntry(t *testing.T) {
	for _, tc := range []struct{ md, title, body string }{
		{"- **`ASANAMATE_X` takes true.** It does.", "ASANAMATE_X takes true", "It does."},
		{"- No bold title here.\nMore.", "No bold title here.", "No bold title here.\nMore."},
	} {
		if title, body := splitWhatsNewEntry(tc.md); title != tc.title || body != tc.body {
			t.Fatalf("splitWhatsNewEntry(%q) = %q, %q", tc.md, title, body)
		}
	}
}
