// Package tui is the asanamate terminal interface.
package tui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

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
	cursor      int
	loading     bool

	filterInput textinput.Model
	filtering   bool

	focusReader bool
	reader      viewport.Model
	readerView  string // config.ViewCards or config.ViewMarkdown
	renderers   map[rendererKey]*glamour.TermRenderer
	details     map[string]ticket.Ticket
	shownGID    string
	shownAgents string // agents section rendered for shownGID

	projects      []asana.Project
	projectFields map[string]map[string]bool // project gid -> its custom field gids
	agents        []agents.Agent
	agentsErr     string
	sym           symbolSet
	frame         int  // spinner frame
	spinning      bool // a spinner tick is scheduled
	modal         *picker
	run           *pendingRun
	menuFor       string // gid whose action menu opens once its details arrive
	status        string
	exitCmd       *exec.Cmd
}

// New returns a model that starts on My Tasks with the configured default filter.
func New(d Deps) *Model {
	in := textinput.New()
	in.Prompt = "/"
	in.SetValue(d.Config.DefaultFilter)
	return &Model{
		deps:          d,
		filterInput:   in,
		reader:        viewport.New(),
		readerView:    d.Config.Reader.View,
		renderers:     map[rendererKey]*glamour.TermRenderer{},
		details:       map[string]ticket.Ticket{},
		projectFields: map[string]map[string]bool{},
		sym:           newSymbols(d.Symbols, d.Config.Agents.Symbols, d.ReducedMotion),
		loading:       true,
	}
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
		m.tasks = msg.tasks
		m.applyFilter()
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
		m.details[msg.gid] = msg.ticket
		if isSelected {
			m.showDetail()
			if m.menuFor == msg.gid {
				m.menuFor = ""
				m.openActionMenu()
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
			m.agents, m.agentsErr = msg.list, ""
			m.applyFilter()
			if t, ok := m.selectedDetail(); ok && t.GID == m.shownGID && m.agentsSection(t.Task) != m.shownAgents {
				m.renderDetail(true)
			}
		}
		cmds := []tea.Cmd{scheduleAgents()}
		if !m.spinning && m.sym.spinner != nil && m.anyWorking() {
			m.spinning = true
			cmds = append(cmds, scheduleSpinner())
		}
		return m, tea.Batch(cmds...)
	case spinnerTickMsg:
		if !m.anyWorking() {
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
		m.projectFields[msg.gid] = msg.fields
		if m.run != nil && m.run.awaitingFields {
			m.run.awaitingFields = false
			return m, m.execute(m.run.repo)
		}
	case candidatesMsg:
		m.openRepoPicker(msg)
	case actionDoneMsg:
		m.status = actionStatus(msg)
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
	if m.modal != nil {
		return m.updateModal(msg)
	}
	m.status = ""
	if m.filtering {
		return m.updateFilter(msg)
	}
	switch k {
	case "q":
		return tea.Quit
	case "tab":
		if !m.deps.NoPreview {
			m.focusReader = !m.focusReader
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
		return m.requestActionMenu()
	case "f":
		m.openAttachments()
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
		m.modal, m.run = nil, nil
	case res.done:
		return tea.Batch(cmd, m.handlePick(p.kind, res))
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
	}
	return nil
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
		m.filterInput.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	return tea.Batch(cmd, m.selectionChanged())
}

func (m *Model) reload() tea.Cmd {
	m.loading = true
	m.tasks, m.visible, m.cursor = nil, nil, 0
	return loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject)
}

// applyFilter recomputes the visible tasks, keeping the selected ticket
// selected when it is still visible.
func (m *Model) applyFilter() {
	prev, hadSelection := m.selected()
	m.visible = filter.Parse(m.filterInput.Value()).Apply(m.tasks, m.agentStates)
	if hadSelection {
		for i, t := range m.visible {
			if t.GID == prev.GID {
				m.cursor = i
				break
			}
		}
	}
	m.moveTo(m.cursor)
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
	if _, cached := m.details[t.GID]; cached {
		m.showDetail()
		return nil
	}
	m.shownGID = ""
	m.reader.SetContent(dimStyle.Render("Loading " + ticket.Clean(t.Name) + "…"))
	return scheduleDetail(t.GID)
}

func (m *Model) showDetail() { m.renderDetail(false) }

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
	m.viewProject = ref
	if ref != nil {
		m.deps.State.TouchProject(ref.GID)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving state: " + err.Error()
		}
	}
	return m.reload()
}

func (m *Model) paneWidths() (listW, readerW int, split bool) {
	if m.deps.NoPreview || m.width < narrowWidth {
		return m.width, m.width, false
	}
	listW = m.width * 2 / 5
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
		m.showDetail()
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
		hints := "j/k move · enter actions · tab focus · / filter · p projects · f files · v view · o open · r reload · q quit"
		if m.deps.NoPreview {
			hints = strings.NewReplacer(" · tab focus", "", " · v view", "").Replace(hints)
		}
		s = dimStyle.Render(hints)
	}
	return ansi.Truncate(s, m.width, "…")
}

func (m *Model) body() string {
	h := m.bodyHeight()
	panes := m.panes(h)
	if m.modal == nil {
		return panes
	}
	// Float the modal over the panes, faded so the modal stands out.
	box := modalStyle.Render(m.modal.view(max(min(m.width-4, 80), 10), max(h-2, 3)))
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

// renderRowLine fits line into width with tail right-aligned, and applies the
// selection style to the line (the tail keeps its colors).
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
	if selected && line != "" {
		style := selectedStyle
		if m.focusReader {
			style = titleStyle
		}
		line = style.Render(ansi.Strip(line))
	}
	if tail == "" {
		return line
	}
	return line + strings.Repeat(" ", max(width-ansi.StringWidth(line)-tailW, 1)) + tail
}

func (m *Model) listView(width, height int) string {
	switch {
	case m.loading:
		return dimStyle.Render("Loading tasks…")
	case len(m.visible) == 0:
		return dimStyle.Render("No tasks match the filter.")
	}
	multi := m.deps.Config.List.Layout == config.LayoutMulti
	itemH, sepH := 1, 0
	if multi {
		itemH = 2
	}
	if m.deps.Config.List.Separator {
		sepH = 1
	}
	// Separators frame every item and neighbours share one: n*(itemH+sepH)+sepH lines.
	rows := max((height-sepH)/(itemH+sepH), 1)
	start := max(m.cursor-rows+1, 0)
	end := min(start+rows, len(m.visible))
	sep := dimStyle.Render(strings.Repeat("─", width))
	var lines []string
	for i := start; i < end; i++ {
		if sepH > 0 {
			lines = append(lines, sep)
		}
		t := m.visible[i]
		mark := m.sym.open
		if t.Completed {
			mark = m.sym.done
		}
		title := mark + " " + ticket.OneLine(t.Name)
		linked := m.viewAgents(t)
		badge := m.sym.badge(linked, m.frame)
		if len(linked) > 0 && linked[0].State == agents.Waiting && i != m.cursor {
			title = stateStyles[agents.Waiting].Render(title)
		}
		details := strings.Join(rowFields(t, m.deps.Config.List.Fields, m.rowContext()), " · ")
		row := []string{title}
		switch {
		case multi && details != "":
			row = append(row, dimStyle.Render("  "+details))
		case multi:
			row = append(row, "")
		case details != "":
			row[0] += "  " + dimStyle.Render(details)
		}
		for j, line := range row {
			tail := ""
			if j == 0 && badge != "" {
				tail = badge
			}
			lines = append(lines, m.renderRowLine(line, tail, width, i == m.cursor))
		}
	}
	if sepH > 0 {
		lines = append(lines, sep)
	}
	return strings.Join(lines, "\n")
}
