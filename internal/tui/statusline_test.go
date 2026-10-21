package tui

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func splitModel(t *testing.T) *Model {
	t.Helper()
	m, _ := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	return m
}

func TestStatuslineShowsModeAndHints(t *testing.T) {
	m := splitModel(t)
	for _, tc := range []struct {
		key, want string
	}{
		{"", " NORMAL  My Tasks │ 2/2"},
		{"/", " FILTER "},
		{"esc", " NORMAL "},
		{"tab", " EDIT "},
		{"esc", " NORMAL "},
	} {
		if tc.key != "" {
			m.Update(key(tc.key))
		}
		if s := ansi.Strip(m.statusline()); !strings.HasPrefix(s, tc.want) {
			t.Fatalf("after %q: statusline = %q", tc.key, s)
		}
	}
	if s := ansi.Strip(m.statusline()); !strings.HasSuffix(s, "? keys") || ansi.StringWidth(s) != 120 {
		t.Fatalf("statusline = %q", s)
	}
}

func TestStatusMessageReplacesHints(t *testing.T) {
	m := splitModel(t)
	m.setStatus(statusInfo, "Comment: done")
	s := ansi.Strip(m.statusline())
	if !strings.HasSuffix(s, "Comment: done") || strings.Contains(s, "? keys") {
		t.Fatalf("statusline = %q", s)
	}
}

func TestStatusMessageColorsAndIcons(t *testing.T) {
	m := splitModel(t)
	defer applyPalette(config.Config{}.Palette(true))
	for _, dark := range []bool{true, false} {
		palette := config.Config{}.Palette(dark)
		palette.Colors.Accent = config.Style{FG: "#123456"}
		palette.Colors.Warn = config.Style{FG: "#abcdef"}
		palette.Colors.Error = config.Style{FG: "#fedcba"}
		m.setPalette(palette)
		for _, tc := range []struct {
			symbols symbolSet
			icons   [3]string
		}{
			{asciiSymbols, [3]string{"(i)", "(!)", "(x)"}},
			{unicodeSymbols, [3]string{"(i)", "⚠", "✖"}},
			{nerdSymbols, [3]string{"\U000f02fd", "\uf071", "\U000f05d6"}},
		} {
			m.sym = tc.symbols
			for level, style := range []lipgloss.Style{m.accentStyle, warnStyle, errorStyle} {
				// Isolate each rendered status; errors otherwise remain queued.
				m.notices = nil
				// The words do not determine the severity.
				m.setStatus(statusLevel(level), "failed: warning about errors")
				view := m.statusline()
				want := style.Render(tc.icons[level] + " " + m.status)
				if !strings.HasSuffix(view, want) || strings.Contains(ansi.Strip(view), "? keys") {
					t.Fatalf("dark=%v, level=%d: statusline = %q, want suffix %q", dark, level, view, want)
				}
				if ansi.StringWidth(view) != m.width {
					t.Fatalf("statusline width = %d, want %d", ansi.StringWidth(view), m.width)
				}
			}
		}
	}
}

func TestStatusMessageSeverityFromEvents(t *testing.T) {
	m := splitModel(t)
	m.Update(tasksMsg{err: errors.New("offline")})
	if m.statusLevel != statusError || !strings.HasSuffix(ansi.Strip(m.statusline()), "✖ loading tasks: offline") {
		t.Fatalf("load error status = %q, level = %d", m.status, m.statusLevel)
	}
	m.Update(key("enter"))
	m.Update(tasksMsg{tasks: []asana.Task{openTask}, warning: "some pinned tickets unavailable"})
	if m.statusLevel != statusWarning || !strings.HasSuffix(ansi.Strip(m.statusline()), "⚠ some pinned tickets unavailable") {
		t.Fatalf("load warning status = %q, level = %d", m.status, m.statusLevel)
	}
	m.Update(statusMsg("browser unavailable"))
	if m.statusLevel != statusError {
		t.Fatalf("browser error level = %d", m.statusLevel)
	}
	m.Update(key("enter"))
	m.Update(actionDoneMsg{name: "Export"})
	if m.statusLevel != statusInfo || !strings.HasSuffix(ansi.Strip(m.statusline()), "(i) Export: done") {
		t.Fatalf("success status = %q, level = %d", m.status, m.statusLevel)
	}
	m.Update(key("j"))
	if m.status != "Export: done" || m.statusLevel != statusInfo || !strings.HasSuffix(ansi.Strip(m.statusline()), "(i) Export: done") {
		t.Fatalf("navigation lost latest status = %q, level = %d", m.status, m.statusLevel)
	}
}

func TestStatusNoticeKeepsSeverityWhileQueued(t *testing.T) {
	m := splitModel(t)
	m.setStatus(statusError, "first failure")
	m.Update(actionDoneMsg{name: "Export", err: errors.New("offline")})
	m.Update(actionDoneMsg{name: "Copy"})
	if !strings.HasSuffix(ansi.Strip(m.statusline()), "✖ first failure") {
		t.Fatalf("first notice = %q", ansi.Strip(m.statusline()))
	}
	m.Update(key("enter"))
	if !strings.HasSuffix(ansi.Strip(m.statusline()), "✖ Export failed: offline") {
		t.Fatalf("second notice = %q", ansi.Strip(m.statusline()))
	}
	m.Update(key("enter"))
	if !strings.HasSuffix(ansi.Strip(m.statusline()), "(i) Copy: done") {
		t.Fatalf("status after notices = %q", ansi.Strip(m.statusline()))
	}
}

func TestStatusMessageTruncation(t *testing.T) {
	m := splitModel(t)
	for _, symbols := range []symbolSet{asciiSymbols, unicodeSymbols, nerdSymbols} {
		m.sym = symbols
		for _, level := range []statusLevel{statusInfo, statusWarning, statusError} {
			m.notices = nil
			m.setStatus(level, strings.Repeat("長い status ", 20)+"\nsecond line")
			for _, width := range []int{0, 1, 8, 16, 30, 120} {
				m.width = width
				view := m.statusline()
				if ansi.StringWidth(view) > width || strings.ContainsAny(view, "\r\n") {
					t.Fatalf("width=%d, level=%d: statusline = %q", width, level, view)
				}
				if width >= 30 && !strings.Contains(ansi.Strip(view), ansi.Strip(m.renderStatus(level, "長い"))) {
					t.Fatalf("width=%d: status message lost priority: %q", width, ansi.Strip(view))
				}
			}
		}
	}
}

func TestHelpOpensAndAnyKeyCloses(t *testing.T) {
	m := splitModel(t)
	m.Update(key("?"))
	if !m.help || !regexp.MustCompile(`esc/1 +list`).MatchString(ansi.Strip(m.body())) {
		t.Fatalf("help = %v, body = %q", m.help, ansi.Strip(m.body()))
	}
	m.Update(key("j"))
	if m.help || m.cursor != 0 {
		t.Fatalf("help = %v, cursor = %d: the closing key must not also move", m.help, m.cursor)
	}
}

func TestFilterGuideLayout(t *testing.T) {
	m := splitModel(t)
	m.Update(key("/"))
	for _, size := range []tea.WindowSizeMsg{
		{Width: 120, Height: 20},
		{Width: 45, Height: 20},
		{Width: 45, Height: 7},
	} {
		m.Update(size)
		hints := m.filterHints()
		plain := ansi.Strip(strings.Join(hints, " "))
		for _, want := range []string{"section:", "project:", "assignee:", "tag:", "is:", "agent:", "ctrl+/ guide"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d guide lacks %q: %q", size.Width, size.Height, want, plain)
			}
		}
		for _, line := range hints {
			if width := ansi.StringWidth(line); width > size.Width {
				t.Errorf("%dx%d guide line width = %d", size.Width, size.Height, width)
			}
		}
		if !strings.Contains(ansi.Strip(m.body()), "ctrl+/ guide") {
			t.Errorf("%dx%d body lacks guide", size.Width, size.Height)
		}
	}
	m.Update(key("esc"))
	if m.filtering || len(m.filterHints()) != 0 || strings.Contains(ansi.Strip(m.body()), "FILTER BY") {
		t.Fatal("filter guide remains after editing")
	}
}

func TestFilterHelpReturnsToEditing(t *testing.T) {
	m := splitModel(t)
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 20})
	m.filterInput.SetValue("is:open")
	m.applyFilter()
	m.Update(key("/"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(key("ctrl+/"))
	if !m.help || !m.filtering || m.filterInput.Value() != "is:open" || !strings.Contains(ansi.Strip(m.body()), "agent:any") {
		t.Fatal("filter help did not open over the current query")
	}
	for _, line := range strings.Split(m.filterHelpView(), "\n") {
		if width := ansi.StringWidth(line); width > m.width-6 {
			t.Errorf("filter help line too wide: %d cells: %q", width, ansi.Strip(line))
		}
	}
	if height := strings.Count(m.filterHelpView(), "\n") + 3; height > m.bodyHeight() {
		t.Errorf("filter help height = %d, body height = %d", height, m.bodyHeight())
	}
	m.Update(key("j"))
	if m.help || !m.filtering || m.filterInput.Value() != "is:open" || m.cursor != 0 {
		t.Fatal("closing filter help changed the query or selection")
	}
	m.Update(key("x"))
	if got := m.filterInput.Value(); got != "is:opexn" {
		t.Fatalf("filter cursor moved while help was open: %q", got)
	}
	m.Update(key("enter"))
	if m.filtering || m.filterInput.Value() != "is:opexn" {
		t.Fatal("filter editing did not finish with the query intact")
	}
	m.Update(key("?"))
	if !m.help || !strings.Contains(ansi.Strip(m.body()), "Keys") {
		t.Fatal("key help did not open outside filter editing")
	}
}

func TestEmptyListExplainsAndHints(t *testing.T) {
	m := splitModel(t)
	m.filterInput.SetValue("tag:nope")
	m.applyFilter()
	body := ansi.Strip(m.listView(60, 10))
	for _, want := range []string{"All clear.", "Nothing matches tag:nope", "/ filter", "p projects"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestFirstLoadShowsPlaceholderRows(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	body := ansi.Strip(m.body())
	lines := strings.Split(body, "\n")
	if !strings.Contains(lines[0], " loading ") || !strings.Contains(lines[1], "▆ ▆▆▆") || strings.Contains(body, "Loading tasks") {
		t.Fatalf("first load:\n%s", body)
	}
	if s := ansi.Strip(m.statusline()); !strings.HasSuffix(s, "q quit") || strings.Contains(s, "enter") {
		t.Fatalf("first load statusline = %q", s)
	}
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.reload()
	if body := ansi.Strip(m.body()); !strings.Contains(body, "Loading tasks") || !strings.Contains(body, "Fix login") {
		t.Fatalf("a reload keeps the list under the loading modal:\n%s", body)
	}
}

func TestFilterHelpOpensOnCtrlUnderscore(t *testing.T) {
	m := splitModel(t)
	m.Update(key("/"))
	m.Update(key("ctrl+_"))
	if !m.help || !m.filtering {
		t.Fatal("ctrl+_ must open the filter guide like ctrl+/")
	}
}

func TestFilterTypesQuestionMark(t *testing.T) {
	m := splitModel(t)
	m.Update(key("/"))
	m.Update(key("?"))
	if m.help || m.filterInput.Value() != "?" {
		t.Fatalf("help = %v, filter = %q", m.help, m.filterInput.Value())
	}
}

func TestFilterEscRestoresPreviousFilter(t *testing.T) {
	m := splitModel(t)
	m.filterInput.SetValue("is:open")
	m.applyFilter()
	m.Update(key("/"))
	m.Update(key("x"))
	m.Update(key("esc"))
	if m.filtering || m.filterInput.Value() != "is:open" || len(m.visible) != 1 {
		t.Fatalf("filtering = %v, filter = %q, visible = %d", m.filtering, m.filterInput.Value(), len(m.visible))
	}
}

func TestUnboundKeysLeaveNoEmptyHints(t *testing.T) {
	cfg := config.Config{}
	var err error
	if cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"filter": {"help": {}}, "main": {"filter": {}}}); err != nil {
		t.Fatal(err)
	}
	m, _ := testModel(t, cfg)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.filtering = true
	if s := ansi.Strip(m.statusline()); strings.Contains(s, "  guide") || strings.HasSuffix(s, " guide") {
		t.Fatalf("statusline shows an unbound key: %q", s)
	}
	for _, line := range m.filterHints() {
		if strings.Contains(ansi.Strip(line), "guide") {
			t.Fatalf("filter hints show an unbound guide key: %q", ansi.Strip(line))
		}
	}
	m.filtering = false
	m.tasks = nil
	if strings.Contains(ansi.Strip(m.emptyView(80, 10)), " filter") {
		t.Fatal("empty view shows an unbound filter key")
	}
}

func TestToastLifecycleAndReplacement(t *testing.T) {
	m := splitModel(t)
	var delays []time.Duration
	m.toastTimer = func(after time.Duration, msg toastTickMsg) tea.Cmd {
		delays = append(delays, after)
		return func() tea.Msg { return msg }
	}
	_, cmd := m.Update(actionDoneMsg{name: "Export"})
	if cmd == nil || m.toast == nil || !m.toast.highlighted || len(m.notices) != 0 {
		t.Fatal("info must start a highlighted, nonblocking toast timer")
	}
	if len(delays) != 1 || delays[0] != toastHighlight {
		t.Fatalf("initial highlight delay = %v", delays)
	}
	old := m.toast.seq
	m.Update(key("j"))
	if m.cursor != 1 || m.toast == nil || m.status != "Export: done" {
		t.Fatal("toast intercepted movement or lost the latest status")
	}
	m.setStatus(statusWarning, "partial result")
	seq := m.toast.seq
	m.Update(toastTickMsg{seq: old, finish: true})
	if m.toast == nil || m.toast.seq != seq {
		t.Fatal("old timer removed a newer toast")
	}
	_, cmd = m.Update(toastTickMsg{seq: seq})
	if cmd == nil || m.toast == nil || m.toast.highlighted {
		t.Fatal("highlight must settle and schedule toast expiry")
	}
	if delays[len(delays)-1] != 6*time.Second-toastHighlight {
		t.Fatalf("warning expiry delay = %v", delays)
	}
	m.Update(toastTickMsg{seq: seq, finish: true})
	if m.toast != nil || m.status != "partial result" || m.statusLevel != statusWarning {
		t.Fatal("expiry must leave the latest message in the statusline")
	}
	if toastDuration(statusInfo) != 4*time.Second || toastDuration(statusWarning) != 6*time.Second {
		t.Fatal("unexpected toast duration")
	}
}

func TestToastReducedMotionAndProgress(t *testing.T) {
	m := splitModel(t)
	m.deps.ReducedMotion = true
	var delay time.Duration
	m.toastTimer = func(after time.Duration, msg toastTickMsg) tea.Cmd {
		delay = after
		return func() tea.Msg { return msg }
	}
	m.Update(actionDoneMsg{name: "Export"})
	if m.toast == nil || m.toast.highlighted || delay != 4*time.Second {
		t.Fatal("reduced motion must skip the border highlight")
	}
	seq := m.toast.seq
	m.setProgress("refreshing…")
	if len(m.statusHistory) != 1 || m.toast.seq != seq {
		t.Fatal("routine progress must not create a toast or history entry")
	}
	m.clearProgress()
	if m.status != "Export: done" || m.statusProgress {
		t.Fatal("clearing progress must restore the latest meaningful message")
	}
	m.Update(toastTickMsg{seq: seq, finish: true})
	if m.toast != nil || m.status != "Export: done" {
		t.Fatal("static toast did not expire cleanly")
	}
}

func TestToastFitsWithoutChangingLayout(t *testing.T) {
	m := splitModel(t)
	for _, symbols := range []symbolSet{asciiSymbols, unicodeSymbols, nerdSymbols} {
		m.sym = symbols
		for _, size := range [][2]int{{8, 3}, {12, 5}, {32, 10}, {60, 20}, {120, 20}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.toast = nil
			before := m.View().Content
			m.setStatus(statusWarning, strings.Repeat("Long message 長い with no spaces: "+strings.Repeat("x", 60), 5))
			view := m.View().Content
			if len(strings.Split(view, "\n")) != len(strings.Split(before, "\n")) {
				t.Fatalf("%v: toast changed the layout height", size)
			}
			for _, line := range strings.Split(view, "\n") {
				if width := ansi.StringWidth(line); width > size[0] {
					t.Fatalf("%v: toast line width = %d: %q", size, width, ansi.Strip(line))
				}
			}
			if size[0] >= 12 && size[1] >= 5 && !strings.Contains(ansi.Strip(view), "…") {
				t.Fatalf("%v: clipped toast must indicate more text", size)
			}
		}
	}
}

func TestToastCoversStatuslineUntilExpiry(t *testing.T) {
	for _, message := range []string{"Ticket saved", "Ticket saved: " + strings.Repeat("details ", 30)} {
		m := splitModel(t)
		m.setStatus(statusInfo, message)
		underneath := m.statusline()
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		if strings.Contains(lines[len(lines)-1], "Ticket saved") {
			t.Fatal("toast must cover the statusline message")
		}
		if strings.Count(view, "Ticket saved") != 1 {
			t.Fatal("message must appear only in the toast, including long messages")
		}
		if !strings.Contains(lines[len(lines)-1], "──") {
			t.Fatal("toast bottom border must occupy the statusline row")
		}
		m.Update(toastTickMsg{seq: m.toast.seq, finish: true})
		view = m.View().Content
		lines = strings.Split(view, "\n")
		if lines[len(lines)-1] != underneath {
			t.Fatal("expiry must restore the underlying statusline exactly")
		}
	}
}

func TestErrorsRequireAcknowledgmentAndWarningsDoNot(t *testing.T) {
	m := splitModel(t)
	m.Update(actionDoneMsg{name: "Export", err: errors.New("offline")})
	if !m.noticeVisible() || m.toast != nil || !strings.Contains(ansi.Strip(m.body()), "Error") {
		t.Fatal("error must show a modal without a duplicate toast")
	}
	m.Update(key("j"))
	if m.cursor != 0 {
		t.Fatal("error acknowledgment key must not move the list")
	}
	m.Update(key("enter"))
	m.setStatus(statusWarning, "no running agent")
	m.Update(key("j"))
	if len(m.notices) != 0 || m.toast == nil || m.cursor != 1 {
		t.Fatal("warning must stay nonblocking")
	}
}

func TestUnrelatedErrorsWaitForEditor(t *testing.T) {
	m := splitModel(t)
	b := newInputBox("Draft", "")
	b.area.SetValue("keep this")
	m.input = b
	m.Update(statusMsg("unrelated browser failure"))
	if len(m.notices) != 1 || m.noticeVisible() || m.input != b {
		t.Fatal("unrelated error interrupted editor")
	}
	before := b.area.Value()
	m.Update(key("x"))
	if b.area.Value() == before || m.input != b || m.noticeVisible() {
		t.Fatal("deferred notice consumed editor input")
	}
	m.Update(key("esc"))
	if m.input != nil || !m.noticeVisible() {
		t.Fatal("deferred error must appear after editor closes")
	}
}

func TestInlineErrorsDoNotQueueNotices(t *testing.T) {
	m := splitModel(t)
	m.setInlineError("logging time: offline")
	if len(m.notices) != 0 || m.toast != nil || len(m.statusHistory) != 1 || m.statusLevel != statusError {
		t.Fatal("inline error must enter history without a second modal")
	}
}

func TestBackgroundErrorsRearmAfterRecovery(t *testing.T) {
	m := splitModel(t)
	failure := tasksMsg{err: errors.New("offline")}
	m.Update(failure)
	m.Update(failure)
	if len(m.notices) != 1 || len(m.statusHistory) != 1 || m.statusHistory[0].count != 2 {
		t.Fatal("repeated background failure must share a notice and history entry")
	}
	m.Update(key("enter"))
	m.Update(failure)
	if len(m.notices) != 0 || m.statusHistory[0].count != 3 {
		t.Fatal("acknowledged failure reopened before recovery")
	}
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(failure)
	if len(m.notices) != 1 {
		t.Fatal("a failure after recovery must require acknowledgment again")
	}
}

func TestStatusHistoryBoundedSearchableAndSessionOnly(t *testing.T) {
	m := splitModel(t)
	m.Update(key("s"))
	m.Update(key("n"))
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Status history") || len(m.modal.items) != 0 {
		t.Fatal("empty session history must open from Settings")
	}
	m.Update(key("esc"))
	now := testToday
	m.now = func() time.Time { return now }
	m.setStatus(statusWarning, "offline")
	now = now.Add(time.Second)
	m.setStatus(statusWarning, "offline")
	if len(m.statusHistory) != 1 || m.statusHistory[0].count != 2 || !m.statusHistory[0].at.Equal(now) {
		t.Fatal("duplicate message must update timestamp and count")
	}
	for i := range 105 {
		m.setStatus(statusInfo, fmt.Sprintf("message %d", i))
	}
	if len(m.statusHistory) != maxStatusHistory || m.statusHistory[0].text != "message 5" {
		t.Fatal("history must retain only the latest 100 entries")
	}
	m.setStatus(statusWarning, "offline again")
	m.openStatusHistory()
	p := m.modal
	if message := p.items[0].Value.(statusMessage); message.text != "offline again" || message.level != statusWarning {
		t.Fatal("history must be newest first with severity")
	}
	p.input.SetValue("warning offline")
	p.refilter()
	if len(p.matches) != 1 {
		t.Fatal("history must search severity and message")
	}
	if cmd := p.onPick(p.choose(p.matches[0])); cmd == nil || m.modal != p {
		t.Fatal("selecting history must copy full text without replaying notifications")
	}
	fresh := splitModel(t)
	if len(fresh.statusHistory) != 0 {
		t.Fatal("history must not survive a new app session")
	}
}

func TestNoticeCopyKeepsFullMessageAndModal(t *testing.T) {
	for _, copyKey := range []string{"y"} {
		t.Run(copyKey, func(t *testing.T) {
			m := splitModel(t)
			text := strings.Repeat("failed request: details ", 40)
			m.setStatus(statusError, text)
			m.setStatus(statusError, "next error")
			want := m.notices[0].text
			view := ansi.Strip(m.body())
			if !strings.Contains(view, "y copy") {
				t.Fatal("notice footer must advertise copy keys")
			}
			_, cmd := m.Update(key(copyKey))
			if !slices.Contains(commandMessages(cmd), tea.SetClipboard(want)()) {
				t.Fatal("copy must send the full visible notice message")
			}
			if len(m.notices) != 2 || m.notices[0].text != want || len(m.statusHistory) != 2 {
				t.Fatal("copy must preserve notice queue and history")
			}
		})
	}
}

func TestStatusHistoryCopyKeysPreserveSelectionAndSearch(t *testing.T) {
	for _, copyKey := range []string{"y", "ctrl+y"} {
		t.Run(copyKey, func(t *testing.T) {
			m := splitModel(t)
			text := strings.Repeat("selected status details ", 40)
			m.setStatus(statusInfo, text)
			m.setStatus(statusWarning, "newest status")
			m.openStatusHistory()
			p := m.modal
			m.Update(key("j"))
			if hints := ansi.Strip(p.controls(m.accentStyle)); !strings.Contains(hints, "y copy") {
				t.Fatal("history footer must advertise copy keys")
			}
			_, cmd := m.Update(key(copyKey))
			if !slices.Contains(commandMessages(cmd), tea.SetClipboard(m.statusHistory[0].text)()) {
				t.Fatal("copy must send the selected full message")
			}
			if m.modal != p || p.cursor != 1 || len(m.statusHistory) != 2 {
				t.Fatal("copy must preserve history, modal, and selection")
			}
			m.Update(key("/"))
			m.Update(key("c"))
			m.Update(key("y"))
			if p.input.Value() != "cy" {
				t.Fatal("printable copy keys must type while searching")
			}
			p.input.SetValue("selected")
			p.refilter()
			_, cmd = m.Update(key("ctrl+y"))
			if !slices.Contains(commandMessages(cmd), tea.SetClipboard(m.statusHistory[0].text)()) || m.modal != p || !p.searching {
				t.Fatal("ctrl+y must copy the filtered selection without leaving search")
			}
			p.input.SetValue("no matching message")
			p.refilter()
			if _, cmd = p.update(key("ctrl+y")); cmd != nil {
				t.Fatal("empty search must not copy")
			}
		})
	}
	m := splitModel(t)
	m.openStatusHistory()
	for _, copyKey := range []string{"y", "ctrl+y"} {
		if _, cmd := m.modal.update(key(copyKey)); cmd != nil {
			t.Fatal("empty history must not copy")
		}
	}
}

func TestStatusHistoryListUsesAvailableWidth(t *testing.T) {
	m := splitModel(t)
	m.setStatus(statusInfo, "Ticket saved successfully")
	m.openStatusHistory()
	p := m.modal
	previewWidth := 0
	p.preview = func(_ *pickItem, width, height int) string {
		previewWidth = width
		return "Preview"
	}
	for _, tc := range []struct{ width, preview int }{{40, 0}, {56, 29}, {80, 45}, {100, 57}, {140, 81}} {
		previewWidth = 0
		view := ansi.Strip(p.view(tc.width, 20, m.accentStyle))
		if previewWidth != tc.preview {
			t.Fatalf("width %d: preview width = %d, want %d", tc.width, previewWidth, tc.preview)
		}
		if tc.width == 140 && !strings.Contains(view, "Info: Ticket saved successfully") {
			t.Fatal("wide history list must show more of the message")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > tc.width {
				t.Fatalf("history exceeds width %d: %q", tc.width, line)
			}
		}
	}
}

func TestStatusHistoryRecordsFinalOperationMessage(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		run        func(*Model)
	}{
		{"completed ticket", "Completed Fix login", func(m *Model) {
			m.Update(editDoneMsg{gid: openTask.GID, what: "complete Fix login", done: "Completed Fix login"})
		}},
		{"cleared branch", "branch cleared", func(m *Model) { m.saveBranch(openTask.GID, "") }},
		{"reset columns", "columns reset to list.fields", func(m *Model) { m.storeColumns(nil, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := splitModel(t)
			tc.run(m)
			if len(m.statusHistory) != 1 || m.statusHistory[0].text != tc.want {
				t.Fatalf("history = %+v, want only %q", m.statusHistory, tc.want)
			}
		})
	}
}
