// Package tui is the asanamate terminal interface.
package tui

import (
	"cmp"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const narrowWidth = 100

// minPaneW is the narrowest a fitted list or its reader gets.
const minPaneW = 30

// Deps are the services the TUI uses.
type Deps struct {
	Config   config.Config
	State    *state.State
	Client   *asana.Client
	StateDir string
	Images   bool
	InTmux   bool
	// Symbols is the resolved symbol set: unicode, nerd, or ascii.
	Symbols string
	// ReducedMotion shows static symbols instead of the working spinner.
	ReducedMotion bool
	// NoPreview hides the reading pane and gives the list the full width.
	NoPreview bool
}

// Model is the Bubble Tea model for asanamate.
type Model struct {
	deps          Deps
	width, height int

	viewProject *asana.Ref
	tasks       []asana.Task
	visible     []asana.Task
	groups      []string       // group label per visible task; nil when ungrouped
	groupBy     string         // list field the list is grouped by; "" for none
	listW       int            // fitted list pane width; 0 for the default split
	accentStyle lipgloss.Style // reader headings
	headerStyle lipgloss.Style // group headers
	markerStyle lipgloss.Style // selected ticket marker
	cursor      int
	loading     bool // tasks are loading; the stale view stays frozen under a modal

	filterInput textinput.Model
	filtering   bool

	focusReader bool
	reader      viewport.Model
	readerView  string // config.ViewCards or config.ViewMarkdown
	renderers   map[rendererKey]*glamour.TermRenderer
	details     map[string]ticket.Ticket
	shownGID    string
	shownAgents string         // agents section rendered for shownGID
	fieldKey    string         // cards view row tabbed to; "" for none
	fieldLines  map[string]int // reader line of each cards view row, by key

	projects      []asana.Project
	projectFields map[string]map[string]bool // project gid -> its custom field gids
	agents        []agents.Agent
	linked        map[string][]agents.Agent // viewAgents by ticket gid; nil when stale
	agentsErr     string
	sym           symbolSet
	frame         int  // spinner frame
	spinning      bool // a spinner tick is scheduled
	modal         *picker
	input         *inputBox // free-text modal for input actions
	run           *pendingRun
	edit          *pendingEdit
	users         []asana.Ref // workspace users, loaded on first assign
	menuFor       string      // gid whose menu opens once its details arrive
	menuOpen      func()      // opens that menu
	status        string
	exitCmd       *exec.Cmd
}

// New returns a model that starts on My Tasks with its last used grouping and filter.
func New(d Deps) *Model {
	in := textinput.New()
	in.Prompt = "/"
	m := &Model{
		deps:          d,
		filterInput:   in,
		reader:        viewport.New(),
		readerView:    d.Config.Reader.View,
		accentStyle:   colorStyle(d.Config.AccentColor),
		headerStyle:   colorStyle(cmp.Or(d.Config.List.Header.Color, d.Config.AccentColor)),
		markerStyle:   colorStyle(cmp.Or(d.Config.List.Selection.Color, d.Config.AccentColor)),
		renderers:     map[rendererKey]*glamour.TermRenderer{},
		details:       map[string]ticket.Ticket{},
		projectFields: map[string]map[string]bool{},
		sym:           newSymbols(d.Symbols, d.Config.Agents.Symbols, d.ReducedMotion),
		loading:       true,
	}
	m.restoreView()
	return m
}

// colorStyle is bold text in a configured color.
func colorStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
}

// ExitCommand is the command an exit-mode action left to run after the TUI quits.
func (m *Model) ExitCommand() *exec.Cmd { return m.exitCmd }

func (m *Model) Init() tea.Cmd {
	cmd := loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject)
	if m.deps.Config.AgentsEnabled() {
		cmd = tea.Batch(cmd, loadAgents(m.deps.Config.Agents))
	}
	return cmd
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case tasksMsg:
		if !sameProject(msg.project, m.viewProject) {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.status = "loading tasks: " + msg.err.Error()
			return m, nil
		}
		if msg.fields != nil {
			m.projectFields[msg.project.GID] = msg.fields
		}
		prev, _ := m.selected()
		m.tasks, m.linked = msg.tasks, nil
		m.applyFilter()
		// A reload that drops the selected ticket starts from the top.
		if t, _ := m.selected(); t.GID != prev.GID {
			m.cursor = 0
		}
		m.fitList()
		return m, m.selectionChanged()
	case detailTickMsg:
		if t, ok := m.selected(); ok && t.GID == msg.gid {
			if _, cached := m.details[msg.gid]; !cached {
				return m, loadDetail(m.deps.Client, msg.gid)
			}
		}
	case detailMsg:
		t, ok := m.selected()
		isSelected := ok && t.GID == msg.gid
		if msg.err != nil {
			if isSelected {
				m.status = "loading ticket: " + msg.err.Error()
			}
			if m.menuFor == msg.gid {
				m.menuFor = ""
			}
			return m, nil
		}
		m.details[msg.gid], m.linked = msg.ticket, nil
		if isSelected {
			m.renderDetail(false)
			if m.menuFor == msg.gid {
				m.menuFor = ""
				m.menuOpen()
			}
		}
	case projectsMsg:
		if msg.err != nil {
			m.status = "loading projects: " + msg.err.Error()
			return m, nil
		}
		m.status = ""
		m.projects = msg.projects
		m.openProjectPicker()
	case agentsMsg:
		if !m.deps.Config.AgentsEnabled() {
			return m, nil
		}
		if msg.err != nil {
			if s := "agents: " + msg.err.Error(); s != m.agentsErr {
				m.status, m.agentsErr = s, s
			}
		} else {
			m.agents, m.agentsErr, m.linked = msg.list, "", nil
			m.applyFilter()
			if t, ok := m.selectedDetail(); ok && t.GID == m.shownGID && m.agentsSection(t.Task) != m.shownAgents {
				m.renderDetail(true)
			}
		}
		return m, tea.Batch(scheduleAgents(), m.startSpinner())
	case spinnerTickMsg:
		if !m.animating() {
			m.spinning = false
			return m, nil
		}
		m.frame++
		return m, scheduleSpinner()
	case agentTickMsg:
		return m, loadAgents(m.deps.Config.Agents)
	case projectFieldsMsg:
		if msg.err != nil {
			m.status = "loading project fields: " + msg.err.Error()
		}
		m.projectFields[msg.gid], m.linked = msg.fields, nil
		if m.run != nil && m.run.awaitingFields {
			m.run.awaitingFields = false
			return m, m.execute(m.run.repo)
		}
	case candidatesMsg:
		m.openRepoPicker(msg)
	case actionDoneMsg:
		m.status = actionStatus(msg)
	case sectionsMsg:
		m.openSectionPicker(msg)
	case usersMsg:
		if msg.err == nil {
			m.users = msg.users
		}
		m.openUserPicker(msg.err)
	case editDoneMsg:
		if msg.err != nil {
			m.status = msg.what + " failed: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.what + ": done"
		delete(m.details, msg.gid)
		if m.shownGID == msg.gid {
			m.shownGID = ""
		}
		return m, m.reload()
	case imageMsg:
		if msg.err != nil {
			m.status = "image: " + msg.err.Error() + "; opening in browser"
			return m, openURL(msg.url)
		}
		m.status = ""
		viewer := kitty.NewViewer(msg.payload, m.deps.InTmux)
		return m, tea.Exec(viewer, func(err error) tea.Msg { return actionDoneMsg{name: "image viewer", err: err} })
	case statusMsg:
		m.status = string(msg)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func sameProject(a, b *asana.Ref) bool {
	return gidOf(a) == gidOf(b)
}

// gidOf returns the project's gid, or "" for My Tasks (nil).
func gidOf(project *asana.Ref) string {
	if project == nil {
		return ""
	}
	return project.GID
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.input != nil {
		return m.updateInput(msg)
	}
	if m.modal != nil {
		return m.updateModal(msg)
	}
	if m.loading {
		if k == "q" {
			return tea.Quit
		}
		return nil
	}
	m.status = ""
	if m.filtering {
		return m.updateFilter(msg)
	}
	if k == "enter" && m.focusReader && m.fieldKey != "" {
		return m.openField(m.fieldKey)
	}
	switch k {
	case "q":
		return tea.Quit
	case "tab", "shift+tab":
		if m.deps.NoPreview {
			return nil
		}
		dir := 1
		if k == "shift+tab" {
			dir = -1
		}
		if !m.focusReader {
			m.focusReader = true
			m.stepField(dir)
		} else if !m.stepField(dir) {
			m.focusReader = false
		}
		return nil
	case "esc":
		if m.focusReader {
			m.focusReader = false
			m.clearField()
		}
		return nil
	case "/":
		m.filtering = true
		return m.filterInput.Focus()
	case "p":
		if m.projects != nil {
			m.openProjectPicker()
			return nil
		}
		m.status = "loading projects…"
		return loadProjects(m.deps.Client, m.deps.Config.Workspace)
	case "r":
		m.details = map[string]ticket.Ticket{}
		m.shownGID = ""
		return m.reload()
	case "o":
		if t, ok := m.selected(); ok {
			return openURL(t.PermalinkURL)
		}
		return nil
	case "a", "enter":
		return m.requestMenu(m.openActionMenu)
	case "e":
		return m.requestMenu(m.openEditMenu)
	case "f":
		m.openAttachments()
		return nil
	case "b":
		m.openGroupPicker()
		return nil
	case "=":
		m.fitList()
		return nil
	case "v":
		if m.deps.NoPreview {
			return nil
		}
		if m.readerView == config.ViewCards {
			m.readerView = config.ViewMarkdown
		} else {
			m.readerView = config.ViewCards
		}
		m.fieldKey = ""
		m.renderDetail(false)
		return nil
	}
	if m.focusReader {
		var cmd tea.Cmd
		m.reader, cmd = m.reader.Update(msg)
		return cmd
	}
	switch k {
	case "j", "down":
		m.moveTo(m.cursor + 1)
	case "k", "up":
		m.moveTo(m.cursor - 1)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.visible) - 1)
	default:
		return nil
	}
	return m.selectionChanged()
}

func (m *Model) updateModal(msg tea.KeyPressMsg) tea.Cmd {
	p := m.modal
	res, cmd := p.update(msg)
	switch {
	case res.cancelled:
		m.modal, m.run, m.edit = nil, nil, nil
	case res.done:
		return tea.Batch(cmd, m.handlePick(p.kind, res))
	}
	return cmd
}

func (m *Model) updateInput(msg tea.KeyPressMsg) tea.Cmd {
	res, cmd := m.input.update(msg)
	switch {
	case res.cancelled:
		m.input, m.run, m.edit = nil, nil, nil
	case res.done:
		if m.run == nil {
			return tea.Batch(cmd, m.typedEdit(res.free))
		}
		return tea.Batch(cmd, m.typedInput(res.free))
	}
	return cmd
}

func (m *Model) handlePick(kind pickKind, res pickResult) tea.Cmd {
	switch kind {
	case pickProject:
		return m.pickedProject(res.item.Value.(*asana.Ref))
	case pickAction:
		return m.pickedAction(res.item.Value.(int))
	case pickTicketProject:
		return m.pickedTicketProject(res.item.Value.(asana.Ref))
	case pickRepo:
		path := res.free
		if res.item != nil {
			path = res.item.Value.(string)
		}
		return m.pickedRepo(path)
	case pickAttachment:
		return m.pickedAttachment(res.item.Value.(asana.Attachment))
	case pickAgent:
		return m.pickedAgent(res.item.Value.(agents.Agent))
	case pickGroup:
		return m.pickedGroup(res.item.Value.(string))
	case pickBranchFallback:
		return m.pickedBranchFallback(res.item.Value.(bool))
	case pickEdit:
		return m.pickedEdit(res.item.Value.(editOp))
	case pickEditProject:
		return m.pickedEditProject(res.item.Value.(asana.Ref))
	case pickSection:
		return m.pickedSection(res.item.Value.(asana.Ref))
	case pickField:
		return m.pickedField(res.item.Value.(asana.CustomField))
	case pickEnumOption:
		return m.setField(res.item.Value.(string))
	case pickUser:
		return m.pickedUser(res.item.Value.(asana.Ref))
	case pickMultiEnum, pickPeople:
		return m.pickedValues(res.items)
	}
	return nil
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
		m.filterInput.Blur()
		m.saveView()
		m.fitList()
		return nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	return tea.Batch(cmd, m.selectionChanged())
}

// reload refetches the tasks, keeping the current view until they land.
func (m *Model) reload() tea.Cmd {
	m.loading = true
	return tea.Batch(loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject), m.startSpinner())
}

// applyFilter recomputes the visible tasks and their groups, keeping the
// selected ticket selected when it is still visible.
func (m *Model) applyFilter() {
	var prevGID string
	if prev, ok := m.selected(); ok {
		prevGID = prev.GID
	}
	visible := filter.Parse(m.filterInput.Value()).Apply(m.tasks, m.agentStates)
	m.visible, m.groups = groupTasks(visible, m.groupBy, m.rowContext(), time.Now())
	if prevGID != "" {
		for i, t := range m.visible {
			if t.GID == prevGID {
				m.cursor = i
				break
			}
		}
	}
	m.moveTo(m.cursor)
}

// animating reports whether anything shown uses the spinner.
func (m *Model) animating() bool { return m.loading || m.anyWorking() }

// startSpinner schedules a spinner tick unless one is scheduled or nothing animates.
func (m *Model) startSpinner() tea.Cmd {
	if m.spinning || m.sym.spinner == nil || !m.animating() {
		return nil
	}
	m.spinning = true
	return scheduleSpinner()
}

func (m *Model) anyWorking() bool {
	for _, a := range m.agents {
		if a.State == agents.Working {
			return true
		}
	}
	return false
}

func (m *Model) moveTo(i int) {
	m.cursor = max(min(i, len(m.visible)-1), 0)
}

func (m *Model) selected() (asana.Task, bool) {
	if m.cursor < len(m.visible) {
		return m.visible[m.cursor], true
	}
	return asana.Task{}, false
}

func (m *Model) selectedDetail() (ticket.Ticket, bool) {
	t, ok := m.selected()
	if !ok {
		return ticket.Ticket{}, false
	}
	d, ok := m.details[t.GID]
	return d, ok
}

func (m *Model) selectionChanged() tea.Cmd {
	t, ok := m.selected()
	if !ok {
		m.shownGID = ""
		m.reader.SetContent("")
		return nil
	}
	if t.GID == m.shownGID {
		return nil
	}
	m.fieldKey = ""
	if _, cached := m.details[t.GID]; cached {
		m.renderDetail(false)
		return nil
	}
	m.shownGID = ""
	m.reader.SetContent(dimStyle.Render("Loading " + ticket.Clean(t.Name) + "…"))
	return scheduleDetail(t.GID)
}

// renderDetail renders the selected ticket in the reader, from the top or at
// the current scroll position.
func (m *Model) renderDetail(keepScroll bool) {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	if m.deps.NoPreview {
		m.shownGID = t.GID
		return
	}
	_, readerW, _ := m.paneWidths()
	section := m.agentsSection(t.Task)
	offset := m.reader.YOffset()
	width := max(readerW-2, 20)
	if m.readerView == config.ViewMarkdown {
		m.reader.SetContent(m.glamour(t.MarkdownWith(section), width, false))
	} else {
		m.reader.SetContent(lipgloss.NewStyle().PaddingLeft(1).Render(m.renderCards(t, width)))
	}
	if keepScroll {
		m.reader.SetYOffset(offset)
	} else {
		m.reader.GotoTop()
	}
	m.shownGID, m.shownAgents = t.GID, section
}

// stepField moves the cards view's tabbed-to row by dir, wrapping, and
// scrolls it into view. It reports false when there is no row to move to.
func (m *Model) stepField(dir int) bool {
	t, ok := m.selectedDetail()
	if !ok || m.readerView == config.ViewMarkdown {
		return false
	}
	keys := fieldTargets(t)
	i := slices.Index(keys, m.fieldKey)
	switch {
	case i < 0 && dir < 0:
		i = len(keys) - 1
	case i < 0:
		i = 0
	default:
		i = (i + dir + len(keys)) % len(keys)
	}
	m.fieldKey = keys[i]
	m.renderDetail(true)
	line, top, h := m.fieldLines[m.fieldKey], m.reader.YOffset(), m.reader.Height()
	switch {
	case line < top:
		m.reader.SetYOffset(line)
	case line >= top+h:
		m.reader.SetYOffset(line - h + 1)
	}
	return true
}

// clearField drops the cards view's tabbed-to row.
func (m *Model) clearField() {
	if m.fieldKey != "" {
		m.fieldKey = ""
		m.renderDetail(true)
	}
}

// rendererKey identifies a cached Markdown renderer. Bare renderers drop the
// document margins, for Markdown set inside cards and sections.
type rendererKey struct {
	width int
	bare  bool
}

func (m *Model) glamour(md string, width int, bare bool) string {
	key := rendererKey{width, bare}
	r := m.renderers[key]
	if r == nil {
		style := *styles.DefaultStyles[m.deps.Config.Theme]
		if bare {
			style.Document.Margin = new(uint)
			style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
		}
		var err error
		if r, err = glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width)); err != nil {
			return md
		}
		m.renderers[key] = r
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

func (m *Model) openProjectPicker() {
	items := []pickItem{{Label: "My Tasks", Value: (*asana.Ref)(nil)}}
	byGID := map[string]asana.Project{}
	for _, p := range m.projects {
		byGID[p.GID] = p
	}
	seen := map[string]bool{}
	for _, gid := range m.deps.State.RecentProjects {
		if p, ok := byGID[gid]; ok {
			items = append(items, pickItem{Label: ticket.Clean(p.Name), Hint: "recent", Value: &asana.Ref{GID: p.GID, Name: p.Name}})
			seen[gid] = true
		}
	}
	rest := make([]asana.Project, 0, len(m.projects))
	for _, p := range m.projects {
		if !seen[p.GID] {
			rest = append(rest, p)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return strings.ToLower(rest[i].Name) < strings.ToLower(rest[j].Name) })
	for _, p := range rest {
		items = append(items, pickItem{Label: ticket.Clean(p.Name), Value: &asana.Ref{GID: p.GID, Name: p.Name}})
	}
	m.modal = newPicker(pickProject, "Switch project", items)
}

func (m *Model) pickedProject(ref *asana.Ref) tea.Cmd {
	m.modal = nil
	m.viewProject, m.linked = ref, nil
	m.restoreView()
	if ref != nil {
		m.deps.State.TouchProject(ref.GID)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving state: " + err.Error()
		}
	}
	return m.reload()
}

// restoreView applies the viewed project's last used grouping and filter, else
// the configured group_by and default_filter.
func (m *Model) restoreView() {
	v, ok := m.deps.State.View(gidOf(m.viewProject))
	if !ok {
		v = state.View{GroupBy: strings.TrimSpace(m.deps.Config.List.GroupBy), Filter: m.deps.Config.DefaultFilter}
	}
	m.groupBy = v.GroupBy
	m.filterInput.SetValue(v.Filter)
}

// saveView remembers the viewed project's grouping and filter.
func (m *Model) saveView() {
	m.deps.State.SetView(gidOf(m.viewProject), state.View{GroupBy: m.groupBy, Filter: m.filterInput.Value()})
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving state: " + err.Error()
	}
}

func (m *Model) paneWidths() (listW, readerW int, split bool) {
	if m.deps.NoPreview || m.width < narrowWidth {
		return m.width, m.width, false
	}
	listW = m.width * 2 / 5
	if m.listW > 0 {
		listW = min(max(m.listW, minPaneW), m.width-1-minPaneW)
	}
	return listW, m.width - listW - 1, true
}

func (m *Model) bodyHeight() int { return max(m.height-2, 1) }

func (m *Model) layout() {
	_, readerW, _ := m.paneWidths()
	if readerW != m.reader.Width() {
		m.renderers = map[rendererKey]*glamour.TermRenderer{}
	}
	m.reader.SetWidth(readerW)
	m.reader.SetHeight(m.bodyHeight())
	m.filterInput.SetWidth(max(m.width/3, 10))
	if m.shownGID != "" {
		m.renderDetail(false)
	}
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.header() + "\n" + m.body() + "\n" + m.footer())
	v.AltScreen = true
	return v
}

func (m *Model) header() string {
	name := "My Tasks"
	if m.viewProject != nil {
		name = ticket.Clean(m.viewProject.Name)
	}
	f := dimStyle.Render("filter: " + m.filterInput.Value())
	if m.filtering {
		f = m.filterInput.View()
	}
	count := dimStyle.Render(fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks)))
	line := titleStyle.Render("asanamate · "+name) + "  " + f + "  " + count
	if m.groupBy != "" {
		line += "  " + dimStyle.Render("group: "+m.groupBy)
	}
	// Tickets can share a branch, so count each agent once.
	var linked []agents.Agent
	seen := map[agents.Agent]bool{}
	for _, t := range m.tasks {
		for _, a := range m.viewAgents(t) {
			if !seen[a] {
				seen[a] = true
				linked = append(linked, a)
			}
		}
	}
	if s := m.sym.summary(linked, m.frame); s != "" {
		line += "  " + dimStyle.Render("agents") + " " + s
	}
	return ansi.Truncate(line, m.width, "…")
}

func (m *Model) footer() string {
	s := m.status
	if s == "" {
		hints := "j/k move · enter actions · e edit · tab focus · / filter · b group · = fit · p projects · f files · v view · o open · r reload · q quit"
		if m.deps.NoPreview {
			hints = strings.NewReplacer(" · tab focus", "", " · v view", "").Replace(hints)
		}
		if m.focusReader && m.fieldKey != "" {
			hints = "tab/shift+tab field · enter edit field · esc list · e edit · v view · q quit"
		}
		s = dimStyle.Render(hints)
	}
	return ansi.Truncate(s, m.width, "…")
}

func (m *Model) body() string {
	h := m.bodyHeight()
	panes := m.panes(h)
	w, mh := max(min(m.width-4, 80), 10), max(h-2, 3)
	var content string
	style := modalStyle
	switch {
	case m.input != nil:
		content = m.input.view(w, mh)
	case m.modal != nil:
		content = m.modal.view(w, mh)
	case m.loading:
		content = "Loading tasks…"
		if m.sym.spinner != nil {
			content = m.sym.spinner[m.frame%len(m.sym.spinner)] + " " + content
		}
		style = loadingStyle
	default:
		return panes
	}
	// Float the modal over the panes, faded so the modal stands out.
	box := style.Render(content)
	x := max((m.width-lipgloss.Width(box))/2, 0)
	y := max((h-lipgloss.Height(box))/2, 0)
	canvas := lipgloss.NewCanvas(m.width, h).Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(dimStyle.Render(ansi.Strip(panes))),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	))
	// Canvas trims trailing blanks; restore the full body height.
	return lipgloss.NewStyle().Height(h).Render(canvas.Render())
}

func (m *Model) panes(h int) string {
	listW, _, split := m.paneWidths()
	contentW := listW
	if split {
		contentW = max(listW-1, 1) // keep a gap before the divider
	}
	list := lipgloss.NewStyle().Width(listW).Height(h).Render(m.listView(contentW, h))
	switch {
	case split:
		sep := dimStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, list, sep, m.reader.View())
	case m.focusReader:
		return m.reader.View()
	default:
		return list
	}
}

// selectionBar reports whether the selected ticket is drawn as a reversed bar
// (the list has focus and selection.style is bar).
func (m *Model) selectionBar() bool {
	return m.deps.Config.List.Selection.Style == config.StyleBar && !m.focusReader
}

// renderRowLine fits line into width with tail right-aligned. A selected line
// is bold, or with selectionBar highlighted across the row up to the tail,
// which styles itself.
func (m *Model) renderRowLine(line, tail string, width int, selected bool) string {
	tailW := ansi.StringWidth(tail)
	if tail != "" && width-tailW-1 < 8 {
		tail, tailW = "", 0
	}
	room := width
	if tail != "" {
		room = width - tailW - 1
	}
	line = ansi.Truncate(line, room, "…")
	switch {
	case selected && m.selectionBar():
		return selectedStyle.Render(ansi.Strip(line)+strings.Repeat(" ", max(width-tailW-ansi.StringWidth(line), 0))) + tail
	case selected:
		line = titleStyle.Render(ansi.Strip(line))
	}
	if tail == "" {
		return line
	}
	return line + strings.Repeat(" ", max(width-ansi.StringWidth(line)-tailW, 1)) + tail
}

func (m *Model) listView(width, height int) string {
	switch {
	case m.loading && m.tasks == nil:
		return ""
	case len(m.visible) == 0:
		return dimStyle.Render("No tasks match the filter.")
	}
	itemH, sepH := 1, 0
	if m.deps.Config.List.Layout == config.LayoutMulti {
		itemH = 2
	}
	if m.deps.Config.List.Separator {
		sepH = 1
	}
	// Separators frame every item and neighbours share one; a group header
	// takes the place of the separator above its item. The first row shown
	// always has its group header. Headers get header.spacing blank lines
	// below, and above too unless they top the view.
	spacing := m.deps.Config.List.Header.Spacing
	header := func(i, start int) bool {
		return m.groups != nil && (i == start || m.groups[i] != m.groups[i-1])
	}
	above := func(i, start int) int {
		if i == start {
			return 0
		}
		return spacing
	}
	rowH := func(i, start int) int {
		if header(i, start) {
			return above(i, start) + itemH + 1 + spacing
		}
		return itemH + sepH
	}
	// Scroll so the cursor's row is the last that fits, then fill below it.
	start, used := m.cursor, sepH+rowH(m.cursor, m.cursor)
	for start > 0 {
		next := used - rowH(start, start) + rowH(start, start-1) + rowH(start-1, start-1)
		if next > height {
			break
		}
		start, used = start-1, next
	}
	end := m.cursor + 1
	for end < len(m.visible) && used+rowH(end, start) <= height {
		used += rowH(end, start)
		end++
	}
	counts := m.groupCounts()
	sep := dimStyle.Render(strings.Repeat("─", width))
	// The marker style keeps a gutter on every row so text doesn't shift.
	var gutter string
	cursor := m.marker()
	if cursor != "" {
		gutter = strings.Repeat(" ", ansi.StringWidth(cursor))
		cursorStyle := m.markerStyle
		if m.focusReader {
			cursorStyle = dimStyle
		}
		cursor = cursorStyle.Render(cursor)
	}
	rowW := max(width-ansi.StringWidth(gutter), 1)
	var lines []string
	for i := start; i < end; i++ {
		switch {
		case header(i, start):
			for range above(i, start) {
				lines = append(lines, "")
			}
			lines = append(lines, m.groupHeader(m.groups[i], counts[m.groups[i]], width))
			for range spacing {
				lines = append(lines, "")
			}
		case sepH > 0:
			lines = append(lines, sep)
		}
		row, badge := m.listRow(i)
		for j, line := range row {
			tail := ""
			if j == 0 && badge != "" {
				tail = badge
			}
			lead := gutter
			if i == m.cursor {
				lead = cursor
			}
			lines = append(lines, lead+m.renderRowLine(line, tail, rowW, i == m.cursor))
		}
	}
	if sepH > 0 {
		lines = append(lines, sep)
	}
	return strings.Join(lines, "\n")
}

// groupCounts returns the number of visible tickets per group label.
func (m *Model) groupCounts() map[string]int {
	counts := map[string]int{}
	for _, g := range m.groups {
		counts[g]++
	}
	return counts
}

// marker is the selected row's left marker with its gap, or "" unless
// selection.style is marker.
func (m *Model) marker() string {
	if m.deps.Config.List.Selection.Style != config.StyleMarker {
		return ""
	}
	return m.sym.cursor + " "
}

// listRow returns the lines of visible ticket i's row and the badge
// right-aligned on its first line.
func (m *Model) listRow(i int) (row []string, badge string) {
	t := m.visible[i]
	mark := m.sym.open
	if t.Completed {
		mark = m.sym.done
	}
	title := mark + " " + ticket.OneLine(t.Name)
	linked := m.viewAgents(t)
	badge = m.sym.badge(linked, m.frame, i == m.cursor && m.selectionBar())
	if len(linked) > 0 && linked[0].State == agents.Waiting && i != m.cursor {
		title = stateStyles[agents.Waiting].Render(title)
	}
	details := strings.Join(rowFields(t, m.deps.Config.List.Fields, m.rowContext()), " · ")
	row = []string{title}
	switch {
	case m.deps.Config.List.Layout == config.LayoutMulti && details != "":
		row = append(row, dimStyle.Render("  "+details))
	case m.deps.Config.List.Layout == config.LayoutMulti:
		row = append(row, "")
	case details != "":
		row[0] += "  " + dimStyle.Render(details)
	}
	return row, badge
}

// fitList sizes the list pane to its widest row or group header, leaving the
// reader at least minPaneW columns.
func (m *Model) fitList() {
	gutter := ansi.StringWidth(m.marker())
	w := 0
	for label, n := range m.groupCounts() {
		w = max(w, groupHeaderWidth(label, n))
	}
	for i := range m.visible {
		row, badge := m.listRow(i)
		for j, line := range row {
			lineW := gutter + ansi.StringWidth(line)
			if j == 0 && badge != "" {
				lineW += 1 + ansi.StringWidth(badge)
			}
			w = max(w, lineW)
		}
	}
	m.listW = w + 1 // the gap before the divider
	m.layout()
}
