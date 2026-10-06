package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
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
	pins    []asana.Task // explicitly pinned tasks absent from the normal list
	warning string
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
	seq    uint64
	gid    string
	ticket ticket.Ticket
	err    error
}

type projectsMsg struct {
	projects []asana.Project
	err      error
}

// linkNamesMsg carries names for linked projects outside m.projects; a
// project that failed to load maps to "".
type linkNamesMsg struct{ names map[string]string }

type candidatesMsg struct {
	paths []string
	err   error
	link  *linkTarget // link being edited, outside a run
}

// linkTarget is a project whose repo link is edited, or with ticket set, a
// ticket's own repo that overrides its projects' links.
type linkTarget struct {
	ref    asana.Ref
	ticket bool
}

type actionDoneMsg struct {
	name string
	log  string
	err  error
}

type sectionsMsg struct {
	project  asana.Ref
	sections []asana.Ref
	err      error
}

type usersMsg struct {
	users []asana.Ref
	err   error
}

type editDoneMsg struct {
	gid  string
	what string
	err  error
}

type imageMsg struct {
	payload string
	url     string
	err     error
}

// viewerDoneMsg reports the image viewer closing; step is kitty.Viewer.Step.
type viewerDoneMsg struct {
	step int
	err  error
}

type statusMsg string

// request runs fn off the UI loop with a requestTimeout context.
func request(fn func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return fn(ctx)
	}
}

func loadTasks(c *asana.Client, workspace string, project *asana.Ref, pins []string) tea.Cmd {
	pins = slices.Clone(pins)
	return request(func(ctx context.Context) tea.Msg {
		msg := tasksMsg{project: project}
		var fields map[string]bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			if project != nil {
				// Best effort: only used to pick between same-named fields.
				fields, _ = c.ProjectFieldGIDs(ctx, project.GID)
			}
		}()
		msg.tasks, msg.err = ticket.List(ctx, c, workspace, gidOf(project))
		<-done
		if msg.err == nil {
			msg.fields = fields
			var failed []string
			for _, gid := range pins {
				if slices.ContainsFunc(msg.tasks, func(t asana.Task) bool { return t.GID == gid }) {
					continue
				}
				t, err := c.Task(ctx, gid)
				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", gid, err))
					continue
				}
				msg.pins = append(msg.pins, t)
			}
			if len(failed) > 0 {
				msg.warning = "loading pinned tickets: " + strings.Join(failed, "; ")
			}
		}
		return msg
	})
}

func loadAgents(cfg config.Agents, stateDir string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentRefresh*2)
		defer cancel()
		list, err := agents.Fetch(ctx, cfg.Preset, cfg.Command, cfg.States, stateDir)
		return agentsMsg{list: list, err: err}
	}
}

func scheduleAgents() tea.Cmd {
	return tea.Tick(agentRefresh, func(time.Time) tea.Msg { return agentTickMsg{} })
}

func loadProjectFields(c *asana.Client, gid string) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		fields, err := c.ProjectFieldGIDs(ctx, gid)
		return projectFieldsMsg{gid: gid, fields: fields, err: err}
	})
}

func scheduleDetail(gid string) tea.Cmd {
	return tea.Tick(detailDelay, func(time.Time) tea.Msg { return detailTickMsg{gid: gid} })
}

// loadDetail prevents an older response from replacing newer ticket data.
func (m *Model) loadDetail(gid string) tea.Cmd {
	m.detailSeq++
	seq := m.detailSeq
	if m.detailRequests == nil {
		m.detailRequests = map[string]uint64{}
	}
	m.detailRequests[gid] = seq
	c := m.deps.Client
	return request(func(ctx context.Context) tea.Msg {
		t, err := ticket.Fetch(ctx, c, gid)
		return detailMsg{seq: seq, gid: gid, ticket: t, err: err}
	})
}

func loadProjects(c *asana.Client, workspace string) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		me, err := c.Me(ctx)
		if err != nil {
			return projectsMsg{err: err}
		}
		projects, err := c.MemberProjects(ctx, workspace, me.GID)
		return projectsMsg{projects: projects, err: err}
	})
}

func loadLinkNames(c *asana.Client, gids []string) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		names := make(map[string]string, len(gids))
		for _, gid := range gids {
			p, _ := c.Project(ctx, gid)
			names[gid] = p.Name
		}
		return linkNamesMsg{names: names}
	})
}

// loadCandidates loads the repo picker's paths; link is set when editing a
// link from the repo links picker.
func loadCandidates(src config.RepoSource, link *linkTarget) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		paths, err := repo.Candidates(ctx, src.Command, src.Root)
		return candidatesMsg{paths: paths, err: err, link: link}
	})
}

// loadSections loads a project's sections, or My Tasks' sections when
// project has no gid.
func loadSections(c *asana.Client, workspace string, project asana.Ref) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		gid := project.GID
		if gid == "" {
			list, err := c.MyTaskList(ctx, workspace)
			if err != nil {
				return sectionsMsg{project: project, err: err}
			}
			gid = list.GID
		}
		sections, err := c.Sections(ctx, gid)
		return sectionsMsg{project: project, sections: sections, err: err}
	})
}

func loadUsers(c *asana.Client, workspace string) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		users, err := c.WorkspaceUsers(ctx, workspace)
		return usersMsg{users: users, err: err}
	})
}

// saveEdit runs one write to ticket gid; what names it in the status line.
func saveEdit(gid, what string, write func(context.Context) error) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		return editDoneMsg{gid: gid, what: what, err: write(ctx)}
	})
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		if err := browser.Open(url); err != nil {
			return statusMsg(err.Error())
		}
		return nil
	}
}

func loadImage(c *asana.Client, a asana.Attachment, cols, rows int, cell kitty.CellSize, inTmux bool) tea.Cmd {
	url := ticket.AttachmentURL(a)
	return request(func(ctx context.Context) tea.Msg {
		data, err := fetchImage(ctx, c, a.GID)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		payload, err := kitty.Encode(data, cols, rows, cell, inTmux)
		return imageMsg{payload: payload, url: url, err: err}
	})
}
