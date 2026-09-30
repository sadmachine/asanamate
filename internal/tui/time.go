package tui

import (
	"context"
	"fmt"
	"slices"

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

func (m *Model) finishTime(msg timeDoneMsg) {
	if m.timeEntry != msg.entry {
		return
	}
	if msg.err != nil {
		m.form.busy = false
		m.form.err = msg.err.Error()
		m.status = "logging time: " + msg.err.Error()
		return
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
}
