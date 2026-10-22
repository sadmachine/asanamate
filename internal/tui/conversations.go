package tui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/conversations"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// conversationLookup is one lookup in flight for a ticket. Starting another,
// cancelling, or closing its picker replaces or clears m.conversations, so a
// late reply is dropped instead of attaching to another ticket.
type conversationLookup struct {
	ticket   ticket.Ticket
	cancel   context.CancelFunc
	shown    *picker               // picker shown while looking, with the last result if any
	selected *conversations.Record // non-nil revalidates a selection before focus
	target   string                // chosen focus target; empty lets the user choose
}

type conversationsMsg struct {
	lookup *conversationLookup
	result conversations.Result
	agents []agents.Agent
	err    error
}

// conversationRow is a picker row that is not a record.
type conversationRow int

const (
	conversationsRefresh conversationRow = iota
	conversationsCancel
)

// openAgents lists the selected ticket's running agents, then its saved
// conversations, which load in the background when the source is on.
func (m *Model) openAgents() tea.Cmd {
	t, ok := m.selectedDetail()
	if !ok {
		return nil
	}
	cfg := m.deps.Config
	if !cfg.AgentsEnabled() && !cfg.ConversationsEnabled() {
		m.setStatus(statusWarning, "no agents or conversations configured; set [agents] or [conversations] in config.toml")
		return nil
	}
	if !cfg.ConversationsEnabled() {
		m.modal = m.conversationPicker(t, conversations.Result{}, nil, "")
		return nil
	}
	return m.lookupConversations(t, nil)
}

// lookupConversations starts a fresh lookup for t. A previous successful
// result for t stays visible, marked stale, until the reply arrives.
// highlighted is the record or row Value to keep highlighted.
func (m *Model) lookupConversations(t ticket.Ticket, highlighted any) tea.Cmd {
	m.stopConversations()
	ctx, cancel := context.WithCancel(context.Background())
	l := &conversationLookup{ticket: t, cancel: cancel}
	m.conversations = l
	source := conversations.Source{Command: m.deps.Config.Conversations.Command}
	cmd := func() tea.Msg {
		result, err := source.List(ctx, t.GID)
		return conversationsMsg{lookup: l, result: result, err: err}
	}
	notice := "Finding conversations…"
	last, ok := m.conversationResults[t.GID]
	if ok {
		notice = "Refreshing… showing the last result."
	}
	l.shown = m.conversationPicker(t, last, highlighted, notice)
	m.modal = l.shown
	return cmd
}

// stopConversations cancels the lookup in flight, if any.
func (m *Model) stopConversations() {
	if m.conversations != nil {
		m.conversations.cancel()
		m.conversations = nil
	}
}

func (m *Model) gotConversations(msg conversationsMsg) tea.Cmd {
	l := msg.lookup
	if l != m.conversations {
		return nil
	}
	m.stopConversations()
	if l.selected != nil {
		m.waiting = ""
	} else if m.modal != l.shown {
		return nil
	}
	if t, ok := m.selected(); !ok || t.GID != l.ticket.GID {
		m.modal = nil
		m.setStatus(statusInfo, "selection changed; conversations closed")
		return nil
	}
	if l.selected != nil {
		return m.gotConversationOpen(msg)
	}
	// Refresh keeps the row highlighted in the shown picker when still present.
	var highlighted any
	if it := l.shown.current(); it != nil {
		highlighted = it.Value
	}
	gid := l.ticket.GID
	if msg.err == nil {
		conversations.Sort(msg.result.Conversations)
		m.conversationResults[gid] = msg.result
		m.modal = m.conversationPicker(l.ticket, msg.result, highlighted, "")
		return nil
	}
	notice := failureNotice(conversations.Failure(msg.err))
	last, ok := m.conversationResults[gid]
	if ok {
		notice += " Showing the last successful result."
	}
	m.modal = m.conversationPicker(l.ticket, last, highlighted, notice)
	return nil
}

// conversationPicker lists t's running agents, then result's records that
// are not running, with Refresh (when the source is on) and Cancel rows. A
// record running as an agent, by exact provider and session ID, shows once
// as the agent. notice explains a pending, stale, or failed lookup; otherwise
// the picker tells an empty complete lookup from a partial one.
func (m *Model) conversationPicker(t ticket.Ticket, result conversations.Result, highlighted any, notice string) *picker {
	running := m.viewAgents(t.Task)
	items := make([]pickItem, 0, len(running)+len(result.Conversations)+2)
	live := map[[2]string]bool{}
	for _, a := range running {
		items = append(items, pickItem{
			Label:  m.agentLabel(a, stateStyles[a.State].Bold(true)),
			Hint:   "running · " + ticket.OneLine(filepath.Base(a.Path)),
			Help:   ticket.OneLine(a.Path),
			Search: a.Title + " " + a.Path,
			Value:  a,
		})
		if a.Provider != "" && a.SessionID != "" {
			live[[2]string{a.Provider, a.SessionID}] = true
		}
	}
	for _, c := range result.Conversations {
		if !live[[2]string{c.Provider, c.SessionID}] {
			items = append(items, conversationItem(c))
		}
	}
	conversationsOn := m.deps.Config.ConversationsEnabled()
	if conversationsOn {
		items = append(items, pickItem{Label: "Refresh", Value: conversationsRefresh})
	}
	items = append(items, pickItem{Label: "Cancel", Value: conversationsCancel})
	cursor := 0
	for i, it := range items {
		if sameConversationRow(it.Value, highlighted) {
			cursor = i
		}
	}
	var notes []string
	switch {
	case notice != "":
		notes = append(notes, notice)
	case !conversationsOn:
		if len(running) == 0 {
			notes = append(notes, "No running agents.")
		}
	case len(result.Conversations) == 0 && result.Complete():
		notes = append(notes, "No linked conversations.")
	}
	if !result.Complete() {
		notes = append(notes, "Incomplete lookup; some conversations may be missing.")
	}
	for _, d := range result.Diagnostics {
		notes = append(notes, diagnosticNotice(d))
	}
	p := newPicker(func(res pickResult) tea.Cmd { return m.pickedConversation(t, res.item.Value) },
		"Agents: "+ticket.Clean(t.Name), items)
	p.helpIcon, p.notice = m.sym.info, strings.Join(notes, "\n")
	p.cursor = cursor
	return p
}

// sameConversationRow compares records by exact identity, other rows by value.
func sameConversationRow(a, b any) bool {
	ra, okA := a.(conversations.Record)
	rb, okB := b.(conversations.Record)
	if okA || okB {
		return okA && okB && ra.Provider == rb.Provider && ra.SessionID == rb.SessionID
	}
	return a != nil && a == b
}

// conversationItem shows a record's display-safe title, without the ticket
// marker the ticket already implies; the exact identity stays in Value.
func conversationItem(c conversations.Record) pickItem {
	title := conversations.DisplayText(c.Title)
	title = strings.TrimSpace(strings.TrimSuffix(title, "[asana:"+c.TaskGID+"]"))
	if title == "" {
		title = "(untitled)"
	}
	provider := conversations.DisplayText(c.Provider)
	help := []string{provider + " " + conversations.DisplayText(c.SessionID)}
	if c.CWD != "" {
		help = append(help, conversations.DisplayText(c.CWD))
	}
	if c.Branch != "" {
		help = append(help, "branch "+conversations.DisplayText(c.Branch))
	}
	return pickItem{
		Label:  title,
		Hint:   provider + " · " + c.UpdatedLabel() + " · " + c.StateLabel(),
		Help:   strings.Join(help, " · "),
		Search: strings.Join(help, " "),
		Value:  c,
	}
}

func failureNotice(d conversations.Diagnostic) string {
	label := map[string]string{
		"unsupported_version": "Unsupported conversation source version",
		"incomplete":          "Conversation lookup did not finish",
		"provider_error":      "Conversation source sent an invalid response",
	}[d.Code]
	if label == "" {
		label = "Conversation lookup unavailable"
	}
	return label + ": " + d.Message + " (" + conversations.Guidance(d.Code) + ")."
}

func diagnosticNotice(d conversations.Diagnostic) string {
	line := conversations.DisplayText(d.Provider) + ": " + d.Message
	if d.Severity != "info" {
		line += " (" + conversations.Guidance(d.Code) + ")"
	}
	return line
}

// pickedConversation refreshes, closes, focuses a running agent, or opens a
// record through the foreground action handoff, which restores the TUI when
// the opener exits.
func (m *Model) pickedConversation(t ticket.Ticket, value any) tea.Cmd {
	m.modal = nil
	switch v := value.(type) {
	case conversationRow:
		if v == conversationsRefresh {
			return m.lookupConversations(t, v)
		}
		m.stopConversations()
		return nil
	case agents.Agent:
		m.stopConversations()
		if strings.TrimSpace(m.deps.Config.Agents.Focus) != "" {
			if c, ok := m.conversationResults[t.GID].Find(v.Provider, v.SessionID); ok {
				return m.checkConversationOpen(t, c, v.Target)
			}
		}
		return m.focusAgent(t, v)
	case conversations.Record:
		m.stopConversations()
		if strings.TrimSpace(m.deps.Config.Agents.Focus) != "" {
			return m.checkConversationOpen(t, v, "")
		}
		return m.openConversation(t, v)
	}
	return nil
}

// checkConversationOpen refreshes both contracts; the periodic agent cache is
// display data, never authority for opening a selected conversation.
func (m *Model) checkConversationOpen(t ticket.Ticket, selected conversations.Record, target string) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), conversations.LookupTimeout)
	l := &conversationLookup{ticket: t, cancel: cancel, selected: &selected, target: target}
	m.conversations = l
	source := conversations.Source{Command: m.deps.Config.Conversations.Command}
	cfg := m.deps.Config.Agents
	return m.wait("Checking running conversation…", func() tea.Msg {
		msg := conversationsMsg{lookup: l}
		msg.result, msg.err = source.List(ctx, t.GID)
		if msg.err == nil {
			if _, ok := msg.result.Find(selected.Provider, selected.SessionID); ok {
				msg.agents, msg.err = agents.Fetch(ctx, cfg.Command, cfg.States)
			}
		}
		if msg.err == nil {
			msg.err = ctx.Err()
		}
		return msg
	})
}

func (m *Model) gotConversationOpen(msg conversationsMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus(statusError, "Open conversation failed: "+msg.err.Error())
		return nil
	}
	l := msg.lookup
	selected, ok := msg.result.Find(l.selected.Provider, l.selected.SessionID)
	if !ok {
		m.setStatus(statusError, "Open conversation failed: conversation is no longer available for this ticket; refresh conversations")
		return nil
	}
	var matches []agents.Agent
	for _, a := range msg.agents {
		if a.Provider == selected.Provider && a.SessionID == selected.SessionID && a.Target != "" {
			if l.target != "" {
				if a.Target == l.target {
					return m.focusConversation(l.ticket, selected, a)
				}
				continue
			}
			matches = append(matches, a)
		}
	}
	if l.target != "" {
		m.setStatus(statusError, "Focus agent failed: target is no longer available; refresh conversations")
		return nil
	}
	switch len(matches) {
	case 0:
		// Absence from an agent source does not prove inactivity. Preserve the
		// conversation provider's activity and let its opener decide safely.
		return m.openConversation(l.ticket, selected)
	case 1:
		return m.focusConversation(l.ticket, selected, matches[0])
	default:
		m.pickAgent(matches, func(a agents.Agent) tea.Cmd {
			m.modal = nil
			return m.checkConversationOpen(l.ticket, selected, a.Target)
		})
		return nil
	}
}

// focusConversation invokes trusted configuration with an opaque target and
// the revalidated conversation snapshot. It never quits or takes the terminal,
// and failure never falls through to a second process resuming the session.
func (m *Model) focusConversation(t ticket.Ticket, selected conversations.Record, a agents.Agent) tea.Cmd {
	m.modal = nil
	if current, ok := m.selected(); !ok || current.GID != t.GID {
		m.setStatus(statusInfo, "selection changed; conversations closed")
		return nil
	}
	cmd, err := m.conversationCommand(t, selected, m.deps.Config.Agents.Focus, &a)
	if err != nil {
		m.setStatus(statusError, "Focus agent failed: "+err.Error())
		return nil
	}
	logPath := filepath.Join(m.deps.StateDir, "actions.log")
	m.setProgress("Focus agent: running…")
	return func() tea.Msg {
		err := action.RunBackground(cmd, logPath)
		if err != nil {
			err = fmt.Errorf("%w; refresh conversations before retrying", err)
		}
		return actionDoneMsg{name: "Focus agent", log: logPath, err: err}
	}
}

// focusAgent runs agents.focus in the background for a, with the exports an
// agent = true action gets.
func (m *Model) focusAgent(t ticket.Ticket, a agents.Agent) tea.Cmd {
	focus := m.deps.Config.Agents.Focus
	if strings.TrimSpace(focus) == "" {
		m.setStatus(statusInfo, "set agents.focus in config.toml to switch to a running agent")
		return nil
	}
	return m.launch(&pendingRun{
		action:  config.Action{Name: "Focus agent", Mode: config.ModeBackground, Agent: true, Command: focus},
		ticket:  t,
		agent:   &a,
		project: action.DefaultProject(t.Task, gidOf(m.viewProject)),
	})
}

func (m *Model) openConversation(t ticket.Ticket, c conversations.Record) tea.Cmd {
	const name = "Resume in terminal"
	cmd, err := m.conversationCommand(t, c, m.deps.Config.Conversations.Open, nil)
	if err != nil {
		m.setStatus(statusError, name+" failed: "+err.Error())
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return actionDoneMsg{name: name, err: err} })
}

func (m *Model) conversationCommand(t ticket.Ticket, c conversations.Record, command string, agent *agents.Agent) (*exec.Cmd, error) {
	files, err := action.WriteFiles(m.deps.StateDir, t)
	if err != nil {
		return nil, fmt.Errorf("writing ticket files: %w", err)
	}
	return action.ConversationCommand(m.deps.StateDir, command,
		action.Context{Ticket: t, Files: files, BranchField: m.deps.Config.BranchField, Agent: agent}, c)
}
