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
	step           int           // index of the advanceRun step in progress
	agent          *agents.Agent // chosen agent for agent = true actions
	project        *asana.Ref
	repo           string       // resolved repo; "" for repo = false actions
	comment        *asana.Story // highlighted comment, set when the menu opened
	input          string       // text typed for an input action
	formValues     map[string]string
	awaitingFields bool   // the project's fields are loading
	branch         string // branch the action runs on
}

// advanceRun runs the run's steps in order from the current one, then
// launches the action. Each step settles one part of the run, or reports wait
// when it opened a modal or started a load; the handler for that answer
// records it and calls resumeRun. A step that cancels the run clears m.run.
func (m *Model) advanceRun() tea.Cmd {
	steps := []func(*pendingRun) (cmd tea.Cmd, wait bool){
		m.runAgent,
		m.runProject,
		m.runRepo,
		m.runInput,
		m.runForm,
		m.runProjectFields,
		m.runBranch,
	}
	for r := m.run; r.step < len(steps); r.step++ {
		if cmd, wait := steps[r.step](r); wait || m.run == nil {
			return cmd
		}
	}
	return m.launch(m.run)
}

// resumeRun moves past the step whose answer just arrived.
func (m *Model) resumeRun() tea.Cmd {
	m.run.step++
	return m.advanceRun()
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
		m.setStatus(statusWarning, "ticket details are still loading")
		return "", false
	}
	comment := m.selectedComment(t)
	m.run, m.edit = &pendingRun{ticket: t, comment: comment}, nil
	if comment != nil {
		return config.ContextComment, true
	}
	return "", true
}

// openActionMenu lists the actions for the selection.
func (m *Model) openActionMenu() {
	if len(m.deps.Config.Actions) == 0 {
		m.setStatus(statusWarning, "no actions configured; add a .toml file to the actions folder next to config.toml")
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
		m.run = nil
		m.setStatus(statusWarning, "no actions for this selection")
		return
	}
	p := newPicker(func(res pickResult) tea.Cmd {
		return m.pickedAction(res.item.Value.(int))
	}, "Run on: "+ticket.Clean(m.run.ticket.Name), items)
	p.keySelect = true
	m.modal = p
}

// repeatAction runs the last picked action again on the current selection.
func (m *Model) repeatAction() tea.Cmd {
	if m.lastAction < 0 || m.lastAction >= len(m.deps.Config.Actions) {
		m.setStatus(statusInfo, "no action run yet")
		return nil
	}
	context, ok := m.startRun()
	if !ok {
		return nil
	}
	a := m.deps.Config.Actions[m.lastAction]
	if a.Context != "" && a.Context != context {
		m.run = nil
		m.setStatus(statusWarning, a.Name+" needs a highlighted "+a.Context)
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
	m.setProgress("loading ticket…")
	return m.loadDetail(t.GID)
}

func (m *Model) pickedAction(i int) tea.Cmd {
	m.modal = nil
	m.lastAction = i
	m.run.action = m.deps.Config.Actions[i]
	return m.advanceRun()
}

// runAgent picks the agent an agent = true action talks to, asking when the
// ticket has several.
func (m *Model) runAgent(r *pendingRun) (tea.Cmd, bool) {
	if !r.action.Agent {
		return nil, false
	}
	list := m.viewAgents(r.ticket.Task)
	switch len(list) {
	case 0:
		m.run = nil
		m.setStatus(statusWarning, "no running agent for this ticket")
		return nil, true
	case 1:
		r.agent = &list[0]
		return nil, false
	}
	items := make([]pickItem, len(list))
	for i, a := range list {
		items[i] = pickItem{Label: m.agentLabel(a, lipgloss.NewStyle()), Hint: ticket.OneLine(filepath.Base(a.Path)), Value: a}
	}
	m.modal = newPicker(pickValue(m.pickedAgent), "Which agent?", items)
	return nil, true
}

func (m *Model) pickedAgent(a agents.Agent) tea.Cmd {
	m.modal = nil
	m.run.agent = &a
	return m.resumeRun()
}

// runProject picks the action's project. A repo = true action without a
// valid repo of the ticket's own asks which project's repo to use when the
// ticket is in several.
func (m *Model) runProject(r *pendingRun) (tea.Cmd, bool) {
	if !r.action.Repo {
		r.project = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject))
		return nil, false
	}
	if path, ok := m.deps.State.TaskRepos[r.ticket.GID]; ok {
		if isRepoRoot(path) {
			r.project, r.repo = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject)), path
			return nil, false
		}
		m.setStatus(statusWarning, fmt.Sprintf("ticket repo %s is no longer a git repository; using the project's", path))
	}
	switch len(r.ticket.Memberships) {
	case 0:
		return nil, false
	case 1:
		p := r.ticket.Memberships[0].Project
		r.project = &p
		return nil, false
	}
	items := make([]pickItem, len(r.ticket.Memberships))
	for i, mb := range r.ticket.Memberships {
		items[i] = pickItem{Label: ticket.Clean(mb.Project.Name), Value: mb.Project}
	}
	m.modal = newPicker(pickValue(m.pickedTicketProject), "Which project's repo?", items)
	return nil, true
}

func (m *Model) pickedTicketProject(ref asana.Ref) tea.Cmd {
	m.modal = nil
	m.run.project = &ref
	return m.resumeRun()
}

// runRepo uses the project's linked repo while it is still valid, otherwise
// loads candidates for the repo picker.
func (m *Model) runRepo(r *pendingRun) (tea.Cmd, bool) {
	if !r.action.Repo || r.repo != "" {
		return nil, false
	}
	if r.project != nil {
		if path, ok := m.deps.State.Repos[r.project.GID]; ok {
			if isRepoRoot(path) {
				r.repo = path
				return nil, false
			}
			m.setStatus(statusWarning, fmt.Sprintf("linked repo %s is no longer a git repository; pick again", path))
		}
	}
	projectGID := ""
	if r.project != nil {
		projectGID = r.project.GID
	}
	return m.wait("Loading repos…", loadCandidates(m.deps.Config.RepoSource, nil, m.projectLinks(), projectGID)), true
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
// and accepts a typed path. Suggested repos come first, so without lead items
// the cursor starts on the best suggestion.
func repoPicker(msg candidatesMsg, project *asana.Ref, onPick func(pickResult) tea.Cmd, lead ...pickItem) *picker {
	items := lead
	for _, p := range msg.suggested {
		items = append(items, pickItem{Label: p, Hint: "suggested", Value: p})
	}
	for _, p := range msg.paths {
		if !slices.Contains(msg.suggested, p) {
			items = append(items, pickItem{Label: p, Value: p})
		}
	}
	title := "Repo for this ticket"
	if project != nil {
		title = "Repo for " + ticket.Clean(project.Name)
	}
	p := newPicker(onPick, title, items)
	p.allowFree = true
	if msg.err != nil {
		p.err = msg.err.Error()
	} else if msg.linksErr != nil {
		p.err = "no suggestions: " + msg.linksErr.Error()
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
	m.run.repo = resolved
	return m.resumeRun()
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
		m.setStatus(statusError, "saving repo link: "+err.Error())
	}
}

// runInput asks for an input action's text.
func (m *Model) runInput(r *pendingRun) (tea.Cmd, bool) {
	if r.action.Input == "" {
		return nil, false
	}
	m.input = newInputBox(r.action.Input, "optional; leave empty to skip")
	m.input.onSubmit = func(text string) tea.Cmd {
		m.input, r.input = nil, text
		return m.resumeRun()
	}
	return nil, true
}

// runForm opens the action's form, starting each remembered field on its
// most recent valid choice for the project.
func (m *Model) runForm(r *pendingRun) (tea.Cmd, bool) {
	if len(r.action.Form.Fields) == 0 {
		return nil, false
	}
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
	return nil, true
}

func (m *Model) typedActionForm(values map[string]string) tea.Cmd {
	m.form = nil
	r := m.run
	r.formValues = values
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
				m.setStatus(statusError, "saving action choices: "+err.Error())
			}
		}
	}
	return m.resumeRun()
}

// runProjectFields loads the project's fields when the ticket has same-named
// fields, so the action sees the project's one. The projectFieldsMsg handler
// resumes the run.
func (m *Model) runProjectFields(r *pendingRun) (tea.Cmd, bool) {
	p := r.project
	if p == nil || !sharesFieldNames(r.ticket.Task) {
		return nil, false
	}
	if _, ok := m.projectFields[p.GID]; ok {
		return nil, false
	}
	r.awaitingFields = true
	m.setProgress("loading project fields…")
	return loadProjectFields(m.deps.Client, p.GID), true
}

// runBranch picks the branch, asking for one when the command uses it and
// the branch field is empty. A typed branch other than the fallback is saved
// for the ticket, so agents on it link.
func (m *Model) runBranch(r *pendingRun) (tea.Cmd, bool) {
	preferred := m.projectFields[gidOf(r.project)]
	saved := m.deps.State.TaskBranches[r.ticket.GID]
	r.branch = action.Branch(r.ticket.Task, saved, m.deps.Config.BranchField, preferred)
	if !strings.Contains(r.action.Command, "ASANAMATE_BRANCH") ||
		action.BranchWarning(r.ticket.Task, saved, m.deps.Config.BranchField, preferred) == "" {
		return nil, false
	}
	m.input = newInputBox(fmt.Sprintf("Branch (%q is empty)", m.deps.Config.BranchField), "empty uses the ID field or title slug")
	m.input.area.SetValue(r.branch)
	m.input.onSubmit = func(text string) tea.Cmd {
		m.input = nil
		fallback := action.DefaultBranch(r.ticket.Task)
		if r.branch = cmp.Or(text, fallback); r.branch != fallback {
			m.saveBranch(r.ticket.GID, r.branch)
		}
		return m.resumeRun()
	}
	return nil, true
}

// launch starts the run's action in its mode.
func (m *Model) launch(r *pendingRun) tea.Cmd {
	m.run = nil
	preferred := m.projectFields[gidOf(r.project)]
	linked := m.ticketAgents(r.ticket.Task, preferred)
	agent := r.agent
	if agent == nil {
		agent = firstAgent(linked)
	}
	files, err := action.WriteFiles(m.deps.StateDir, r.ticket)
	if err != nil {
		m.setStatus(statusError, "writing ticket files: "+err.Error())
		return nil
	}
	if r.action.Input != "" {
		if err := files.WriteInput(r.input); err != nil {
			m.setStatus(statusError, "writing input: "+err.Error())
			return nil
		}
	}
	var worktree string
	if r.repo != "" {
		worktree = repo.Worktree(r.repo, r.branch)
	}
	cmd := action.Command(r.action, action.Context{
		Ticket:        r.ticket,
		Project:       r.project,
		Repo:          r.repo,
		Worktree:      worktree,
		ConfirmWrites: m.deps.Config.ConfirmWritesFor(r.action),
		Files:         files,
		Preferred:     preferred,
		BranchField:   m.deps.Config.BranchField,
		Branch:        r.branch,
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
		m.setProgress(name + ": running…")
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
	linked := m.viewAgents(t)
	notes, hidden := m.agentNotes(t)
	if len(linked) == 0 && len(notes) == 0 && hidden == 0 {
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
	if hidden > 0 {
		fmt.Fprintf(&b, "- *%s*\n", otherAgents(hidden))
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
		m.setStatus(statusWarning, "ticket details are still loading")
		return
	}
	if len(t.Attachments) == 0 {
		m.setStatus(statusInfo, "no attachments")
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

// openContentLinks lists the links in the ticket's description and comments,
// by label and URL; picking one opens it, an Asana task link in asanamate
// and any other in the browser, and copy copies it.
func (m *Model) openContentLinks() {
	t, ok := m.selectedDetail()
	if !ok {
		m.setStatus(statusWarning, "ticket details are still loading")
		return
	}
	links := t.Links()
	if len(links) == 0 {
		m.setStatus(statusInfo, "no links")
		return
	}
	items := make([]pickItem, len(links))
	for i, l := range links {
		items[i] = pickItem{Label: cmp.Or(l.Label, l.URL), Value: l.URL}
		if l.Label != "" {
			items[i].Hint, items[i].Search = l.URL, l.URL
		}
	}
	p := newPicker(func(res pickResult) tea.Cmd {
		m.modal = nil
		url := res.item.Value.(string)
		if res.copy {
			m.setStatus(statusInfo, "link sent to clipboard")
			return tea.SetClipboard(url)
		}
		if gid := asana.TaskGIDFromURL(url); gid != "" {
			return m.openTicket(gid, res.item.Label)
		}
		return openURL(url)
	}, "Links", items)
	p.links = true
	m.modal = p
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
	m.setProgress("loading image…")
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
