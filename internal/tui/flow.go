package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// pendingRun tracks an action between picking it and launching it.
type pendingRun struct {
	action        config.Action
	ticket        ticket.Ticket
	project       *asana.Ref
	projectChosen bool
}

func (m *Model) openActionMenu() {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return
	}
	if len(m.deps.Config.Actions) == 0 {
		m.status = "no actions configured; add [[actions]] to config.toml"
		return
	}
	items := make([]pickItem, len(m.deps.Config.Actions))
	for i, a := range m.deps.Config.Actions {
		items[i] = pickItem{Label: a.Name, Hint: a.Mode, Key: a.Key, Value: i}
	}
	p := newPicker(pickAction, "Run on: "+ticket.Clean(t.Name), items)
	p.keySelect = true
	m.modal = p
	m.run = &pendingRun{ticket: t}
}

func (m *Model) pickedAction(i int) tea.Cmd {
	m.modal = nil
	m.run.action = m.deps.Config.Actions[i]
	return m.continueRun()
}

func (m *Model) pickedTicketProject(ref asana.Ref) tea.Cmd {
	m.modal = nil
	m.run.project, m.run.projectChosen = &ref, true
	return m.continueRun()
}

// continueRun advances the run: choose the project, use its linked repo if
// still valid, otherwise load candidates for the repo picker.
func (m *Model) continueRun() tea.Cmd {
	r := m.run
	if !r.action.Repo {
		r.project = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject))
		return m.execute("")
	}
	if !r.projectChosen {
		switch len(r.ticket.Memberships) {
		case 0:
			r.projectChosen = true
		case 1:
			p := r.ticket.Memberships[0].Project
			r.project, r.projectChosen = &p, true
		default:
			items := make([]pickItem, len(r.ticket.Memberships))
			for i, mb := range r.ticket.Memberships {
				items[i] = pickItem{Label: ticket.Clean(mb.Project.Name), Value: mb.Project}
			}
			m.modal = newPicker(pickTicketProject, "Which project's repo?", items)
			return nil
		}
	}
	if r.project != nil {
		if path, ok := m.deps.State.Repos[r.project.GID]; ok {
			if resolved, err := repo.Resolve(path); err == nil {
				return m.execute(resolved)
			}
			m.status = fmt.Sprintf("linked repo %s is no longer a git repository; pick again", path)
		}
	}
	return loadCandidates(m.deps.Config.RepoSource.Command)
}

func (m *Model) openRepoPicker(msg candidatesMsg) {
	if m.run == nil {
		return
	}
	items := make([]pickItem, len(msg.paths))
	for i, p := range msg.paths {
		items[i] = pickItem{Label: p, Value: p}
	}
	title := "Repo for this ticket"
	if m.run.project != nil {
		title = "Repo for " + ticket.Clean(m.run.project.Name)
	}
	p := newPicker(pickRepo, title, items)
	p.allowFree = true
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	m.modal = p
}

func (m *Model) pickedRepo(path string) tea.Cmd {
	resolved, err := repo.Resolve(path)
	if err != nil {
		if m.modal != nil {
			m.modal.err = err.Error()
		}
		return nil
	}
	m.modal = nil
	if p := m.run.project; p != nil {
		m.deps.State.LinkRepo(p.GID, resolved)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving repo link: " + err.Error()
		}
	}
	return m.execute(resolved)
}

func (m *Model) execute(repoPath string) tea.Cmd {
	r := m.run
	m.run = nil
	files, err := action.WriteFiles(m.deps.StateDir, r.ticket)
	if err != nil {
		m.status = "writing ticket files: " + err.Error()
		return nil
	}
	cmd := action.Command(r.action, action.Context{
		Ticket:        r.ticket,
		Project:       r.project,
		Repo:          repoPath,
		ConfirmWrites: m.deps.Config.ConfirmWritesFor(r.action),
		Files:         files,
	})
	name := r.action.Name
	switch r.action.Mode {
	case config.ModeForeground:
		return tea.ExecProcess(cmd, func(err error) tea.Msg { return actionDoneMsg{name: name, err: err} })
	case config.ModeBackground:
		m.status = name + ": running…"
		logPath := filepath.Join(m.deps.StateDir, "actions.log")
		return func() tea.Msg {
			return actionDoneMsg{name: name, log: logPath, err: action.RunBackground(cmd, logPath)}
		}
	default:
		m.exitCmd = cmd
		return tea.Quit
	}
}

func actionStatus(msg actionDoneMsg) string {
	if msg.err == nil {
		return msg.name + ": done"
	}
	s := msg.name + " failed: " + msg.err.Error()
	if msg.log != "" {
		s += " (see " + msg.log + ")"
	}
	return s
}

func (m *Model) openAttachments() {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return
	}
	if len(t.Attachments) == 0 {
		m.status = "no attachments"
		return
	}
	items := make([]pickItem, len(t.Attachments))
	for i, a := range t.Attachments {
		hint := "open in browser"
		if m.showsInline(a) {
			hint = "view image"
		}
		items[i] = pickItem{Label: fmt.Sprintf("%d. %s", i+1, ticket.Clean(a.Name)), Hint: hint, Value: a}
	}
	m.modal = newPicker(pickAttachment, "Attachments", items)
}

func (m *Model) showsInline(a asana.Attachment) bool {
	return m.deps.Images && a.Host == "asana" && kitty.IsImage(a.Name)
}

func (m *Model) pickedAttachment(a asana.Attachment) tea.Cmd {
	m.modal = nil
	if m.showsInline(a) {
		m.status = "loading image…"
		return loadImage(m.deps.Client, a, max(m.width, 1), max(m.height-3, 1), m.deps.InTmux)
	}
	return openURL(ticket.AttachmentURL(a))
}
