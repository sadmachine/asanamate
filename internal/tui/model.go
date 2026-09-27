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
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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

	focusReader   bool
	reader        viewport.Model
	renderer      *glamour.TermRenderer
	rendererWidth int
	details       map[string]ticket.Ticket
	shownGID      string

	projects []asana.Project
	modal    *picker
	run      *pendingRun
	menuFor  string // gid whose action menu opens once its details arrive
	status   string
	exitCmd  *exec.Cmd
}

// New returns a model that starts on My Tasks with the configured default filter.
func New(d Deps) *Model {
	in := textinput.New()
	in.Prompt = "/"
	in.SetValue(d.Config.DefaultFilter)
	return &Model{
		deps:        d,
		filterInput: in,
		reader:      viewport.New(),
		details:     map[string]ticket.Ticket{},
		loading:     true,
	}
}

// ExitCommand is the command an exit-mode action left to run after the TUI quits.
func (m *Model) ExitCommand() *exec.Cmd { return m.exitCmd }

func (m *Model) Init() tea.Cmd {
	return loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject)
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

func (m *Model) applyFilter() {
	m.visible = filter.Parse(m.filterInput.Value()).Apply(m.tasks)
	m.moveTo(m.cursor)
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

func (m *Model) showDetail() {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	if m.deps.NoPreview {
		m.shownGID = t.GID
		return
	}
	_, readerW, _ := m.paneWidths()
	m.reader.SetContent(m.renderMarkdown(t.Markdown(), max(readerW-2, 20)))
	m.reader.GotoTop()
	m.shownGID = t.GID
}

func (m *Model) renderMarkdown(md string, width int) string {
	if m.renderer == nil || m.rendererWidth != width {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(m.deps.Config.Theme), glamour.WithWordWrap(width))
		if err != nil {
			return md
		}
		m.renderer, m.rendererWidth = r, width
	}
	out, err := m.renderer.Render(md)
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
	return ansi.Truncate(titleStyle.Render("asanamate · "+name)+"  "+f+"  "+count, m.width, "…")
}

func (m *Model) footer() string {
	s := m.status
	if s == "" {
		hints := "j/k move · enter actions · tab focus · / filter · p projects · f files · o open · r reload · q quit"
		if m.deps.NoPreview {
			hints = strings.Replace(hints, " · tab focus", "", 1)
		}
		s = dimStyle.Render(hints)
	}
	return ansi.Truncate(s, m.width, "…")
}

func (m *Model) body() string {
	h := m.bodyHeight()
	if m.modal != nil {
		box := modalStyle.Render(m.modal.view(max(min(m.width-4, 80), 10), max(h-2, 3)))
		return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
	}
	listW, _, split := m.paneWidths()
	list := lipgloss.NewStyle().Width(listW).Height(h).Render(m.listView(listW, h))
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

func (m *Model) listView(width, height int) string {
	switch {
	case m.loading:
		return dimStyle.Render("Loading tasks…")
	case len(m.visible) == 0:
		return dimStyle.Render("No tasks match the filter.")
	}
	start := max(m.cursor-height+1, 0)
	var lines []string
	for i := start; i < len(m.visible) && i < start+height; i++ {
		t := m.visible[i]
		mark := "○"
		if t.Completed {
			mark = "✓"
		}
		line := mark + " " + ticket.Clean(t.Name)
		if sec := t.SectionFor(gidOf(m.viewProject)); sec != "" {
			line += "  " + dimStyle.Render(ticket.Clean(sec))
		}
		line = ansi.Truncate(line, width, "…")
		if i == m.cursor {
			style := selectedStyle
			if m.focusReader {
				style = titleStyle
			}
			line = style.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
