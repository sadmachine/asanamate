package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

type pendingTime struct {
	ticket  ticket.Ticket
	project asana.Ref
	spec    form.Spec
}

type timeFormMsg struct {
	entry *pendingTime
	spec  form.Spec
	err   error
}

type timeDoneMsg struct {
	entry  *pendingTime
	values map[string]string
	err    error
}

func (m *Model) openTime() tea.Cmd {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return nil
	}
	if t.GID == "" || t.PermalinkURL == "" {
		m.status = "ticket needs an Asana link to log time"
		return nil
	}
	var projects []asana.Ref
	for _, member := range t.Memberships {
		if member.Project.GID != "" && !slices.ContainsFunc(projects, func(p asana.Ref) bool { return p.GID == member.Project.GID }) {
			projects = append(projects, member.Project)
		}
	}
	if len(projects) == 0 {
		m.status = "ticket belongs to no Asana project"
		return nil
	}
	m.timeEntry = &pendingTime{ticket: t}
	if len(projects) == 1 {
		return m.pickedTimeAsanaProject(projects[0])
	}
	items := make([]pickItem, len(projects))
	for i, project := range projects {
		items[i] = pickItem{Label: ticket.OneLine(project.Name), Value: project}
	}
	m.modal = newPicker(pickValue(m.pickedTimeAsanaProject), "Which Asana project?", items)
	return nil
}

func (m *Model) pickedTimeAsanaProject(project asana.Ref) tea.Cmd {
	m.modal = nil
	entry := m.timeEntry
	entry.project = project
	m.status = "loading time form…"
	provider := timetracking.Provider{Command: m.deps.Config.TimeTracking.Command}
	return request(func(ctx context.Context) tea.Msg {
		spec, err := provider.Form(ctx)
		return timeFormMsg{entry: entry, spec: spec, err: err}
	})
}

func (m *Model) gotTimeForm(msg timeFormMsg) {
	if m.timeEntry != msg.entry {
		return
	}
	if msg.err != nil {
		m.status = "loading time form: " + msg.err.Error()
		m.timeEntry = nil
		return
	}
	entry := m.timeEntry
	entry.spec = msg.spec
	defaults := map[string]string{}
	owner := "time:" + m.deps.Config.TimeTracking.ID
	for _, field := range msg.spec.Fields {
		if !field.Remember {
			continue
		}
		for _, id := range m.deps.State.RecentFormChoices(owner, entry.project.GID, field.ID) {
			if field.HasOption(id) {
				defaults[field.ID] = id
				break
			}
		}
	}
	m.status = ""
	m.form = newFormModal("Log time · "+ticket.OneLine(entry.project.Name), msg.spec, defaults, m.submitTimeForm)
}

func (m *Model) submitTimeForm(values map[string]string) tea.Cmd {
	entry := m.timeEntry
	m.form.busy = true
	m.status = "logging time…"
	provider := timetracking.Provider{Command: m.deps.Config.TimeTracking.Command}
	asanaTask := timetracking.Asana{TaskGID: entry.ticket.GID, ProjectGID: entry.project.GID, Title: entry.ticket.Name, URL: entry.ticket.PermalinkURL}
	return request(func(ctx context.Context) tea.Msg {
		return timeDoneMsg{entry: entry, values: values, err: provider.Log(ctx, entry.spec, values, asanaTask)}
	})
}

func (m *Model) finishTime(msg timeDoneMsg) tea.Cmd {
	if m.timeEntry != msg.entry {
		return nil
	}
	if msg.err != nil {
		m.form.busy = false
		m.form.err = msg.err.Error()
		m.status = "logging time: " + msg.err.Error()
		return nil
	}
	entry := m.timeEntry
	owner := "time:" + m.deps.Config.TimeTracking.ID
	for _, field := range entry.spec.Fields {
		if field.Remember {
			m.deps.State.TouchFormChoice(owner, entry.project.GID, field.ID, msg.values[field.ID])
		}
	}
	if err := m.deps.State.Save(); err != nil {
		m.status = fmt.Sprintf("time logged; saving choices: %v", err)
	} else {
		m.status = "time logged"
	}
	m.form, m.timeEntry = nil, nil
	cmd := m.loadTimeSummary(entry.ticket.Task)
	if t, ok := m.selected(); ok && t.GID == entry.ticket.GID {
		m.renderDetail(true)
	}
	return cmd
}

// timeSummaryState stays separate from Asana details so provider failures do
// not prevent ticket loading. Each request owns one generation per ticket.
type timeSummaryState struct {
	seq     uint64
	loading bool
	summary timetracking.Summary
	err     error
}

type timeSummaryMsg struct {
	gid     string
	seq     uint64
	summary timetracking.Summary
	err     error
}

func (m *Model) loadTimeSummary(t asana.Task) tea.Cmd {
	if !m.deps.Config.TimeTrackingEnabled() {
		return nil
	}
	if m.timeSummaries == nil {
		m.timeSummaries = map[string]timeSummaryState{}
	}
	m.timeSummarySeq++
	seq := m.timeSummarySeq
	m.timeSummaries[t.GID] = timeSummaryState{seq: seq, loading: true}
	provider := timetracking.Provider{Command: m.deps.Config.TimeTracking.Command}
	return request(func(ctx context.Context) tea.Msg {
		summary, err := provider.Summary(ctx, timetracking.Asana{TaskGID: t.GID, URL: t.PermalinkURL})
		return timeSummaryMsg{gid: t.GID, seq: seq, summary: summary, err: err}
	})
}

func (m *Model) gotTimeSummary(msg timeSummaryMsg) {
	state, ok := m.timeSummaries[msg.gid]
	if !ok || state.seq != msg.seq {
		return
	}
	state.loading, state.summary, state.err = false, msg.summary, msg.err
	m.timeSummaries[msg.gid] = state
	if t, ok := m.selected(); ok && t.GID == msg.gid {
		m.renderDetail(true)
	}
}

func (m *Model) timeSummaryValue(gid string) string {
	if !m.deps.Config.TimeTrackingEnabled() {
		return ""
	}
	state, ok := m.timeSummaries[gid]
	value := "loading…"
	switch {
	case !ok || state.loading:
	case errors.Is(state.err, timetracking.ErrUnsupported):
		value = "unsupported"
	case state.err != nil:
		value = "unavailable"
	default:
		seconds := state.summary.TotalSeconds
		parts := []string{}
		if seconds >= 3600 {
			parts = append(parts, fmt.Sprintf("%dh", seconds/3600))
		}
		if minutes := seconds / 60 % 60; minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
		if remainder := seconds % 60; remainder > 0 {
			parts = append(parts, fmt.Sprintf("%ds", remainder))
		}
		if len(parts) == 0 {
			parts = append(parts, "0m")
		}
		value = strings.Join(parts, " ") + " (visible entries)"
	}
	return value + " · " + ticket.OneLine(m.deps.Config.TimeTracking.ID)
}
