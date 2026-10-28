package tui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/writeback"
)

// hookEvent is something that happened to a ticket, for hooks.
type hookEvent struct {
	name    string     // a config.Hook* event
	task    asana.Task // the ticket, with the event's change applied
	comment *asana.Story
}

// hookEdits maps hooks' edit:<item> names to edit menu operations.
var hookEdits = map[string]editOp{
	"title": editTitle, "description": editDescription, "section": editSection,
	"field": editField, "assign": editAssignee, "due": editDue,
	"add_project": editAddProject, "remove_project": editRemoveProject, "branch": editBranch,
}

// hookActionMsg carries a background hook action's ticket once its details
// have loaded.
type hookActionMsg struct {
	action config.Action
	event  hookEvent
	ticket ticket.Ticket
	err    error
}

// runHooks runs the hooks that match events, in config order. Background
// actions all run; at most one prompt opens for the whole batch, and only
// when nothing else is open. Hooks whose prompt cannot open stay unmarked
// for once, so they ask next time.
func (m *Model) runHooks(events []hookEvent) tea.Cmd {
	var cmds []tea.Cmd
	prompted, marked := false, false
	for _, ev := range events {
		for _, h := range m.deps.Config.Hooks {
			if h.On != ev.name || (h.Once && m.deps.State.HookRan(h.Key(), ev.task.GID)) {
				continue
			}
			if !filter.Parse(h.When).Match(ev.task, m.filterEnv()) {
				continue
			}
			a, isAction := m.deps.Config.HookAction(h)
			if isAction && !a.Interactive() {
				cmds = append(cmds, m.runHookAction(a, ev))
			} else {
				if prompted || !m.canPrompt(ev.task.GID) {
					continue
				}
				prompted = true
				cmds = append(cmds, m.openHookPrompt(h, a, ev))
			}
			if h.Once {
				m.deps.State.MarkHookRun(h.Key(), ev.task.GID)
				marked = true
			}
		}
	}
	if marked {
		if err := m.deps.State.Save(); err != nil {
			m.setStatus(statusError, "saving hooks: "+err.Error())
		}
	}
	return tea.Batch(cmds...)
}

// canPrompt reports whether a hook can open a prompt on ticket gid: it is
// the selected ticket, its details have loaded, and nothing else is open.
func (m *Model) canPrompt(gid string) bool {
	t, ok := m.selectedDetail()
	return ok && t.GID == gid && m.modal == nil && m.input == nil && m.form == nil &&
		m.composer == nil && m.builder == nil && m.extension == nil && m.waiting == "" &&
		len(m.notices) == 0 && !m.help
}

// openHookPrompt opens a hook's interactive target on the selected ticket.
// Edits it opens raise no hook events.
func (m *Model) openHookPrompt(h config.Hook, a config.Action, ev hookEvent) tea.Cmd {
	kind, name := h.Target()
	switch kind {
	case config.HookLogTime:
		return m.openTime()
	case "edit":
		if !m.startEdit() {
			return nil
		}
		m.edit.hook = true
		return m.pickedEdit(hookEdits[name])
	}
	if _, ok := m.startRun(); !ok {
		return nil
	}
	m.run.action, m.run.event = a, ev.name
	return m.advanceRun()
}

// runHookAction runs a background hook action, loading the ticket's details
// first when the view does not have them.
func (m *Model) runHookAction(a config.Action, ev hookEvent) tea.Cmd {
	if t, ok := m.details[ev.task.GID]; ok {
		return m.launchHookAction(hookActionMsg{action: a, event: ev, ticket: t})
	}
	c := m.deps.Client
	return request(func(ctx context.Context) tea.Msg {
		t, err := ticket.Fetch(ctx, c, ev.task.GID)
		return hookActionMsg{action: a, event: ev, ticket: t, err: err}
	})
}

// launchHookAction starts a background hook action without touching the
// action run the user may have open.
func (m *Model) launchHookAction(msg hookActionMsg) tea.Cmd {
	if msg.err != nil {
		m.observeAsana("loading ticket for "+msg.action.Name, msg.err)
		m.setStatus(statusError, msg.action.Name+": loading ticket: "+msg.err.Error())
		return nil
	}
	t := msg.ticket
	r := &pendingRun{action: msg.action, ticket: t, comment: msg.event.comment, event: msg.event.name}
	r.project = action.DefaultProject(t.Task, gidOf(m.viewProject))
	r.branch = action.Branch(t.Task, m.deps.State.TaskBranches[t.GID], m.deps.Config.BranchField, m.projectFields[gidOf(r.project)])
	return m.launch(r)
}

// checkTicketOpen raises ticket_open when the reader has focus on a ticket
// it did not have before, once the ticket's details have loaded. Starting a
// comment also focuses the reader; that counts as already open.
func (m *Model) checkTicketOpen(composerBefore bool) tea.Cmd {
	if !m.focusReader || m.activityOpen || !m.readerOn() {
		m.openedGID = ""
		return nil
	}
	t, ok := m.selectedDetail()
	if !ok || t.GID == m.openedGID {
		return nil
	}
	m.openedGID = t.GID
	if m.composer != nil && !composerBefore {
		return nil
	}
	return m.runHooks([]hookEvent{{name: config.HookTicketOpen, task: t.Task}})
}

// withAssignee returns t assigned to user gid, "" for nobody, named from the
// loaded people when known.
func (m *Model) withAssignee(t asana.Task, gid string) asana.Task {
	t.Assignee = nil
	if gid != "" {
		ref := asana.Ref{GID: gid}
		if i := slices.IndexFunc(m.users, func(u asana.Ref) bool { return u.GID == gid }); i >= 0 {
			ref = m.users[i]
		}
		t.Assignee = &ref
	}
	return t
}

// changeEvents returns the hook events changes raise on t, applying each
// change in turn: assignments, and completions but not reopenings.
func (m *Model) changeEvents(t asana.Task, changes []writeback.Change) []hookEvent {
	var events []hookEvent
	for _, change := range changes {
		switch change.Kind {
		case "assign":
			t = m.withAssignee(t, change.Value)
			events = append(events, hookEvent{name: config.HookAssign, task: t})
		case "complete":
			t.Completed = change.Value == "true"
			if t.Completed {
				events = append(events, hookEvent{name: config.HookComplete, task: t})
			}
		}
	}
	return events
}
