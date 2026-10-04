package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// pendingRun tracks an action between picking it and launching it.
type pendingRun struct {
	action         config.Action
	ticket         ticket.Ticket
	project        *asana.Ref
	projectChosen  bool
	repo           string // resolved repo, kept while input or project fields are pending
	awaitingFields bool
	agent          *agents.Agent // chosen agent for agent = true actions
	comment        *asana.Story  // highlighted comment, set when the menu opened
	input          string        // text typed for an input action
	inputDone      bool
	formValues     map[string]string
	formDone       bool
	branch         string // branch typed when the branch field is empty
}

// sharesFieldNames reports whether two of the task's custom fields share a
// name, in which case the active project decides which one an action sees.
func sharesFieldNames(t asana.Task) bool {
	for i, a := range t.CustomFields {
		for _, b := range t.CustomFields[i+1:] {
			if asana.SameFieldName(a.Name, b.Name) {
				return true
			}
		}
	}
	return false
}

// startRun begins a run on the selected ticket and its highlighted comment,
// returning the action context the selection offers. It reports false when
// the ticket's details have not loaded.
func (m *Model) startRun() (string, bool) {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return "", false
	}
	comment := m.selectedComment(t)
	m.run, m.edit = &pendingRun{ticket: t, comment: comment}, nil
	if comment != nil {
		return config.ContextComment, true
	}
	return "", true
}

func (m *Model) openActionMenu() {
	if len(m.deps.Config.Actions) == 0 {
		m.status = "no actions configured; add a .toml file to the actions folder next to config.toml"
		return
	}
	context, ok := m.startRun()
	if !ok {
		return
	}
	// Actions for the highlighted item come first, so their keys win over
	// everywhere actions bound to the same key.
	var first, rest []pickItem
	for i, a := range m.deps.Config.Actions {
		item := pickItem{Label: a.Name, Hint: a.Mode, Key: a.Key, Value: i}
		switch a.Context {
		case "":
			rest = append(rest, item)
		case context:
			first = append(first, item)
		}
	}
	items := append(first, rest...)
	if len(items) == 0 {
		m.run, m.status = nil, "no actions for this selection"
		return
	}
	p := newPicker(pickValue(m.pickedAction), "Run on: "+ticket.Clean(m.run.ticket.Name), items)
	p.keySelect = true
	m.modal = p
}

// repeatAction runs the last picked action again on the current selection.
func (m *Model) repeatAction() tea.Cmd {
	if m.lastAction < 0 || m.lastAction >= len(m.deps.Config.Actions) {
		m.status = "no action run yet"
		return nil
	}
	context, ok := m.startRun()
	if !ok {
		return nil
	}
	a := m.deps.Config.Actions[m.lastAction]
	if a.Context != "" && a.Context != context {
		m.run, m.status = nil, a.Name+" needs a highlighted "+a.Context
		return nil
	}
	return m.pickedAction(m.lastAction)
}

// selectedComment returns the comment highlighted in the reader, or nil.
func (m *Model) selectedComment(t ticket.Ticket) *asana.Story {
	for i, c := range t.Comments {
		if m.fieldKey == commentTarget(c, i) {
			return &t.Comments[i]
		}
	}
	return nil
}

// requestMenu runs open, first fetching the selected ticket's details if
// they have not arrived yet.
func (m *Model) requestMenu(open func() tea.Cmd) tea.Cmd {
	t, ok := m.selected()
	if !ok {
		return nil
	}
	if _, cached := m.details[t.GID]; cached {
		return open()
	}
	m.menuFor, m.menuOpen = t.GID, open
	m.status = "loading ticket…"
	return loadDetail(m.deps.Client, t.GID)
}

func (m *Model) pickedAction(i int) tea.Cmd {
	m.modal = nil
	m.lastAction = i
	m.run.action = m.deps.Config.Actions[i]
	return m.continueRun()
}

func (m *Model) pickedAgent(a agents.Agent) tea.Cmd {
	m.modal = nil
	m.run.agent = &a
	return m.continueRun()
}

func (m *Model) pickedTicketProject(ref asana.Ref) tea.Cmd {
	m.modal = nil
	m.run.project, m.run.projectChosen = &ref, true
	return m.continueRun()
}

// continueRun advances the run: use the ticket's own repo if still valid,
// otherwise choose the project, use its linked repo if still valid, otherwise
// load candidates for the repo picker.
func (m *Model) continueRun() tea.Cmd {
	r := m.run
	if r.action.Agent && r.agent == nil {
		list := m.viewAgents(r.ticket.Task)
		switch len(list) {
		case 0:
			m.run = nil
			m.showNotice("no running agent for this ticket")
			return nil
		case 1:
			r.agent = &list[0]
		default:
			items := make([]pickItem, len(list))
			for i, a := range list {
				items[i] = pickItem{Label: m.agentLabel(a, lipgloss.NewStyle()), Hint: ticket.OneLine(filepath.Base(a.Path)), Value: a}
			}
			m.modal = newPicker(pickValue(m.pickedAgent), "Which agent?", items)
			return nil
		}
	}
	if !r.action.Repo {
		r.project = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject))
		return m.execute("")
	}
	if path, ok := m.deps.State.TaskRepos[r.ticket.GID]; ok {
		if isRepoRoot(path) {
			if !r.projectChosen {
				r.project = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject))
			}
			return m.execute(path)
		}
		m.status = fmt.Sprintf("ticket repo %s is no longer a git repository; using the project's", path)
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
			m.modal = newPicker(pickValue(m.pickedTicketProject), "Which project's repo?", items)
			return nil
		}
	}
	if r.project != nil {
		if path, ok := m.deps.State.Repos[r.project.GID]; ok {
			if isRepoRoot(path) {
				return m.execute(path)
			}
			m.status = fmt.Sprintf("linked repo %s is no longer a git repository; pick again", path)
		}
	}
	return loadCandidates(m.deps.Config.RepoSource.Command, nil)
}

// isRepoRoot reports whether a saved link still names a repo. Links are saved
// as top-level paths; a different result means the directory is no longer its
// own repo (it may sit inside a parent one).
func isRepoRoot(path string) bool {
	resolved, err := repo.Resolve(path)
	return err == nil && resolved == path
}

// effectiveRepo returns the repo a repo = true action on t runs in without
// asking, as continueRun picks it: t's own repo, else its only project's link.
// own reports the former; path is "" when the action would ask.
func (m *Model) effectiveRepo(t asana.Task) (path string, own bool) {
	if p, ok := m.deps.State.TaskRepos[t.GID]; ok && isRepoRoot(p) {
		return p, true
	}
	if len(t.Memberships) == 1 {
		if p, ok := m.deps.State.Repos[t.Memberships[0].Project.GID]; ok && isRepoRoot(p) {
			return p, false
		}
	}
	return "", false
}

func (m *Model) openRepoPicker(msg candidatesMsg) {
	if msg.link != nil {
		m.openLinkRepoPicker(msg)
		return
	}
	if m.run == nil {
		return
	}
	m.modal = repoPicker(msg, m.run.project, pickPath(m.pickedRepo))
}

// repoPicker lists msg's candidate repos for project, after any lead items,
// and accepts a typed path.
func repoPicker(msg candidatesMsg, project *asana.Ref, onPick func(pickResult) tea.Cmd, lead ...pickItem) *picker {
	items := lead
	for _, p := range msg.paths {
		items = append(items, pickItem{Label: p, Value: p})
	}
	title := "Repo for this ticket"
	if project != nil {
		title = "Repo for " + ticket.Clean(project.Name)
	}
	p := newPicker(onPick, title, items)
	p.allowFree = true
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	return p
}

// pickPath adapts fn to the repo picker's choice: a listed path or typed text.
func pickPath(fn func(string) tea.Cmd) func(pickResult) tea.Cmd {
	return func(res pickResult) tea.Cmd {
		if res.item != nil {
			return fn(res.item.Value.(string))
		}
		return fn(res.free)
	}
}

func (m *Model) pickedRepo(path string) tea.Cmd {
	resolved, ok := m.resolvePicked(path)
	if !ok {
		return nil
	}
	if p := m.run.project; p != nil {
		m.saveLink(linkTarget{ref: *p}, resolved)
	}
	return m.execute(resolved)
}

// resolvePicked resolves a path chosen in the repo picker, closing it, or
// keeps it open with the error.
func (m *Model) resolvePicked(path string) (string, bool) {
	resolved, err := repo.Resolve(path)
	if err != nil {
		if m.modal != nil {
			m.modal.err = err.Error()
		}
		return "", false
	}
	m.modal = nil
	return resolved, true
}

// saveLink links target to path, or unlinks it when path is empty, and saves
// the state.
func (m *Model) saveLink(target linkTarget, path string) {
	gid, st := target.ref.GID, m.deps.State
	switch {
	case target.ticket && path == "":
		st.UnlinkTaskRepo(gid)
	case target.ticket:
		st.LinkTaskRepo(gid, path)
	case path == "":
		st.UnlinkRepo(gid)
	default:
		st.LinkRepo(gid, path)
	}
	m.linked = nil
	m.renderDetail(true)
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving repo link: " + err.Error()
	}
}

// typedInput takes the input box's text: the action's input, or, once that
// is done, the branch for a ticket whose branch field is empty. A branch
// other than the fallback is saved for the ticket, so agents on it link.
func (m *Model) typedInput(text string) tea.Cmd {
	m.input = nil
	r := m.run
	if r.action.Input != "" && !r.inputDone {
		r.input, r.inputDone = text, true
	} else {
		r.branch = cmp.Or(text, action.DefaultBranch(r.ticket.Task))
		if r.branch != action.DefaultBranch(r.ticket.Task) {
			m.saveBranch(r.ticket.GID, r.branch)
		}
	}
	return m.execute(r.repo)
}

func (m *Model) typedActionForm(values map[string]string) tea.Cmd {
	m.form = nil
	r := m.run
	r.formValues, r.formDone = values, true
	if r.project != nil {
		remembered := false
		for _, field := range r.action.Form.Fields {
			if field.Remember {
				m.deps.State.TouchFormChoice("action:"+r.action.Key, r.project.GID, field.ID, values[field.ID])
				remembered = true
			}
		}
		if remembered {
			if err := m.deps.State.Save(); err != nil {
				m.status = "saving action choices: " + err.Error()
			}
		}
	}
	return m.execute(r.repo)
}

func (m *Model) execute(repoPath string) tea.Cmd {
	r := m.run
	if r.action.Input != "" && !r.inputDone {
		r.repo = repoPath
		m.input = newInputBox(r.action.Input, "optional; leave empty to skip")
		return nil
	}
	if len(r.action.Form.Fields) > 0 && !r.formDone {
		r.repo = repoPath
		defaults := map[string]string{}
		if r.project != nil {
			for _, field := range r.action.Form.Fields {
				for _, id := range m.deps.State.RecentFormChoices("action:"+r.action.Key, r.project.GID, field.ID) {
					if field.HasOption(id) {
						defaults[field.ID] = id
						break
					}
				}
			}
		}
		m.form = newFormModal(r.action.Name, r.action.Form, defaults, m.typedActionForm)
		return nil
	}
	if p := r.project; p != nil && sharesFieldNames(r.ticket.Task) {
		if _, ok := m.projectFields[p.GID]; !ok {
			r.repo, r.awaitingFields = repoPath, true
			m.status = "loading project fields…"
			return loadProjectFields(m.deps.Client, p.GID)
		}
	}
	preferred := m.projectFields[gidOf(r.project)]
	branch := r.branch
	if branch == "" {
		saved := m.deps.State.TaskBranches[r.ticket.GID]
		branch = action.Branch(r.ticket.Task, saved, m.deps.Config.BranchField, preferred)
		if strings.Contains(r.action.Command, "ASANAMATE_BRANCH") &&
			action.BranchWarning(r.ticket.Task, saved, m.deps.Config.BranchField, preferred) != "" {
			r.repo = repoPath
			m.input = newInputBox(fmt.Sprintf("Branch (%q is empty)", m.deps.Config.BranchField), "empty uses the ID field or title slug")
			m.input.area.SetValue(branch)
			return nil
		}
	}
	m.run = nil
	linked := m.ticketAgents(r.ticket.Task, preferred)
	agent := r.agent
	if agent == nil {
		agent = firstAgent(linked)
	}
	files, err := action.WriteFiles(m.deps.StateDir, r.ticket)
	if err != nil {
		m.showNotice("writing ticket files: " + err.Error())
		return nil
	}
	if r.inputDone {
		if err := files.WriteInput(r.input); err != nil {
			m.showNotice("writing input: " + err.Error())
			return nil
		}
	}
	var worktree string
	if repoPath != "" {
		worktree = repo.Worktree(repoPath, branch)
	}
	cmd := action.Command(r.action, action.Context{
		Ticket:        r.ticket,
		Project:       r.project,
		Repo:          repoPath,
		Worktree:      worktree,
		ConfirmWrites: m.deps.Config.ConfirmWritesFor(r.action),
		Files:         files,
		Preferred:     preferred,
		BranchField:   m.deps.Config.BranchField,
		Branch:        branch,
		Agent:         agent,
		Agents:        linked,
		Comment:       r.comment,
		FormValues:    r.formValues,
	})
	name, mode := r.action.Name, r.action.Mode
	if m.deps.ExitOnAction && mode == config.ModeBackground {
		mode = config.ModeExit
	}
	switch mode {
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

// agentLabel is an agent's static symbol, state, and title; style colors the
// symbol and state.
func (m *Model) agentLabel(a agents.Agent, style lipgloss.Style) string {
	label := style.Render(m.sym.states[a.State] + " " + string(a.State))
	if a.Title != "" {
		label += " · " + a.Title
	}
	return label
}

// agentsSection is the reading pane's Markdown list of t's agents.
func (m *Model) agentsSection(t asana.Task) string {
	linked, notes := m.viewAgents(t), m.agentNotes(t)
	if len(linked) == 0 && len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Agents\n\n")
	for _, a := range linked {
		fmt.Fprintf(&b, "- %s — `%s`\n", m.agentLabel(a, lipgloss.NewStyle()), ticket.OneLine(filepath.Base(a.Path)))
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "- *%s*\n", ticket.OneLine(n))
	}
	return b.String()
}

func firstAgent(list []agents.Agent) *agents.Agent {
	if len(list) == 0 {
		return nil
	}
	return &list[0]
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
	m.modal = newPicker(pickValue(m.pickedAttachment), "Attachments", items)
}

func (m *Model) showsInline(a asana.Attachment) bool {
	return m.deps.Images && a.Host == "asana" && kitty.IsImage(a.Name)
}

func (m *Model) pickedAttachment(a asana.Attachment) tea.Cmd {
	m.modal = nil
	if !m.showsInline(a) {
		return openURL(ticket.AttachmentURL(a))
	}
	t, _ := m.selectedDetail()
	m.viewImages = nil
	for _, other := range t.Attachments {
		if m.showsInline(other) {
			m.viewImages = append(m.viewImages, other)
		}
	}
	m.viewIndex = slices.IndexFunc(m.viewImages, func(b asana.Attachment) bool { return b.GID == a.GID })
	if m.viewIndex < 0 {
		m.viewImages, m.viewIndex = []asana.Attachment{a}, 0
	}
	return m.viewImage(m.viewIndex)
}

// viewImage loads viewImages[i] for the attachment viewer.
func (m *Model) viewImage(i int) tea.Cmd {
	m.viewIndex = i
	m.status = "loading image…"
	// The image leaves room for a blank line and the two footer lines.
	return loadImage(m.deps.Client, m.viewImages[i], max(m.width, 1), max(m.height-3, 1), m.imageCell, m.deps.InTmux)
}

// viewerFooter is the image viewer's bottom bar, centered like a browser
// lightbox: the image's name, then its position and the viewer's keys.
func (m *Model) viewerFooter() string {
	key := func(k string) string { return m.accentStyle.Bold(true).Render(k) }
	name := ticket.OneLine(ticket.Clean(m.viewImages[m.viewIndex].Name))
	count := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(m.accentStyle.GetForeground()).
		Render(fmt.Sprintf(" %d / %d ", m.viewIndex+1, len(m.viewImages)))
	keys := count
	if len(m.viewImages) > 1 {
		keys = key("‹ k") + " " + dimStyle.Render("previous") + "   " + count + "   " + dimStyle.Render("next") + " " + key("j ›")
	}
	keys += "      " + key("esc") + " " + dimStyle.Render("close")
	// Only left padding, so the bottom line never fills the last column.
	center := func(s string) string {
		s = ansi.Truncate(s, m.width-1, "…")
		return strings.Repeat(" ", max(m.width-ansi.StringWidth(s), 0)/2) + s
	}
	return center(titleStyle.Render(name)) + "\n" + center(keys)
}
