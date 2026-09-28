package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/browser"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const (
	detailDelay    = 200 * time.Millisecond
	requestTimeout = time.Minute
	agentRefresh   = 5 * time.Second
	spinnerFrame   = 100 * time.Millisecond
)

type spinnerTickMsg struct{}

func scheduleSpinner() tea.Cmd {
	return tea.Tick(spinnerFrame, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

type tasksMsg struct {
	project *asana.Ref
	tasks   []asana.Task
	fields  map[string]bool // the project's custom field gids, when known
	err     error
}

type agentsMsg struct {
	list []agents.Agent
	err  error
}

type agentTickMsg struct{}

type projectFieldsMsg struct {
	gid    string
	fields map[string]bool
	err    error
}

type detailTickMsg struct{ gid string }

type detailMsg struct {
	gid    string
	ticket ticket.Ticket
	err    error
}

type projectsMsg struct {
	projects []asana.Project
	err      error
}

type candidatesMsg struct {
	paths []string
	err   error
}

type actionDoneMsg struct {
	name string
	log  string
	err  error
}

type imageMsg struct {
	payload string
	url     string
	err     error
}

type statusMsg string

func loadTasks(c *asana.Client, workspace string, project *asana.Ref) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		tasks, err := ticket.List(ctx, c, workspace, gidOf(project))
		msg := tasksMsg{project: project, tasks: tasks, err: err}
		if err == nil && project != nil {
			// Best effort: only used to pick between same-named fields.
			msg.fields, _ = c.ProjectFieldGIDs(ctx, project.GID)
		}
		return msg
	}
}

func loadAgents(cfg config.Agents) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentRefresh*2)
		defer cancel()
		list, err := agents.Fetch(ctx, cfg.Preset, cfg.Command, cfg.States)
		return agentsMsg{list: list, err: err}
	}
}

func scheduleAgents() tea.Cmd {
	return tea.Tick(agentRefresh, func(time.Time) tea.Msg { return agentTickMsg{} })
}

func loadProjectFields(c *asana.Client, gid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		fields, err := c.ProjectFieldGIDs(ctx, gid)
		return projectFieldsMsg{gid: gid, fields: fields, err: err}
	}
}

func scheduleDetail(gid string) tea.Cmd {
	return tea.Tick(detailDelay, func(time.Time) tea.Msg { return detailTickMsg{gid: gid} })
}

func loadDetail(c *asana.Client, gid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		t, err := ticket.Fetch(ctx, c, gid)
		return detailMsg{gid: gid, ticket: t, err: err}
	}
}

func loadProjects(c *asana.Client, workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		me, err := c.Me(ctx)
		if err != nil {
			return projectsMsg{err: err}
		}
		projects, err := c.MemberProjects(ctx, workspace, me.GID)
		return projectsMsg{projects: projects, err: err}
	}
}

func loadCandidates(command string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		paths, err := repo.Candidates(ctx, command)
		return candidatesMsg{paths: paths, err: err}
	}
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		if err := browser.Open(url); err != nil {
			return statusMsg(err.Error())
		}
		return nil
	}
}

func loadImage(c *asana.Client, a asana.Attachment, cols, rows int, inTmux bool) tea.Cmd {
	url := ticket.AttachmentURL(a)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		fresh, err := c.Attachment(ctx, a.GID)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		if fresh.DownloadURL == nil {
			return imageMsg{url: url, err: errors.New("attachment has no download URL")}
		}
		data, err := kitty.Download(ctx, *fresh.DownloadURL)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		payload, err := kitty.Encode(data, cols, rows, inTmux)
		return imageMsg{payload: payload, url: url, err: err}
	}
}
