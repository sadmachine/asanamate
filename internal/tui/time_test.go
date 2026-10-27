package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

const timeFormJSON = `{"fields":[{"id":"project_id","label":"Harvest project","type":"select","options":[{"id":"123","name":"Web"},{"id":"789","name":"Other"}],"remember":true},{"id":"task_id","label":"Harvest task","type":"select","options":[{"id":"456","name":"Development"}],"remember":true},{"id":"hours","label":"Hours","type":"hours"}]}`

func timeModel(t *testing.T, memberships ...asana.Membership) (*Model, *state.State) {
	t.Helper()
	m, st := testModel(t, config.Config{TimeTracking: config.TimeTracking{ID: "hrvst", Command: "printf '%s' '" + timeFormJSON + "'"}})
	task := asana.Task{GID: "42", Name: "Fix login", PermalinkURL: "https://app.asana.com/0/1/42", Memberships: memberships}
	m.tasks, m.visible = []asana.Task{task}, []asana.Task{task}
	m.details[task.GID] = ticket.Ticket{Task: task}
	m.loading = false
	return m, st
}

func TestTimeKeyOnlyWhenConfigured(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	if _, ok := m.bindingFor("t"); ok || strings.Contains(m.helpView(), "log time") {
		t.Fatal("time key shown while disabled")
	}
	m, _ = timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	if _, ok := m.bindingFor("t"); !ok || !strings.Contains(m.helpView(), "log time") {
		t.Fatal("time key missing while enabled")
	}
}

func TestFontResizeDoesNotOpenTimeForm(t *testing.T) {
	for _, shortcut := range []rune{'=', '-', '0'} {
		t.Run(string(shortcut), func(t *testing.T) {
			m, _ := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Web"}})
			m.deps.Images, m.deps.InTmux, m.deps.Config.Images.Inline = true, true, true
			m.Update(tea.KeyPressMsg{Code: shortcut, Mod: tea.ModSuper})
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			var reply string
			for _, msg := range commandMessages(cmd) {
				if raw, ok := msg.(tea.RawMsg); ok {
					// Captured from tmux with extended-keys on and csi-u encoding.
					// A bypassed query's reply is interpreted as keyboard input.
					switch raw.Msg {
					case "\x1b[16t":
						reply = "\x1b[6;32;16t"
					case "\x1bPtmux;\x1b\x1b[16t\x1b\\":
						reply = "\x1b[91;3u6;18;9t"
					default:
						t.Fatalf("unexpected terminal query: %q", raw.Msg)
					}
				}
			}
			if reply == "" {
				t.Fatal("resize did not query cell size")
			}
			var decoder uv.EventDecoder
			for data := []byte(reply); len(data) > 0; {
				n, event := decoder.Decode(data)
				if n == 0 {
					t.Fatalf("undecoded terminal reply: %q", data)
				}
				if key, ok := event.(uv.KeyPressEvent); ok {
					m.Update(tea.KeyPressMsg(key))
				} else {
					m.Update(event)
				}
				data = data[n:]
			}
			if m.waiting != "" || m.form != nil || m.timeEntry != nil {
				t.Fatalf("font resize opened time tracking: waiting %q, form %v, entry %v", m.waiting, m.form, m.timeEntry)
			}
			if m.imageCell.Width != 16 || m.imageCell.Height != 32 {
				t.Fatalf("cell dimensions not updated: %+v", m.imageCell)
			}
			m.Update(key("t"))
			if m.waiting != "Loading time form…" {
				t.Fatal("intentional time shortcut stopped working")
			}
		})
	}
}

func TestTimeFormChoosesAsanaProjectAndRestoresFields(t *testing.T) {
	web := asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}}
	api := asana.Membership{Project: asana.Ref{GID: "2", Name: "Asana API"}}
	m, st := timeModel(t, web, api)
	if cmd := m.openTime(); cmd != nil || m.modal == nil || m.modal.title != "Which Asana project?" {
		t.Fatalf("project prompt = %+v, cmd = %v", m.modal, cmd)
	}
	cmd := m.pickedTimeAsanaProject(api.Project)
	if m.waiting != "Loading time form…" {
		t.Fatalf("waiting = %q, want the loading modal", m.waiting)
	}
	st.TouchFormChoice("time:hrvst", "2", "project_id", "789")
	st.TouchFormChoice("time:hrvst", "2", "task_id", "456")
	drain(m, cmd)
	if m.form == nil || m.waiting != "" || m.form.values["project_id"] != "789" || m.form.values["task_id"] != "456" || m.form.values["hours"] != "" {
		t.Fatalf("form values = %+v", m.form)
	}
	if m.form.spec.Fields[0].Type != form.Select || m.form.spec.Fields[2].Type != form.Hours {
		t.Fatalf("form spec = %+v", m.form.spec)
	}
}

// The provider's info lines sit under the title, followed by the ticket's
// tracked time.
func TestTimeFormShowsProviderInfoAndTicketTime(t *testing.T) {
	m, _ := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	withInfo := strings.Replace(timeFormJSON, `{"fields":`, `{"info":["Today: Acme 3h · Foo 2h"],"fields":`, 1)
	m.deps.Config.TimeTracking.Command = "printf '%s' '" + withInfo + "'"
	today := int64(4200)
	m.timeSummaries = map[string]timeSummaryState{"42": {summary: timetracking.Summary{TotalSeconds: 21600, TodaySeconds: &today}}}
	drain(m, m.openTime())
	if m.form == nil {
		t.Fatalf("no form; status = %q", m.status)
	}
	lines := strings.Split(ansi.Strip(m.form.view(80, 20, m.modalLook())), "\n")
	if len(lines) < 3 || lines[1] != "Today: Acme 3h · Foo 2h" || lines[2] != "This ticket: 6h · 1h 10m today" {
		t.Fatalf("form view = %q", lines)
	}
	if len(m.timeEntry.spec.Info) != 1 {
		t.Errorf("ticket line leaked into the provider spec: %q", m.timeEntry.spec.Info)
	}
}

// A provider default fills a select without a remembered choice; a
// remembered choice still offered wins over it.
func TestTimeFormProviderDefaultYieldsToRememberedChoice(t *testing.T) {
	m, st := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	withDefault := strings.Replace(timeFormJSON, `"remember":true}`, `"remember":true,"default":"123"}`, 1)
	m.deps.Config.TimeTracking.Command = "printf '%s' '" + withDefault + "'"
	m.deps.Config.ProjectLinks.Command = `printf '{"links":[{"type":"harvest_project","value":"123"}]}'`
	drain(m, m.openTime())
	if m.form == nil || m.form.values["project_id"] != "123" || m.status != "" {
		t.Fatalf("form = %+v, status = %q", m.form, m.status)
	}
	st.TouchFormChoice("time:hrvst", "1", "project_id", "789")
	drain(m, m.openTime())
	if m.form.values["project_id"] != "789" {
		t.Fatalf("remembered choice lost to default: %+v", m.form.values)
	}
	m.deps.Config.ProjectLinks.Command = "echo hub down >&2; exit 1"
	drain(m, m.openTime())
	if m.form == nil || !strings.Contains(m.status, "no suggestions: project links: hub down") {
		t.Fatalf("form = %+v, status = %q", m.form, m.status)
	}
}

func TestTimeFormValidationFailureAndSuccess(t *testing.T) {
	m, st := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	cmd := m.openTime()
	drain(m, cmd)
	if m.form == nil || m.form.values["task_id"] != "456" {
		t.Fatalf("form = %+v", m.form)
	}
	m.form.values["project_id"] = "123"
	m.form.values["hours"] = "0"
	_, cmd = m.form.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd != nil || !strings.Contains(m.form.err, "positive") {
		t.Fatalf("invalid form: cmd = %v, err = %q", cmd, m.form.err)
	}
	m.form.values["hours"] = "1.25"
	entry := m.timeEntry
	m.finishTime(timeDoneMsg{entry: entry, values: m.form.values, err: errors.New("provider unavailable")})
	if m.form == nil || m.form.values["hours"] != "1.25" || len(st.RecentFormChoices("time:hrvst", "1", "project_id")) != 0 {
		t.Fatal("failed log lost hours or saved choices")
	}
	_, cmd = m.form.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("valid form did not call provider")
	}
	_, refresh := m.Update(cmd())
	if refresh == nil || !m.timeSummaries["42"].loading {
		t.Fatal("successful log did not refresh summary")
	}
	if m.form != nil || m.timeEntry != nil || st.RecentFormChoices("time:hrvst", "1", "project_id")[0] != "123" || st.RecentFormChoices("time:hrvst", "1", "task_id")[0] != "456" {
		t.Fatal("successful log did not save remembered fields")
	}
}

func TestTimeFormCancelKeepsStateUnchanged(t *testing.T) {
	m, st := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	drain(m, m.openTime())
	m.form.values["project_id"] = "123"
	cancelled, _ := m.form.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !cancelled || len(st.RecentFormChoices("time:hrvst", "1", "project_id")) != 0 {
		t.Fatal("cancel changed time state")
	}
}

func TestTimeSummaryStatesAndReaderViews(t *testing.T) {
	for _, view := range []string{config.ViewCards, config.ViewMarkdown} {
		t.Run(view, func(t *testing.T) {
			m, _ := timeModel(t)
			m.readerView = view
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
			task := m.details["42"].Task
			task.HTMLNotes = "<body>" + strings.Repeat("<p>line</p>", 40) + "</body>"
			m.details["42"] = ticket.Ticket{Task: task}
			m.deps.Config.TimeTracking.Command = "printf '%s' '{\"total_seconds\":13500,\"today_seconds\":4500}'"
			cmd := m.loadTimeSummary(task)
			m.renderDetail(false)
			if !strings.Contains(ansi.Strip(m.reader.GetContent()), "loading") {
				t.Fatal("loading state missing")
			}
			m.Update(cmd())
			content := ansi.Strip(m.reader.GetContent())
			if !strings.Contains(content, "3h 45m · 1h 15m today") || strings.Contains(content, "visible entries") || strings.Contains(content, "hrvst") {
				t.Fatalf("summary missing: %s", content)
			}
			m.reader.SetYOffset(8)
			offset := m.reader.YOffset()
			seq := m.timeSummaries["42"].seq
			m.Update(timeSummaryMsg{gid: "42", seq: seq, summary: timetracking.Summary{TotalSeconds: 0}})
			if !strings.Contains(ansi.Strip(m.reader.GetContent()), "0m") || m.reader.YOffset() != offset {
				t.Fatal("zero state missing or scroll lost")
			}
			m.Update(timeSummaryMsg{gid: "42", seq: seq, err: errors.New("offline")})
			if !strings.Contains(m.timeSummaryValue("42"), "unavailable") || strings.Contains(m.timeSummaryValue("42"), "0m") {
				t.Fatal("failed request shown as zero")
			}
			m.Update(timeSummaryMsg{gid: "42", seq: seq, err: timetracking.ErrUnsupported})
			if !strings.Contains(m.timeSummaryValue("42"), "unsupported") {
				t.Fatal("unsupported state missing")
			}
			m.deps.Config.TimeTracking = config.TimeTracking{}
			m.renderDetail(true)
			if strings.Contains(ansi.Strip(m.reader.GetContent()), "unsupported") || m.loadTimeSummary(task) != nil {
				t.Fatal("disabled tracker shown or called")
			}
		})
	}
}

func TestTimeSummaryTodayFormatting(t *testing.T) {
	m, _ := timeModel(t)
	for _, tc := range []struct {
		summary timetracking.Summary
		want    string
	}{
		{timetracking.Summary{TotalSeconds: 13500}, "3h 45m"},
		{timetracking.Summary{TotalSeconds: 13500, TodaySeconds: new(int64(0))}, "3h 45m · 0m today"},
		{timetracking.Summary{TotalSeconds: 3661, TodaySeconds: new(int64(61))}, "1h 1m 1s · 1m 1s today"},
		{timetracking.Summary{TodaySeconds: new(int64(0))}, "0m · 0m today"},
	} {
		m.timeSummaries = map[string]timeSummaryState{"42": {summary: tc.summary}}
		if got := m.timeSummaryValue("42"); got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}
}

func TestTimeSummaryRejectsStaleRepliesAndCachesByTicket(t *testing.T) {
	m, _ := timeModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	task := m.details["42"].Task
	m.deps.Config.TimeTracking.Command = "printf '%s' '{\"total_seconds\":60}'"
	old := m.loadTimeSummary(task)
	m.deps.Config.TimeTracking.Command = "printf '%s' '{\"total_seconds\":3600}'"
	latest := m.loadTimeSummary(task)
	m.Update(latest())
	m.Update(old())
	if m.timeSummaries["42"].loading || m.timeSummaries["42"].summary.TotalSeconds != 3600 {
		t.Fatal("stale reply replaced newest result")
	}
	other := asana.Task{GID: "99", Name: "Other"}
	m.tasks, m.visible = append(m.tasks, other), append(m.visible, other)
	m.details["99"] = ticket.Ticket{Task: other}
	otherCmd := m.loadTimeSummary(other)
	before := m.reader.GetContent()
	m.Update(otherCmd())
	if m.reader.GetContent() != before || m.timeSummaries["99"].summary.TotalSeconds != 3600 {
		t.Fatal("reply for another ticket changed selected reader")
	}
	m.shownGID = ""
	m.selectionChanged()
	if m.timeSummaries["42"].seq != 2 {
		t.Fatal("cached selection refetched summary")
	}
	m.Update(detailMsg{gid: "42", ticket: ticket.Ticket{Task: task}})
	if !m.timeSummaries["42"].loading || m.timeSummaries["42"].seq <= 2 {
		t.Fatal("ticket refresh did not refresh time")
	}
}

func TestTimeSummaryLivesInHeaderWithClock(t *testing.T) {
	for _, tc := range []struct {
		style string
		clock string
	}{
		{config.SymbolsNerd, "\uf017"},
		{config.SymbolsUnicode, "◷"},
		{config.SymbolsASCII, "(time)"},
	} {
		t.Run(tc.style, func(t *testing.T) {
			m, _ := timeModel(t)
			m.sym = newSymbols(tc.style, nil, false)
			m.timeSummaries = map[string]timeSummaryState{"42": {summary: timetracking.Summary{TotalSeconds: 13500}}}
			tk := m.details["42"]
			for _, width := range []int{40, 120} {
				content := ansi.Strip(m.renderCards(tk, width))
				timeAt, detailsAt := strings.Index(content, tc.clock+" 3h 45m"), strings.Index(content, "Details")
				if timeAt < 0 || detailsAt < timeAt || strings.Count(content, "3h 45m") != 1 {
					t.Fatalf("time must appear once in header with clock: %s", content)
				}
				for _, line := range strings.Split(content[:detailsAt], "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("header exceeds width %d: %q", width, line)
					}
				}
				if strings.Contains(ansi.Strip(m.detailsBody(tk, width-4, 0)), "3h 45m") {
					t.Fatal("time remains in Details card")
				}
			}
		})
	}
}

func TestTodayTimeUsesThemeOKOnlyWhenPositive(t *testing.T) {
	m, _ := timeModel(t)
	m.timeSummaries = map[string]timeSummaryState{}
	defer applyPalette(config.Config{}.Palette(true))
	m.deps.Config.Colors.OK = config.Style{FG: "#123456"}
	applyPalette(m.deps.Config.Palette(true))
	for _, seconds := range []int64{0, 3600} {
		m.timeSummaries["42"] = timeSummaryState{summary: timetracking.Summary{TotalSeconds: seconds, TodaySeconds: &seconds}}
		plain, styled := m.timeSummaryValue("42"), m.timeSummaryStyled("42")
		if ansi.Strip(styled) != plain {
			t.Fatalf("styling changed text: %q vs %q", styled, plain)
		}
		tinted := okStyle.Faint(false).Render(trackedDuration(seconds) + " today")
		if strings.Contains(styled, tinted) != (seconds > 0) {
			t.Fatalf("today tint for %d: %q", seconds, styled)
		}
	}
}
