package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func testModel(t *testing.T, cfg config.Config) (*Model, *state.State) {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme == "" {
		cfg.Theme = "dark"
	}
	// Most tests assert on bare rows; TestSelectionMarker covers the default.
	if cfg.List.Selection.Style == "" {
		cfg.List.Selection.Style = config.StyleBar
	}
	m := New(Deps{Config: cfg, State: st, StateDir: t.TempDir()})
	m.now = func() time.Time { return testToday }
	return m, st
}

// testToday is a Monday.
var testToday = time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local)

var (
	openTask = asana.Task{GID: "1", Name: "Fix login"}
	doneTask = asana.Task{GID: "2", Name: "Write docs", Completed: true}
	sideTask = asana.Task{GID: "3", Name: "Fix footer"}
)

func TestTasksFilteredByDefault(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	_, cmd := m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	if len(m.visible) != 1 || m.visible[0].GID != "1" || m.loading {
		t.Fatalf("visible = %+v, loading = %v", m.visible, m.loading)
	}
	if cmd == nil {
		t.Fatal("want a detail fetch to be scheduled")
	}
}

func TestStaleTasksIgnored(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.viewProject = &asana.Ref{GID: "p2"}
	m.Update(tasksMsg{project: &asana.Ref{GID: "p1"}, tasks: []asana.Task{openTask}})
	if m.tasks != nil || !m.loading {
		t.Fatalf("stale response applied: tasks = %+v", m.tasks)
	}
}

func TestDetailForOtherTicketCachedNotShown(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	m.Update(detailMsg{gid: "3", ticket: ticket.Ticket{Task: sideTask}})
	if _, ok := m.details["3"]; !ok || m.shownGID != "" {
		t.Fatalf("details = %v, shown = %q", m.details, m.shownGID)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	if m.shownGID != "1" {
		t.Fatalf("shown = %q, want 1", m.shownGID)
	}
}

func TestReloadReselectsTicket(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("r"))
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask, doneTask}})
	if t2, _ := m.selected(); t2.GID != "3" {
		t.Fatalf("selected = %q, want 3 after reorder", t2.GID)
	}
	m.Update(key("r"))
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	if t2, _ := m.selected(); t2.GID != "1" {
		t.Fatalf("selected = %q, want first when ticket is gone", t2.GID)
	}
}

func TestFilterTyping(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("/"))
	for _, r := range " footer" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if m.filtering || len(m.visible) != 1 || m.visible[0].GID != "3" {
		t.Fatalf("filtering = %v, visible = %+v", m.filtering, m.visible)
	}
}

func TestProjectPickerOrder(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.RecentProjects = []string{"c", "gone"}
	m.projects = []asana.Project{{GID: "b", Name: "beta"}, {GID: "a", Name: "Alpha"}, {GID: "c", Name: "Gamma"}}
	m.openProjectPicker()
	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.Label)
	}
	if want := []string{"My Tasks", "Gamma", "Alpha", "beta"}; !slices.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

func TestPickingProjectRecordsRecent(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.projects = []asana.Project{{GID: "b", Name: "Beta"}}
	m.openProjectPicker()
	m.Update(key("down"))
	m.Update(key("enter"))
	if m.modal != nil || m.viewProject == nil || m.viewProject.GID != "b" || !m.loading {
		t.Fatalf("modal = %v, view = %v, loading = %v", m.modal, m.viewProject, m.loading)
	}
	again, _ := state.Load(st.Path())
	if len(again.RecentProjects) == 0 || again.RecentProjects[0] != "b" {
		t.Fatalf("recent = %v", again.RecentProjects)
	}
}

// historyGIDs returns the ticket history's gids, most recent first.
func historyGIDs(st *state.State) []string {
	var gids []string
	for _, r := range st.RecentTickets {
		gids = append(gids, r.GID)
	}
	return gids
}

func TestFocusingReaderRecordsHistory(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	for _, k := range []string{"j", "j", "k"} {
		m.Update(key(k))
	}
	if len(st.RecentTickets) != 0 {
		t.Fatalf("moving recorded %v", historyGIDs(st))
	}
	// Focus a, b, c, then b again, by different keys.
	for _, step := range [][]string{{"g", "2"}, {"j", "tab"}, {"j", "2"}, {"k", "shift+tab", "shift+tab"}} {
		m.Update(key("1"))
		for _, k := range step {
			m.Update(key(k))
		}
	}
	want := []string{m.visible[1].GID, m.visible[2].GID, m.visible[0].GID}
	again, _ := state.Load(st.Path())
	if got := historyGIDs(again); !slices.Equal(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

func TestPickingHistoryShowsTicket(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	st.TouchTicket(state.RecentTicket{GID: "3", Name: "Fix footer"})
	st.TouchTicket(state.RecentTicket{GID: "9", Name: "Elsewhere", Project: "Other"})

	// A ticket the view never had loads, then is kept visible and focused.
	m.Update(key("H"))
	if m.modal == nil || m.modal.title != "Ticket history" {
		t.Fatalf("modal = %+v", m.modal)
	}
	if _, cmd := m.Update(key("enter")); cmd == nil || m.openGID != "9" {
		t.Fatalf("openGID = %q", m.openGID)
	}
	far := asana.Task{GID: "9", Name: "Elsewhere"}
	m.Update(detailMsg{gid: "9", ticket: ticket.Ticket{Task: far}})
	if sel, ok := m.selected(); !ok || sel.GID != "9" || !m.focusReader || m.shownGID != "9" || m.openGID != "" {
		t.Fatalf("selected = %v, focusReader = %v, shown = %q", sel.GID, m.focusReader, m.shownGID)
	}

	// A listed ticket is selected, focused, and moves to the front.
	m.Update(key("1"))
	m.Update(key("H"))
	m.Update(key("down"))
	m.Update(key("enter"))
	if sel, _ := m.selected(); sel.GID != "3" || !m.focusReader || st.RecentTickets[0].GID != "3" {
		t.Fatalf("selected = %v, focusReader = %v, history = %v", sel.GID, m.focusReader, historyGIDs(st))
	}
}

func TestEmptyHistoryOpensNothing(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("H"))
	if m.modal != nil || m.status != "no ticket history yet" {
		t.Fatalf("modal = %v, status = %q", m.modal, m.status)
	}
}

func TestReaderViewSwitch(t *testing.T) {
	m, _ := testModel(t, config.Config{Reader: config.Reader{View: config.ViewCards}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask,
		Comments: []asana.Story{{CreatedBy: &asana.Ref{Name: "Sam"}, CreatedAt: "2026-09-20T10:00:00Z", HTMLText: "<body>Looks good</body>"}}}})
	cards := ansi.Strip(m.reader.GetContent())
	for _, want := range []string{"Details", "Comments 1", "Sam · Sep 20", "Looks good"} {
		if !strings.Contains(cards, want) {
			t.Errorf("cards view missing %q:\n%s", want, cards)
		}
	}
	m.Update(key("v"))
	if md := ansi.Strip(m.reader.GetContent()); m.readerView != config.ViewMarkdown || strings.Contains(md, "Details") || !strings.Contains(md, "Sam · 2026-09-20") {
		t.Fatalf("view = %q, content:\n%s", m.readerView, md)
	}
	m.Update(key("v"))
	if m.readerView != config.ViewCards {
		t.Fatalf("view = %q, want cards", m.readerView)
	}
}

func TestInlineImages(t *testing.T) {
	html := `<body>Look:<img data-asana-gid="9" src="https://app.asana.com/x/shot.png" alt="shot.png">done</body>`
	placeholder := string(rune(0x10EEEE))

	m, _ := testModel(t, config.Config{})
	m.deps.Images = true
	m.images["9"] = &inlineImage{id: 20, cols: 4, rows: 2}
	if body := m.renderRich(html, 60); strings.Contains(body, placeholder) || !strings.Contains(ansi.Strip(body), "shot.png") {
		t.Fatalf("inline off must keep the link:\n%s", body)
	}

	m.deps.Config.Images.Inline = true
	body := m.renderRich(html, 60)
	if strings.Count(body, placeholder) != 8 || strings.Contains(ansi.Strip(body), "shot.png") {
		t.Fatalf("inline on must draw 4x2 placeholder cells in place of the link:\n%q", body)
	}
	if !strings.Contains(body, "Look:") || !strings.Contains(body, "done") {
		t.Fatalf("text around the image is lost:\n%s", ansi.Strip(body))
	}
	lines := strings.Split(ansi.Strip(body), "\n")
	if len(lines) != 6 || strings.TrimSpace(lines[1]) != "" || strings.TrimSpace(lines[4]) != "" {
		t.Fatalf("want one blank row around the two-row image:\n%q", lines)
	}
	imageTag := `<img data-asana-gid="9" src="https://app.asana.com/x/shot.png" alt="shot.png">`
	for _, tc := range []struct {
		html      string
		blankRows []int
		rows      int
	}{
		{imageTag, []int{0, 3}, 4},
		{imageTag + "done", []int{0, 3}, 5},
		{"Look:" + imageTag, []int{1, 4}, 5},
		{imageTag + imageTag, []int{0, 3, 6}, 7},
	} {
		lines := strings.Split(ansi.Strip(m.renderRich(tc.html, 60)), "\n")
		if len(lines) != tc.rows {
			t.Fatalf("rows = %d, want %d for %q", len(lines), tc.rows, tc.html)
		}
		for _, row := range tc.blankRows {
			if strings.TrimSpace(lines[row]) != "" {
				t.Fatalf("row %d must be blank for %q", row, tc.html)
			}
		}
	}

	m.images["9"] = &inlineImage{}
	if body := m.renderRich(html, 60); strings.Contains(body, placeholder) || !strings.Contains(ansi.Strip(body), "shot.png") {
		t.Fatalf("a loading image must stay a link:\n%s", body)
	}
}

func TestImageCellSizeChanges(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.deps.Images = true
	m.deps.Config.Images.Inline = true
	m.images["9"] = &inlineImage{id: 20, cols: 4, rows: 2}
	m.Update(uv.CellSizeEvent{Width: 8, Height: 18})
	if m.imageCell != (kitty.CellSize{Width: 8, Height: 18}) || len(m.images) != 0 {
		t.Fatal("cell report must invalidate placements sized with the old estimate")
	}
	m.images["9"] = &inlineImage{}
	if cmd := m.inlineImageLoaded(inlineImageMsg{gid: "9", id: 20, cols: 4, rows: 2}); cmd != nil || m.images["9"].id != 0 {
		t.Fatal("late results sized with the old estimate must be ignored")
	}
	m.inlineImageLoaded(inlineImageMsg{gid: "9", id: 21, cols: 4, rows: 2, cell: m.imageCell})
	m.Update(uv.CellSizeEvent{Width: 8, Height: 18})
	m.Update(uv.CellSizeEvent{Width: 0, Height: 0})
	if m.images["9"].id != 21 {
		t.Fatal("unchanged or invalid cell reports must keep loaded images")
	}
}

func TestImageViewerStepsThroughImages(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.deps.Images = true
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	atts := []asana.Attachment{
		{GID: "a", Name: "one.png", Host: "asana"},
		{GID: "b", Name: "notes.pdf", Host: "asana"},
		{GID: "c", Name: "two.jpg", Host: "asana"},
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask, Attachments: atts}})
	m.pickedAttachment(atts[2])
	if len(m.viewImages) != 2 || m.viewIndex != 1 {
		t.Fatalf("images = %+v, index = %d; want the two images, at two.jpg", m.viewImages, m.viewIndex)
	}
	m.width = 60
	footer := strings.Split(ansi.Strip(m.viewerFooter()), "\n")
	if len(footer) != 2 || strings.TrimSpace(footer[0]) != "two.jpg" || !strings.Contains(footer[1], "‹ k previous    2 / 2    next j ›") {
		t.Fatalf("footer = %q", footer)
	}
	if left := len(footer[1]) - len(strings.TrimLeft(footer[1], " ")); left == 0 || left > 30 {
		t.Fatalf("footer keys are not centered: %q", footer[1])
	}
	m.Update(viewerDoneMsg{step: 1})
	if m.viewIndex != 0 {
		t.Fatalf("j on the last image: index = %d, want a wrap to 0", m.viewIndex)
	}
	m.Update(viewerDoneMsg{step: -1})
	if m.viewIndex != 1 {
		t.Fatalf("k on the first image: index = %d, want a wrap to 1", m.viewIndex)
	}
	if _, cmd := m.Update(viewerDoneMsg{}); cmd != nil || m.viewIndex != 1 {
		t.Fatal("a close key must not load another image")
	}
}

func TestNextImageIDKeepsLowByteValid(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.imageSeq = imageLows - 1
	if id := m.nextImageID(); id != lastImageLow {
		t.Fatalf("id = %#x, want %#x", id, lastImageLow)
	}
	if id := m.nextImageID(); id != 1<<24|firstImageLow {
		t.Fatalf("id = %#x, want the next high byte", id)
	}
	m.imageSeq = imageIDs
	if id := m.nextImageID(); id != firstImageLow {
		t.Fatalf("id = %#x, want ids to wrap", id)
	}
}

func TestEditedTicketStaysSelectedAfterReloadDropsIt(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	m.moveTo(1)
	m.selectionChanged()
	m.Update(editDoneMsg{gid: sideTask.GID, what: "assign"})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if got, _ := m.selected(); got.GID != sideTask.GID || !m.viewingRow || m.cursor != 0 {
		t.Fatalf("selected = %q, cursor = %d, viewingRow = %v; want the edited ticket on top", got.GID, m.cursor, m.viewingRow)
	}
	if !slices.Equal(m.groups, []string{viewingLabel, ungroupedLabel}) {
		t.Fatalf("groups = %q, want Viewing apart from the ungrouped tickets", m.groups)
	}
	m.moveTo(1)
	m.selectionChanged()
	if got, _ := m.selected(); got.GID != openTask.GID || len(m.visible) != 1 || m.viewingRow || m.groups != nil {
		t.Fatalf("selected = %q, visible = %+v, groups = %q; want Viewing dropped", got.GID, m.visible, m.groups)
	}
}
