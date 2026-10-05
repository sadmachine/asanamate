package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

const timeFormJSON = `{"fields":[{"id":"project_id","label":"Harvest project","type":"select","options":[{"id":"123","name":"Web"},{"id":"789","name":"Other"}],"remember":true},{"id":"task_id","label":"Harvest task","type":"select","options":[{"id":"456","name":"Engineering"}],"remember":true},{"id":"hours","label":"Hours","type":"hours"}]}`

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

func TestTimeFormChoosesAsanaProjectAndRestoresFields(t *testing.T) {
	web := asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}}
	api := asana.Membership{Project: asana.Ref{GID: "2", Name: "Asana API"}}
	m, st := timeModel(t, web, api)
	if cmd := m.openTime(); cmd != nil || m.modal == nil || m.modal.title != "Which Asana project?" {
		t.Fatalf("project prompt = %+v, cmd = %v", m.modal, cmd)
	}
	cmd := m.pickedTimeAsanaProject(api.Project)
	st.TouchFormChoice("time:hrvst", "2", "project_id", "789")
	st.TouchFormChoice("time:hrvst", "2", "task_id", "456")
	m.Update(cmd())
	if m.form == nil || m.form.values["project_id"] != "789" || m.form.values["task_id"] != "456" || m.form.values["hours"] != "" {
		t.Fatalf("form values = %+v", m.form)
	}
	if m.form.spec.Fields[0].Type != form.Select || m.form.spec.Fields[2].Type != form.Hours {
		t.Fatalf("form spec = %+v", m.form.spec)
	}
}

func TestTimeFormValidationFailureAndSuccess(t *testing.T) {
	m, st := timeModel(t, asana.Membership{Project: asana.Ref{GID: "1", Name: "Asana Web"}})
	cmd := m.openTime()
	m.Update(cmd())
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
	m.Update(m.openTime()())
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
			m.deps.Config.TimeTracking.Command = "printf '%s' '{\"total_seconds\":13500}'"
			cmd := m.loadTimeSummary(task)
			m.renderDetail(false)
			if !strings.Contains(ansi.Strip(m.reader.GetContent()), "loading") {
				t.Fatal("loading state missing")
			}
			m.Update(cmd())
			content := ansi.Strip(m.reader.GetContent())
			if !strings.Contains(content, "Time tracked") || !strings.Contains(content, "3h 45m") || !strings.Contains(content, "visible entries") {
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
			if strings.Contains(ansi.Strip(m.reader.GetContent()), "Time tracked") || m.loadTimeSummary(task) != nil {
				t.Fatal("disabled tracker shown or called")
			}
		})
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
