// Package tui is the asanamate terminal interface.
package tui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"math/rand/v2"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
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

// maxColW caps a list field column's width; longer values are cut.
const maxColW = 24

// minPaneW is the narrowest a fitted list or its reader gets.
const minPaneW = 30

// minTitleW is the title room a list row keeps before its field columns hide.
const minTitleW = 30

// The reader aims for readerIdealW columns in the default split. A fitted
// list grows into the reader down to readerMinW, then truncates its titles.
const (
	readerIdealW = 120
	readerMinW   = 80
)

// panelFrame is the cells a panel's border takes across and down.
const panelFrame = 2

// listGutter is the blank columns kept right of the list's rows; group
// headers and separators still run the full width.
const listGutter = 1

// Deps are the services the TUI uses.
type Deps struct {
	Config     config.Config
	ConfigPath string // configuration path; empty uses config.Path
	State      *state.State
	Client     *asana.Client
	StateDir   string
	Images     bool
	InTmux     bool
	// Symbols is the resolved symbol set: unicode, nerd, or ascii.
	Symbols string
	// ReducedMotion shows static symbols instead of the working spinner.
	ReducedMotion bool
	// NoPreview hides the reading pane and gives the list the full width.
	NoPreview bool
	// ExitOnAction runs background actions as exit actions, so asanamate
	// quits first. It suits popups, which close when asanamate exits.
	ExitOnAction bool
}

// Model is the Bubble Tea model for asanamate.
type Model struct {
	deps          Deps
	width, height int

	viewProject     *asana.Ref
	tasks           []asana.Task
	pinExtras       []asana.Task // pinned tickets absent from the normal list
	visible         []asana.Task
	groups          []string       // group label per visible task; nil when ungrouped
	pinnedCount     int            // leading visible rows in the Pinned section
	viewingRow      bool           // visible[pinnedCount] is the temporary ticket, under Viewing
	sortBy          config.Sort    // ordering within groups; empty By keeps Asana order
	groupBy         string         // list field the list is grouped by; "" for none
	listW           int            // fitted list pane width; 0 for the default split
	accentStyle     lipgloss.Style // reader headings
	headerStyle     lipgloss.Style // group headers
	pinnedStyle     lipgloss.Style // Pinned header
	markerStyle     lipgloss.Style // selected ticket marker
	cursor          int
	cols            []int            // width of each list field column; 0 when empty
	badgeW          int              // width of the widest agent badge
	titleW          int              // width of the widest ticket title
	now             func() time.Time // today, for due dates
	loading         bool             // tasks are loading; the stale view stays frozen under a modal
	background      bool             // the timer started the loading reload, so it shows no modal
	refreshInterval time.Duration    // auto-update interval, from saved settings or config
	refreshSeq      uint64           // invalidates timers from earlier session intervals

	filterInput textinput.Model
	filtering   bool

	focusReader bool
	focusNav    bool // the views panel has focus; wide layout only
	navCursor   int  // selected row among the views panel's selectable rows
	help        bool // the key help is open
	reader      viewport.Model
	readerView  string // config.ViewCards or config.ViewMarkdown
	separator   bool   // lines frame each ticket; starts at list.separator
	spacing     bool   // blank lines around group headers; starts at list.header.spacing
	savedView   string // saved view [ and ] last applied
	renderers   map[rendererKey]*markdownRenderer
	details     map[string]ticket.Ticket
	images      map[string]*inlineImage // inline images by attachment gid
	imageSeq    int                     // inline image id sequence, from a random start
	imageCell   kitty.CellSize          // cell size reported by the outer terminal
	viewImages  []asana.Attachment      // images the attachment viewer steps through
	viewIndex   int                     // viewImages index of the image in the viewer
	shownGID    string
	viewing     *asana.Task    // edited ticket kept listed after a reload drops it, until the selection moves
	shownAgents string         // agents section rendered for shownGID
	fieldKey    string         // selected cards view target; "" for none
	showEmpty   bool           // the cards view lists empty custom fields; resets per ticket
	fieldLines  map[string]int // reader line of each cards view target, by key
	fieldEnds   map[string]int // last reader line of multi-line targets, by key
	cardTargets []string       // cards view targets in reading order

	projects        []asana.Project
	projectFields   map[string]map[string]bool // project gid -> its custom field gids
	agents          []agents.Agent
	linked          map[string][]agents.Agent // viewAgents by ticket gid; nil when stale
	agentsErr       string
	sym             symbolSet
	frame           int  // spinner frame
	spinning        bool // a spinner tick is scheduled
	modal           *picker
	input           *inputBox // free-text modal for input actions
	builder         *actionBuilder
	form            *formModal // shared action and time-entry controls
	notices         []string   // messages that block input until dismissed
	run             *pendingRun
	edit            *pendingEdit
	timeEntry       *pendingTime
	lastAction      int               // index of the last picked action; -1 for none
	users           []asana.Ref       // workspace users, loaded on first assign
	afterProjects   func() tea.Cmd    // runs once projects load
	linkNames       map[string]string // names of linked projects outside projects
	loadingProjects bool              // projects are loading
	menuFor         string            // gid whose menu opens once its details arrive
	menuOpen        func() tea.Cmd    // opens that menu
	openGID         string            // history ticket to show once its details arrive
	status          string
	exitCmd         *exec.Cmd
}

// New returns a model that starts on My Tasks with its last used grouping, sort, and filter.
func New(d Deps) *Model {
	in := textinput.New()
	in.Prompt = "/"
	m := &Model{
		deps:          d,
		filterInput:   in,
		reader:        viewport.New(),
		readerView:    cmp.Or(d.State.Display.ReaderView, d.Config.Reader.View),
		separator:     *cmp.Or(d.State.Display.Separator, &d.Config.List.Separator),
		spacing:       *cmp.Or(d.State.Display.HeaderSpacing, &d.Config.List.Header.Spacing),
		lastAction:    -1,
		accentStyle:   colorStyle(d.Config.AccentColor),
		headerStyle:   colorStyle(cmp.Or(d.Config.List.Header.Color, d.Config.AccentColor)),
		pinnedStyle:   colorStyle(cmp.Or(d.Config.List.Pinned.Color, d.Config.AccentColor)),
		markerStyle:   colorStyle(cmp.Or(d.Config.List.Selection.Color, d.Config.AccentColor)),
		renderers:     map[rendererKey]*markdownRenderer{},
		details:       map[string]ticket.Ticket{},
		images:        map[string]*inlineImage{},
		imageSeq:      rand.IntN(imageIDs),
		projectFields: map[string]map[string]bool{},
		sym:           newSymbols(d.Symbols, d.Config.Agents.Symbols, d.ReducedMotion),
		loading:       true,
		now:           time.Now,
	}
	m.restoreView()
	m.refreshInterval, _ = config.ParseRefreshInterval(cmp.Or(d.Config.List.RefreshInterval, config.Default().List.RefreshInterval))
	if saved, err := config.ParseRefreshInterval(d.State.Display.RefreshInterval); err == nil {
		m.refreshInterval = saved
	}
	return m
}

// colorStyle is bold text in a configured color.
func colorStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
}

// ExitCommand is the command an exit-mode action left to run after the TUI quits.
func (m *Model) ExitCommand() *exec.Cmd { return m.exitCmd }

func (m *Model) Init() tea.Cmd {
	cmd := tea.Batch(loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject, m.pinnedTasks()), m.startSpinner(), m.scheduleRefresh())
	if m.deps.Config.AgentsEnabled() {
		cmd = tea.Batch(cmd, loadAgents(m.deps.Config.Agents, m.deps.StateDir))
	}
	return cmd
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshTickMsg:
		return m, m.autoRefresh(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		// The views panel lists recent projects by name.
		if m.showNav() && m.projects == nil && !m.loadingProjects && m.deps.Client != nil {
			m.loadingProjects = true
			return m, tea.Batch(loadProjects(m.deps.Client, m.deps.Config.Workspace), m.requestImageCellSize())
		}
		return m, m.requestImageCellSize()
	case uv.CellSizeEvent:
		return m, m.imageCellSizeChanged(kitty.CellSize{Width: msg.Width, Height: msg.Height})
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
		m.tasks, m.pinExtras, m.linked = msg.tasks, msg.pins, nil
		m.refreshViewing(msg.tasks...)
		m.refreshViewing(msg.pins...)
		m.applyFilter()
		// A reload that drops the selected ticket starts from the top.
		if t, _ := m.selected(); t.GID != prev.GID {
			m.cursor = 0
		}
		m.fitList()
		if msg.warning != "" {
			m.status = msg.warning
		}
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
			if isSelected || m.openGID == msg.gid {
				m.status = "loading ticket: " + msg.err.Error()
			}
			if m.openGID == msg.gid {
				m.openGID = ""
			}
			if m.menuFor == msg.gid {
				m.menuFor = ""
			}
			return m, nil
		}
		m.details[msg.gid], m.linked = msg.ticket, nil
		if m.refreshRetained(msg.ticket.Task) {
			m.applyFilter()
		}
		images := m.loadInlineImages(msg.ticket)
		if m.openGID == msg.gid {
			m.openGID, m.status = "", ""
			return m, tea.Batch(images, m.showTicket(msg.ticket.Task))
		}
		if isSelected {
			m.renderDetail(false)
			if m.menuFor == msg.gid {
				m.menuFor = ""
				return m, tea.Batch(images, m.menuOpen())
			}
		}
		return m, images
	case projectsMsg:
		m.loadingProjects = false
		if msg.err != nil {
			m.status = "loading projects: " + msg.err.Error()
			return m, nil
		}
		m.status = ""
		m.projects = msg.projects
		if open := m.afterProjects; open != nil {
			m.afterProjects = nil
			return m, open()
		}
	case linkNamesMsg:
		m.status = ""
		if m.linkNames == nil {
			m.linkNames = map[string]string{}
		}
		maps.Copy(m.linkNames, msg.names)
		return m, m.openLinks()
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
			badgeW := m.badgeW
			m.applyFilter()
			// Badges widen the rows; refit so titles keep their room.
			if m.badgeW != badgeW {
				m.fitList()
			}
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
		return m, loadAgents(m.deps.Config.Agents, m.deps.StateDir)
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
		status := actionStatus(msg)
		if msg.err != nil {
			m.showNotice(status)
		} else {
			m.status = status
		}
	case timeFormMsg:
		m.gotTimeForm(msg)
	case timeDoneMsg:
		m.finishTime(msg)
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
		// Keep the edited ticket shown even if the reload drops it from the view.
		if t, ok := m.selected(); ok && t.GID == msg.gid {
			m.viewing = &t
		}
		delete(m.details, msg.gid)
		if m.shownGID == msg.gid {
			m.shownGID = ""
		}
		return m, m.reload()
	case inlineImageMsg:
		return m, m.inlineImageLoaded(msg)
	case imageMsg:
		if msg.err != nil {
			m.status = "image: " + msg.err.Error() + "; opening in browser"
			return m, openURL(msg.url)
		}
		m.status = ""
		viewer := kitty.NewViewer(msg.payload, m.viewerFooter(), len(m.viewImages) > 1, m.deps.InTmux)
		return m, tea.Exec(viewer, func(err error) tea.Msg { return viewerDoneMsg{step: viewer.Step, err: err} })
	case viewerDoneMsg:
		if msg.err != nil || msg.step == 0 {
			m.status = actionStatus(actionDoneMsg{name: "image viewer", err: msg.err})
			return m, nil
		}
		return m, m.viewImage((m.viewIndex + msg.step + len(m.viewImages)) % len(m.viewImages))
	case statusMsg:
		m.status = string(msg)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	// Editors also receive paste events and asynchronous clipboard results.
	if m.builder != nil {
		return m, m.updateActionBuilder(msg)
	}
	if m.input != nil {
		return m, m.updateInput(msg)
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
	if len(m.notices) > 0 {
		if k == "enter" || k == "esc" {
			m.notices = m.notices[1:]
		}
		return nil
	}
	if m.builder != nil {
		return m.updateActionBuilder(msg)
	}
	if m.input != nil {
		return m.updateInput(msg)
	}
	if m.form != nil {
		cancelled, cmd := m.form.update(msg)
		if cancelled {
			m.form, m.run, m.timeEntry = nil, nil, nil
		}
		return cmd
	}
	if m.modal != nil {
		return m.updateModal(msg)
	}
	if m.help {
		m.help = false
		return nil
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
		if m.fieldKey == emptyFieldsKey {
			m.showEmptyFields()
			return nil
		}
		if m.editableTarget() {
			return m.openField(m.fieldKey)
		}
		return nil
	}
	if m.focusNav && slices.Contains(navKeys, k) {
		return m.updateNav(k)
	}
	if b, ok := m.bindingFor(k); ok {
		return b.run(m, msg)
	}
	if m.focusReader {
		return m.scrollReader(msg)
	}
	return nil
}

func (m *Model) showNotice(message string) {
	m.notices = append(m.notices, message)
	m.status = message
}

func (m *Model) updateModal(msg tea.KeyPressMsg) tea.Cmd {
	p := m.modal
	res, cmd := p.update(msg)
	switch {
	case res.cancelled:
		m.modal, m.run, m.edit, m.timeEntry = nil, nil, nil, nil
	case res.done:
		return tea.Batch(cmd, p.onPick(res))
	}
	return cmd
}

func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	res, cmd := m.input.update(msg)
	switch {
	case res.cancelled:
		m.input, m.run, m.edit = nil, nil, nil
	case res.done:
		if m.input.onSubmit != nil {
			return tea.Batch(cmd, m.input.onSubmit(res.free))
		}
		if m.run == nil {
			return tea.Batch(cmd, m.typedEdit(res.free))
		}
		return tea.Batch(cmd, m.typedInput(res.free))
	}
	return cmd
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "?":
		m.help = true
		return nil
	case "enter", "esc":
		m.filtering = false
		m.filterInput.Blur()
		m.reader.SetHeight(m.paneHeight())
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
	m.loading, m.background = true, false
	return tea.Batch(loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject, m.pinnedTasks()), m.startSpinner())
}

// applyFilter recomputes the visible tasks and their groups, keeping the
// selected ticket selected when it is still visible.
func (m *Model) applyFilter() {
	var prevGID string
	if prev, ok := m.selected(); ok {
		prevGID = prev.GID
	}
	pins := m.pinnedTasks()
	var pinned []asana.Task
	for _, gid := range pins {
		for _, tasks := range [][]asana.Task{m.tasks, m.pinExtras} {
			if i := slices.IndexFunc(tasks, func(t asana.Task) bool { return t.GID == gid }); i >= 0 {
				pinned = append(pinned, tasks[i])
				break
			}
		}
	}
	visible := filter.Parse(m.filterInput.Value()).Apply(m.tasks, m.agentStates, m.now())
	visible = slices.DeleteFunc(visible, func(t asana.Task) bool { return slices.Contains(pins, t.GID) })
	m.visible, m.groups = groupTasks(visible, m.groupBy, m.rowContext(), m.now())
	sortTasks(m.visible, m.groups, m.sortBy, m.rowContext())
	m.pinnedCount, m.viewingRow = len(pinned), false
	var viewing []asana.Task
	if t := m.viewing; t != nil && !slices.Contains(pins, t.GID) && !slices.ContainsFunc(visible, func(v asana.Task) bool { return v.GID == t.GID }) {
		viewing = []asana.Task{*t}
		m.viewingRow = true
	}
	if len(pinned)+len(viewing) > 0 {
		if m.groups == nil {
			m.groups = slices.Repeat([]string{ungroupedLabel}, len(m.visible))
		}
		labels := slices.Repeat([]string{pinnedLabel}, len(pinned))
		if m.viewingRow {
			labels = append(labels, viewingLabel)
		}
		m.visible = append(append(pinned, viewing...), m.visible...)
		m.groups = append(labels, m.groups...)
	}
	m.measureColumns()
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

// refreshViewing replaces the viewed ticket with its copy among tasks, if any,
// and reports whether it did.
func (m *Model) refreshViewing(tasks ...asana.Task) bool {
	if m.viewing == nil {
		return false
	}
	for _, t := range tasks {
		if t.GID == m.viewing.GID {
			m.viewing = &t
			return true
		}
	}
	return false
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
	if m.viewing != nil && (!ok || t.GID != m.viewing.GID) {
		// Moving off the viewed ticket drops it unless the view still lists it.
		m.viewing = nil
		m.applyFilter()
		t, ok = m.selected()
	}
	if !ok {
		m.shownGID = ""
		m.reader.SetContent("")
		return nil
	}
	if t.GID == m.shownGID {
		return nil
	}
	m.fieldKey, m.showEmpty = "", false
	if _, cached := m.details[t.GID]; cached {
		m.renderDetail(false)
		return m.loadInlineImages(m.details[t.GID])
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
		extra := section
		if path, own := m.effectiveRepo(t.Task); path != "" {
			repo := "`" + path + "`"
			if own {
				repo += " (this ticket only)"
			}
			extra = "\n## Local repo\n\n" + repo + "\n" + extra
		}
		m.reader.SetContent(m.glamour(t.MarkdownWith(extra), width, false))
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
	keys := m.fieldTargets(t)
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
	m.showTarget()
	return true
}

// stepCardTarget moves through fields, section headings, and comments.
func (m *Model) stepCardTarget(dir int) bool {
	if _, ok := m.selectedDetail(); !ok || m.readerView == config.ViewMarkdown || len(m.cardTargets) == 0 {
		return false
	}
	i := slices.Index(m.cardTargets, m.fieldKey)
	switch {
	case i < 0 && dir < 0:
		i = len(m.cardTargets) - 1
	case i < 0:
		i = 0
	default:
		i = (i + dir + len(m.cardTargets)) % len(m.cardTargets)
	}
	m.fieldKey = m.cardTargets[i]
	m.showTarget()
	return true
}

// jumpReader scrolls the reader to its top or bottom, selecting the cards
// view's first or last target.
func (m *Model) jumpReader(top bool) {
	if _, ok := m.selectedDetail(); ok && m.readerView != config.ViewMarkdown && len(m.cardTargets) > 0 {
		m.fieldKey = m.cardTargets[len(m.cardTargets)-1]
		if top {
			m.fieldKey = m.cardTargets[0]
		}
		m.renderDetail(true)
	}
	if top {
		m.reader.GotoTop()
	} else {
		m.reader.GotoBottom()
	}
}

func (m *Model) editableTarget() bool {
	t, ok := m.selectedDetail()
	return ok && m.readerView != config.ViewMarkdown && slices.Contains(m.fieldTargets(t), m.fieldKey)
}

// showTarget redraws the highlight and brings the whole target into view, or
// its first line when it is taller than the reader. A target that fits on
// screen with the ticket's top scrolls to the top, so the head stays reachable.
func (m *Model) showTarget() {
	m.renderDetail(true)
	line, top, h := m.fieldLines[m.fieldKey], m.reader.YOffset(), m.reader.Height()
	last := max(m.fieldEnds[m.fieldKey], line)
	switch {
	case line < top && last < h:
		m.reader.GotoTop()
	case line < top:
		m.reader.SetYOffset(line)
	case last >= top+h:
		m.reader.SetYOffset(min(line, last-h+1))
	}
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
		r = newMarkdownRenderer(m.deps.Config.Theme, width, bare, m.sym.codeWrap)
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
	m.modal = newPicker(pickValue(m.pickedProject), "Switch project", items)
}

func (m *Model) pickedProject(ref *asana.Ref) tea.Cmd {
	m.modal = nil
	m.viewProject, m.linked, m.viewing = ref, nil, nil
	m.restoreView()
	if ref != nil {
		m.deps.State.TouchProject(ref.GID)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving state: " + err.Error()
		}
	}
	return m.reload()
}

// restoreView applies the viewed project's last used grouping, sort, and filter, else
// the configured group_by, sort, and default_filter.
func (m *Model) restoreView() {
	v, ok := m.deps.State.View(gidOf(m.viewProject))
	if !ok {
		v = state.View{GroupBy: strings.TrimSpace(m.deps.Config.List.GroupBy), Sort: m.deps.Config.List.Sort, Filter: m.deps.Config.DefaultFilter}
	}
	m.groupBy, m.sortBy = v.GroupBy, v.Sort
	m.filterInput.SetValue(v.Filter)
}

// saveView remembers the viewed project's grouping, sort, and filter.
func (m *Model) saveView() {
	m.deps.State.SetView(gidOf(m.viewProject), state.View{GroupBy: m.groupBy, Sort: m.sortBy, Filter: m.filterInput.Value()})
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving state: " + err.Error()
	}
}

// paneWidths returns the content widths of the list and the reader. Split
// panes sit in panels, each panelFrame columns wider than its content.
func (m *Model) paneWidths() (listW, readerW int, split bool) {
	if m.deps.NoPreview || m.width < narrowWidth {
		return m.width, m.width, false
	}
	room := m.width - m.navWidth()
	outer := min(room*2/5, room-readerIdealW-panelFrame)
	if m.listW > 0 {
		outer = min(m.listW, room-readerMinW-panelFrame)
	}
	outer = min(max(outer, minPaneW), room-minPaneW)
	return outer - panelFrame, room - outer - panelFrame, true
}

func (m *Model) bodyHeight() int { return max(m.height-1, 1) }

// paneHeight is the height of the panes' content.
func (m *Model) paneHeight() int {
	if _, _, split := m.paneWidths(); split {
		return max(m.bodyHeight()-m.filterHintHeight()-panelFrame, 1)
	}
	return max(m.bodyHeight()-m.filterHintHeight(), 1)
}

func (m *Model) layout() {
	if !m.showNav() {
		m.focusNav = false
	}
	_, readerW, _ := m.paneWidths()
	widthChanged := readerW != m.reader.Width()
	if widthChanged {
		m.renderers = map[rendererKey]*markdownRenderer{}
	}
	m.reader.SetWidth(readerW)
	m.reader.SetHeight(m.paneHeight())
	m.filterInput.SetWidth(max(m.width/3, 10))
	if t, ok := m.selected(); widthChanged && ok && t.GID == m.shownGID {
		m.renderDetail(true)
	}
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.body() + "\n" + m.statusline())
	v.AltScreen = true
	return v
}

func (m *Model) body() string {
	h := m.bodyHeight()
	hints := m.filterHints()
	panes := m.panes(max(h-len(hints), 1))
	if len(hints) > 0 {
		panes += "\n" + strings.Join(hints, "\n")
	}
	w, mh := max(min(m.width-4, 80), 10), max(h-2, 3)
	var content string
	style := modalStyle.BorderForeground(m.accentStyle.GetForeground())
	switch {
	case len(m.notices) > 0:
		content = m.accentStyle.Render("Notice") + "\n\n" + ansi.Wrap(ticket.OneLine(m.notices[0]), w, "") +
			"\n\n" + m.accentStyle.Render("enter") + dimStyle.Render(" / ") + m.accentStyle.Render("esc") + dimStyle.Render(" dismiss")
		style = modalStyle.BorderForeground(warnStyle.GetForeground())
	case m.builder != nil:
		content = m.builder.view(w, mh, m.accentStyle)
	case m.input != nil:
		if m.input.area.DynamicHeight {
			w = max(min(m.width-4, 100), 10)
		}
		content = m.input.view(w, mh, m.accentStyle)
	case m.form != nil:
		content = m.form.view(w, mh, m.accentStyle)
	case m.modal != nil:
		content = m.modal.view(w, mh, m.accentStyle)
	case m.help:
		if m.filtering {
			content = m.filterHelpView()
		} else {
			content = m.helpView()
		}
		style = style.Padding(0, 2)
	case m.loading && m.tasks != nil && !m.background:
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
	listW, readerW, split := m.paneWidths()
	switch {
	case split:
		inner := m.paneHeight()
		title, count := m.listTitle()
		list := m.panel(title, count, m.listView(listW, inner), listW+panelFrame, h, m.listHighlight())
		reader := m.panel("[2] Ticket", m.readerView, m.reader.View(), readerW+panelFrame, h, m.focusColor(m.focusReader))
		if !m.showNav() {
			return lipgloss.JoinHorizontal(lipgloss.Top, list, reader)
		}
		nav := m.panel("[0] Views", "", m.navView(navW-panelFrame, inner), navW, h, m.focusColor(m.focusNav))
		return lipgloss.JoinHorizontal(lipgloss.Top, nav, list, reader)
	case m.focusReader:
		return m.reader.View()
	default:
		return lipgloss.NewStyle().Width(listW).Height(h).Render(m.listView(listW, h))
	}
}

// focusColor is the accent color for a focused panel, or nil.
func (m *Model) focusColor(focused bool) color.Color {
	if focused {
		return m.accentStyle.GetForeground()
	}
	return nil
}

// listHighlight marks the list panel in the warning color while the timer
// refreshes it, since that refresh shows no modal.
func (m *Model) listHighlight() color.Color {
	if m.loading && m.background {
		return warnStyle.GetForeground()
	}
	return m.focusColor(!m.focusReader && !m.focusNav)
}

// listTitle is the list panel's title and its right-hand count, or a
// loading note while tasks load.
func (m *Model) listTitle() (title, count string) {
	count = fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks))
	if m.loading {
		count = "loading"
		if m.sym.spinner != nil {
			count = m.sym.spinner[m.frame%len(m.sym.spinner)] + " " + count
		}
	}
	return "[1] Tickets · " + m.viewName(), count
}

// skeletonWidths are the placeholder rows' title widths, in percent.
var skeletonWidths = []int{70, 55, 82, 64, 48, 76, 60, 88, 52, 68, 58, 74}

// skeleton fills the list with placeholder rows while the first tasks load;
// a lighter band runs down them with the spinner.
func (m *Model) skeleton(width, height int) string {
	bar, lit := lipgloss.NewStyle().Foreground(borderColor), dimStyle
	colW := min(12, width/4)
	lines := make([]string, min(height, len(skeletonWidths)))
	for i := range lines {
		style := bar
		if m.sym.spinner != nil && i == m.frame%len(lines) {
			style = lit
		}
		titleW := max((width-colW-6)*skeletonWidths[i]/100, 1)
		row := "▆ " + strings.Repeat("▆", titleW)
		lines[i] = style.Render(row + strings.Repeat(" ", max(width-ansi.StringWidth(row)-colW, 1)) + strings.Repeat("▆", max(colW-2, 0)))
	}
	return strings.Join(lines, "\n")
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
		return m.skeleton(width, height)
	case len(m.visible) == 0:
		return m.emptyView(width, height)
	}
	itemH, sepH := 1, 0
	if m.deps.Config.List.Layout == config.LayoutMulti {
		itemH = 2
	}
	if m.separator {
		sepH = 1
	}
	// Separators frame every item and neighbours share one; a group header
	// takes the place of the separator above its item. The first row shown
	// always has its group header. With spacing, headers get a blank line
	// below, and above too unless they top the view.
	spacing := 0
	if m.spacing {
		spacing = 1
	}
	header := func(i, start int) bool {
		return m.groups != nil && (i == start || m.groups[i] != m.groups[i-1] || i == m.pinnedCount || m.viewingRow && i == m.pinnedCount+1)
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
	rowW := max(width-ansi.StringWidth(gutter)-listGutter, 1)
	cols := m.rowCols(rowW)
	var lines []string
	for i := start; i < end; i++ {
		switch {
		case header(i, start):
			for range above(i, start) {
				lines = append(lines, "")
			}
			if label, n := m.retainedSection(i); label != "" {
				style := m.headerStyle
				if i < m.pinnedCount {
					style = m.pinnedStyle
				}
				lines = append(lines, m.groupHeader(style, label, n, width))
			} else {
				lines = append(lines, m.groupHeader(m.headerStyle, m.groups[i], counts[m.groups[i]], width))
			}
			for range spacing {
				lines = append(lines, "")
			}
		case sepH > 0:
			lines = append(lines, sep)
		}
		row, rowTail := m.listRow(i, cols)
		for j, line := range row {
			tail := ""
			if j == 0 {
				tail = rowTail
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

// emptyView centers a note on why the list is empty, with the keys that
// change what it shows.
func (m *Model) emptyView(width, height int) string {
	title, why := "Nothing here.", "No tickets in "+m.viewName()
	if len(m.tasks) > 0 {
		title, why = "All clear.", "Nothing matches "+warnStyle.Render(m.filterInput.Value())
	}
	hint := func(k, desc string) string { return m.accentStyle.Render(k) + " " + dimStyle.Render(desc) }
	block := lipgloss.JoinVertical(lipgloss.Center,
		okStyle.Bold(true).Render(m.sym.done), "", titleStyle.Render(title), dimStyle.Render(why), "",
		hint("/", "filter")+"   "+hint("p", "projects")+"   "+hint("r", "reload"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// groupCounts returns the number of visible tickets per group label, leaving
// out retained rows so a group sharing their label keeps its own count.
func (m *Model) groupCounts() map[string]int {
	counts := map[string]int{}
	for i, g := range m.groups {
		if label, _ := m.retainedSection(i); label == "" {
			counts[g]++
		}
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

// listRow returns the lines of visible ticket i's row and the tail
// right-aligned on its first line: the single layout's field columns sized by
// cols and agent badge, or the multi layout's badge.
func (m *Model) listRow(i int, cols []int) (row []string, tail string) {
	t := m.visible[i]
	mark := m.sym.open
	if t.Completed {
		mark = m.sym.done
	}
	name := ticket.OneLine(t.Name)
	linked := m.viewAgents(t)
	bar := i == m.cursor && m.selectionBar()
	badge := m.sym.badge(linked, m.frame, bar)
	switch {
	case len(linked) > 0 && linked[0].State == agents.Waiting && i != m.cursor:
		name = stateStyles[agents.Waiting].Render(name)
	case t.Completed:
		name = dimStyle.Strikethrough(true).Render(name)
	}
	title := dimStyle.Render(mark) + "  " + name
	rc, today := m.rowContext(), m.now()
	if m.deps.Config.List.Layout == config.LayoutMulti {
		var parts []string
		for _, name := range m.deps.Config.List.Fields {
			v, style := cellValue(t, name, rc, today)
			if v == "" {
				continue
			}
			if n := strings.TrimSpace(name); !isDue(n) && !strings.EqualFold(n, initialsField) {
				style = dimStyle
			}
			parts = append(parts, style.Render(v))
		}
		details := ""
		if len(parts) > 0 {
			details = "   " + strings.Join(parts, dimStyle.Render(" · "))
		}
		return []string{title, details}, badge
	}
	// A highlighted bar runs through the columns, which then lose their colors.
	pad := func(s string, w int) string {
		s += strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
		if bar && s != "" {
			return selectedStyle.Render(ansi.Strip(s))
		}
		return s
	}
	var cells []string
	for j, name := range m.deps.Config.List.Fields {
		if cols[j] == 0 {
			continue
		}
		v, style := cellValue(t, name, rc, today)
		v = ansi.Truncate(v, cols[j], "…")
		cells = append(cells, pad(style.Render(v), cols[j]))
	}
	if m.badgeW > 0 {
		cells = append(cells, badge+pad("", m.badgeW-ansi.StringWidth(badge)))
	}
	return []string{title}, strings.Join(cells, pad("", 2))
}

// rowCols returns the field column widths for rows width wide, hiding columns
// until titles get minTitleW, or the widest title if shorter: other fields
// from the right, then due. The agent badge always stays.
func (m *Model) rowCols(width int) []int {
	cols := slices.Clone(m.cols)
	names := m.deps.Config.List.Fields
	// The title line also holds the check mark and the two spaces after it.
	room := min(m.titleW, minTitleW) + ansi.StringWidth(m.sym.open) + 2
	var order, due []int
	for j := len(cols) - 1; j >= 0; j-- {
		if isDue(strings.TrimSpace(names[j])) {
			due = append(due, j)
		} else {
			order = append(order, j)
		}
	}
	for _, j := range append(order, due...) {
		if width-tailWidth(cols, m.badgeW)-1 >= room {
			break
		}
		cols[j] = 0
	}
	return cols
}

// tailWidth is the width of a row tail with these field columns and badge.
func tailWidth(cols []int, badgeW int) int {
	w, n := badgeW, min(badgeW, 1)
	for _, c := range cols {
		if c > 0 {
			w, n = w+c, n+1
		}
	}
	return w + 2*max(n-1, 0)
}

// measureColumns sizes each list field column to its widest visible value,
// up to maxColW, the badge column to the widest agent badge, and titleW to
// the widest title.
func (m *Model) measureColumns() {
	names := m.deps.Config.List.Fields
	m.cols, m.badgeW, m.titleW = make([]int, len(names)), 0, 0
	rc, today := m.rowContext(), m.now()
	for _, t := range m.visible {
		m.titleW = max(m.titleW, ansi.StringWidth(ticket.OneLine(t.Name)))
		for j, name := range names {
			v, _ := cellValue(t, name, rc, today)
			m.cols[j] = min(max(m.cols[j], ansi.StringWidth(v)), maxColW)
		}
		m.badgeW = max(m.badgeW, ansi.StringWidth(m.sym.badge(m.viewAgents(t), 0, false)))
	}
}

// fitList sizes the list pane to its widest row or group header. paneWidths
// caps it to leave the reader readerMinW columns.
func (m *Model) fitList() {
	gutter := ansi.StringWidth(m.marker())
	w := 0
	if m.viewingRow {
		w = groupHeaderWidth(viewingLabel, 1)
	}
	if m.pinnedCount > 0 {
		w = max(w, groupHeaderWidth(pinnedLabel, m.pinnedCount))
	}
	for label, n := range m.groupCounts() {
		w = max(w, groupHeaderWidth(label, n))
	}
	for i := range m.visible {
		row, tail := m.listRow(i, m.cols)
		for j, line := range row {
			lineW := gutter + ansi.StringWidth(line)
			if j == 0 && tail != "" {
				lineW += 2 + ansi.StringWidth(tail)
			}
			w = max(w, lineW)
		}
	}
	// The title needs its corners, the spaces around it and the count, and
	// one rule cell between them.
	title, count := m.listTitle()
	m.listW = max(w+listGutter+panelFrame, ansi.StringWidth(title)+ansi.StringWidth(count)+8)
	m.layout()
}
