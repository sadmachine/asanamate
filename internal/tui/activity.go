package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/activity"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var activityModes = []string{"Both", "Mine", "Related"}

// activityOverlap rescans the end of the last scan, which Asana's search
// index may not have caught up with yet.
const activityOverlap = 15 * time.Minute

// activityTimeout bounds one scan; a scan of an empty cache covers the whole
// cache window.
const activityTimeout = 10 * time.Minute

type activityView struct {
	cache     *activity.Cache
	report    activity.Report // the running or last scan: progress and warning
	visible   []activity.Entry
	rows      []activityRow
	expanded  map[string]bool // task GIDs whose runs of events are expanded
	mode      string
	days      int
	cursor    int
	query     textinput.Model
	filtering bool
	automated bool
	details   bool
	reader    viewport.Model
	lastSeen  time.Time     // SeenAt before the view opened; later finds are marked new
	interval  time.Duration // background scan interval; 0 is off
	stopped   bool          // Asana refused workspace search; background scans stop
	loading   bool
	seq       uint64
	cancel    context.CancelFunc
	updates   chan activityMsg
	err       string
}

// activityRow is one row of the Activity list: the newest event in a run of
// consecutive events on one ticket, or, while the run is expanded, one of
// its older events.
type activityRow struct {
	entry int  // index into visible
	more  int  // older events in the run, on its newest event's row
	child bool // an older event of an expanded run
	last  bool // the run's oldest event
}

// activityMsg carries a scan snapshot. Partial snapshots stream in while the
// scan runs; the last one is final and may carry an error.
type activityMsg struct {
	seq     uint64
	report  activity.Report
	err     error
	partial bool
	replace bool      // manual reload: a complete report replaces its window
	foundAt time.Time // FoundAt for new entries; zero when seeding the cache
}

// activityTickMsg starts a background Activity scan.
type activityTickMsg struct{}

func newActivityView(d Deps) *activityView {
	query := textinput.New()
	query.Prompt = "/"
	interval, _ := config.Activity{RefreshInterval: cmp.Or(d.Config.Activity.RefreshInterval, config.Default().Activity.RefreshInterval)}.Interval()
	var path string
	if d.CacheDir != "" {
		path = filepath.Join(d.CacheDir, activity.CacheFile)
	}
	return &activityView{cache: activity.LoadCache(path, d.Config.Workspace), mode: "Both", days: 7, query: query, reader: viewport.New(), interval: interval, expanded: map[string]bool{}}
}

// startActivity runs the launch scan and starts the background timer.
func (m *Model) startActivity() tea.Cmd {
	if m.activity.interval == 0 {
		return nil
	}
	return tea.Batch(m.scanActivity(false), m.scheduleActivity())
}

func (m *Model) scheduleActivity() tea.Cmd {
	a := m.activity
	if a.interval == 0 || a.stopped {
		return nil
	}
	return tea.Tick(a.interval, func(time.Time) tea.Msg { return activityTickMsg{} })
}

// activityTick runs a background scan and schedules the next one.
func (m *Model) activityTick() tea.Cmd {
	if m.activity.stopped {
		return nil
	}
	return tea.Batch(m.scanActivity(false), m.scheduleActivity())
}

func (m *Model) openActivity() tea.Cmd {
	if m.activityOpen {
		m.closeActivity()
		m.layout()
		return nil
	}
	a := m.activity
	a.lastSeen, a.cache.SeenAt = a.cache.SeenAt, m.now()
	m.saveActivity()
	m.activityOpen, m.activityTicket = true, false
	m.focusList()
	m.filterActivity()
	m.layout()
	// Without background scans, or before one has finished, opening scans.
	if a.interval == 0 || a.cache.Stale(m.now()) {
		return m.scanActivity(false)
	}
	return m.startSpinner()
}

// closeActivity leaves Activity. A running scan continues in the background.
func (m *Model) closeActivity() {
	a := m.activity
	if m.activityOpen || m.activityTicket {
		// Events that arrived while the view was open are seen.
		a.cache.SeenAt = m.now()
		m.saveActivity()
	}
	m.activityOpen, m.activityTicket = false, false
	a.filtering, a.details = false, false
	a.query.Blur()
	m.focusList()
}

func (m *Model) saveActivity() {
	if err := m.activity.cache.Save(); err != nil {
		m.setBackgroundError("activity cache", "Activity cache: "+ticket.OneLine(err.Error()))
	} else {
		delete(m.backgroundErrors, "activity cache")
	}
}

// scanActivity scans from the end of the last complete scan, unless a scan is
// running. reload cancels a running scan and also covers the selected
// period. An empty or stale cache scans the whole window, and its entries are
// not new.
func (m *Model) scanActivity(reload bool) tea.Cmd {
	a := m.activity
	if a.loading && !reload {
		return nil
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.seq++
	seq := a.seq
	a.loading, a.err = true, ""
	until := m.now()
	since, foundAt := a.cache.Until.Add(-activityOverlap), until
	if period := until.Add(-time.Duration(a.days) * 24 * time.Hour); reload && period.Before(since) {
		since = period
	}
	if a.cache.Stale(until) {
		since, foundAt = until.Add(-activity.Window), time.Time{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), activityTimeout)
	a.cancel = cancel
	c, workspace := m.deps.Client, m.deps.Config.Workspace
	if c == nil {
		cancel()
		return func() tea.Msg { return activityMsg{seq: seq, err: fmt.Errorf("Asana client is unavailable")} }
	}
	// The buffer holds only the newest snapshot, so a slow UI skips stale
	// ones and the scan never blocks. Publishes are serialized.
	updates := make(chan activityMsg, 1)
	a.updates = updates
	publish := func(msg activityMsg) {
		select {
		case <-updates:
		default:
		}
		updates <- msg
	}
	go func() {
		defer cancel()
		report, err := activity.Fetch(ctx, c, workspace, since, until, func(r activity.Report) {
			publish(activityMsg{seq: seq, report: r, partial: true, replace: reload, foundAt: foundAt})
		})
		publish(activityMsg{seq: seq, report: report, err: err, replace: reload, foundAt: foundAt})
	}()
	return tea.Batch(waitActivity(updates), m.startSpinner())
}

func waitActivity(updates <-chan activityMsg) tea.Cmd {
	return func() tea.Msg { return <-updates }
}

func (m *Model) gotActivity(msg activityMsg) tea.Cmd {
	a := m.activity
	if a.seq != msg.seq {
		return nil
	}
	if !msg.partial {
		m.observeAsana("scanning activity", msg.err)
	}
	var next tea.Cmd
	if msg.partial {
		next = waitActivity(a.updates)
	} else {
		a.loading = false
	}
	if msg.err != nil {
		if !connectionFailureError(msg.err) {
			a.err = ticket.OneLine(msg.err.Error())
		}
		var apiErr *asana.APIError
		if errors.As(msg.err, &apiErr) && apiErr.Status == http.StatusPaymentRequired {
			a.stopped = true
		}
		return nil
	}
	a.report = msg.report
	known := make(map[string]bool, len(a.cache.Entries))
	for _, e := range a.cache.Entries {
		known[e.Story.GID] = true
	}
	if msg.replace {
		a.cache.Replace(msg.report, msg.foundAt)
	} else {
		a.cache.Merge(msg.report, msg.foundAt)
	}
	if !msg.partial {
		m.saveActivity()
	}
	m.filterActivity()
	return tea.Batch(next, m.runHooks(newActivityEvents(a.cache.Entries, known)))
}

// newActivityEvents returns a new_activity event for each related entry that
// is not in known, skipping a scan that seeded an empty cache (its entries
// have no FoundAt).
func newActivityEvents(entries []activity.Entry, known map[string]bool) []hookEvent {
	var events []hookEvent
	for _, e := range entries {
		if known[e.Story.GID] || !e.NewSince(time.Time{}) {
			continue
		}
		ev := hookEvent{name: config.HookNewActivity, task: e.Task}
		if e.Story.ResourceSubtype == "comment_added" {
			story := e.Story
			ev.comment = &story
		}
		events = append(events, ev)
	}
	return events
}

func (a *activityView) selected() (activity.Entry, bool) {
	if a.cursor >= 0 && a.cursor < len(a.rows) {
		return a.visible[a.rows[a.cursor].entry], true
	}
	return activity.Entry{}, false
}

// group splits visible into rows, one per run of consecutive events on the
// same ticket, plus the older events of expanded runs.
func (a *activityView) group() {
	a.rows = a.rows[:0]
	for i := 0; i < len(a.visible); {
		gid, j := a.visible[i].Task.GID, i+1
		for j < len(a.visible) && a.visible[j].Task.GID == gid {
			j++
		}
		a.rows = append(a.rows, activityRow{entry: i, more: j - i - 1})
		if a.expanded[gid] {
			for k := i + 1; k < j; k++ {
				a.rows = append(a.rows, activityRow{entry: k, child: true, last: k == j-1})
			}
		}
		i = j
	}
}

func (m *Model) filterActivity() {
	a := m.activity
	previous, _ := a.selected()
	a.visible = nil
	words := strings.Fields(strings.ToLower(a.query.Value()))
	since := m.activitySince()
	for _, e := range a.cache.Entries {
		if !a.shown(e, a.mode, since) {
			continue
		}
		text := strings.ToLower(e.Task.Name + " " + e.Story.Text + " " + ticket.Author(e.Story) + " " + e.Story.ResourceSubtype + " " + strings.Join(e.Reasons, " "))
		if !slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(text, word) }) {
			a.visible = append(a.visible, e)
		}
	}
	a.group()
	// The cursor stays on the previous event, or on its run's row when the
	// run is collapsed.
	i := slices.IndexFunc(a.visible, func(e activity.Entry) bool { return e.Story.GID == previous.Story.GID })
	a.cursor = 0
	for r, row := range a.rows {
		if row.entry > i {
			break
		}
		if !row.child || row.entry == i {
			a.cursor = r
		}
	}
	// Streamed snapshots must not reset the reader under the user.
	current, _ := a.selected()
	m.renderActivityDetail(i >= 0 && current.Story.GID == previous.Story.GID)
}

// activitySince is the start of the selected period.
func (m *Model) activitySince() time.Time {
	return m.now().Add(-time.Duration(m.activity.days) * 24 * time.Hour)
}

// shown reports whether e is in the period from since and passes the
// automated toggle and mode, before the search.
func (a *activityView) shown(e activity.Entry, mode string, since time.Time) bool {
	if e.At.Before(since) || e.Automatic && !a.automated {
		return false
	}
	return mode == "Both" || mode == "Mine" && e.Mine || mode == "Related" && !e.Mine
}

// count is how many cached events mode shows, before the search.
func (m *Model) activityCount(mode string) int {
	a, since := m.activity, m.activitySince()
	n := 0
	for _, e := range a.cache.Entries {
		if a.shown(e, mode, since) {
			n++
		}
	}
	return n
}

// newActivity counts related events found since the user last opened Activity.
func (a *activityView) newActivity() int {
	n := 0
	for _, e := range a.cache.Entries {
		if e.NewSince(a.cache.SeenAt) {
			n++
		}
	}
	return n
}

func (m *Model) layoutActivity() {
	a := m.activity
	_, width, split := m.paneWidths()
	if !split {
		width = m.width - m.navWidth()
	}
	a.reader.SetWidth(max(width, 1))
	a.reader.SetHeight(max(m.bodyHeight()-3, 1))
	a.query.SetWidth(max(m.width-4, 1))
	m.renderActivityDetail(true)
}

func (m *Model) renderActivityDetail(keepScroll bool) {
	a := m.activity
	e, ok := a.selected()
	if !ok {
		a.reader.SetContent(dimStyle.Render("Select an activity entry to see its details."))
		return
	}
	width := max(a.reader.Width()-2, 1)
	actor := ticket.Author(e.Story)
	if e.Mine {
		actor = "You"
	}
	if e.Automatic {
		actor = "Asana / automated"
	}
	reason := strings.Join(e.Reasons, "; ")
	if e.Mine {
		reason = "Your action"
	}
	lines := []string{
		m.accentStyle.Render(ansi.Wrap(ticket.OneLine(e.Task.Name), width, "")), "",
		ansi.Wrap(actor+" · "+e.At.Local().Format("Mon Jan 2, 15:04 MST"), width, ""),
		dimStyle.Render(ansi.Wrap("Why here: "+reason, width, "")), "",
		strings.Repeat("─", width), "",
	}
	if e.Story.HTMLText != "" {
		lines = append(lines, m.glamour(ticket.HTMLToMarkdown(mentionHTML(e.Story.HTMLText)), width, true))
	} else {
		lines = append(lines, ansi.Wrap(ticket.Clean(e.Story.Text), width, ""))
	}
	if e.Story.NewDates != nil {
		lines = append(lines, "", "Previous due: "+activityDue(e.Story.OldDates), "New due: "+activityDue(e.Story.NewDates))
	}
	if e.Story.Assignee != nil {
		lines = append(lines, "", "Assigned to: "+ticket.OneLine(e.Story.Assignee.Name))
	}
	lines = append(lines, "", dimStyle.Render(ansi.Wrap("Event: "+ticket.OneLine(e.Story.ResourceSubtype), width, "")), "", dimStyle.Render(keyLabel("main", "open")+": open ticket · "+keyLabel("main", "focus_list")+": activity list"))
	before := a.reader.YOffset()
	a.reader.SetContent(strings.Join(lines, "\n"))
	if keepScroll {
		a.reader.SetYOffset(before)
	} else {
		a.reader.GotoTop()
	}
}

func activityDue(d *asana.StoryDates) string {
	if d == nil {
		return "none"
	}
	if d.DueAt != nil {
		return ticket.OneLine(*d.DueAt)
	}
	if d.DueOn != nil {
		return ticket.OneLine(*d.DueOn)
	}
	return "none"
}

func (m *Model) handleActivityKey(msg tea.KeyPressMsg) tea.Cmd {
	a, k := m.activity, msg.String()
	if a.filtering {
		if name := keys.Name("filter", k); name == "done" || name == "cancel" {
			a.filtering = false
			a.query.Blur()
			return nil
		}
		return m.updateActivitySearch(msg)
	}
	if m.focusNav {
		if name := keys.Name("views", k); name != "" {
			return m.updateNav(name)
		}
	}
	name := keys.Name("activity", k)
	if name == "" {
		name = keys.Name("main", k)
	}
	move := 0
	switch name {
	case "quit":
		m.closeActivity()
		return tea.Quit
	case "activity":
		m.closeActivity()
		m.layout()
		return nil
	case "focus_list":
		if m.focusNav {
			m.focusNav = false
		} else if a.details {
			a.details = false
		} else {
			m.closeActivity()
			m.layout()
		}
	case "prev_mode", "next_mode":
		i := slices.Index(activityModes, a.mode)
		if name == "prev_mode" {
			i += 2
		} else {
			i++
		}
		a.mode = activityModes[i%len(activityModes)]
		m.filterActivity()
	case "toggle_reader":
		if !m.readerHidden {
			a.details = false
		}
		return m.toggleReader()
	case "toggle_sidebar":
		return m.toggleSidebar()
	case "automated":
		a.automated = !a.automated
		m.filterActivity()
	case "reload":
		return m.scanActivity(true)
	case "expand":
		if a.cursor < len(a.rows) {
			row := a.rows[a.cursor]
			if gid := a.visible[row.entry].Task.GID; row.child || row.more > 0 {
				a.expanded[gid] = !a.expanded[gid]
				m.filterActivity()
			}
		}
	case "period":
		m.openActivityPeriod()
	case "filter":
		a.filtering = true
		return a.query.Focus()
	case "focus_views":
		if m.showNav() {
			m.focusNav = true
			m.focusNavPanel()
		}
	case "focus_reader":
		m.focusNav = false
		a.details = !a.details
	case "next_pane", "prev_pane":
		m.cycleActivityPane(name == "prev_pane")
	case "open":
		if _, _, split := m.paneWidths(); !split && !a.details {
			a.details = true
			return nil
		}
		if e, ok := a.selected(); ok {
			m.activityOpen, m.activityTicket = false, true
			m.layout()
			cmd := m.showTicket(e.Task)
			// The normal reader may have been cached before this workspace
			// scan. Refresh it before the user acts on older ticket details.
			if _, cached := m.details[e.Task.GID]; cached {
				return tea.Batch(cmd, m.loadDetail(e.Task.GID))
			}
			return cmd
		}
	case "open_browser":
		if e, ok := a.selected(); ok {
			return openURL(e.Task.PermalinkURL)
		}
	case "yank", "copy_selection":
		if e, ok := a.selected(); ok {
			v := yankValue{"Ticket title", e.Task.Name}
			if name == "copy_selection" {
				return v.copy(m)
			}
			detail, loaded := m.details[e.Task.GID]
			m.yankMenu(ticket.Ticket{Task: e.Task}, detail, loaded, v)
		}
	case "copy_link":
		if e, ok := a.selected(); ok {
			m.setStatus(statusInfo, "link sent to clipboard")
			return tea.SetClipboard(e.Task.PermalinkURL)
		}
	case "projects":
		return m.requestProjects(func() tea.Cmd { m.openProjectPicker(); return nil })
	case "help":
		m.help = true
	case "down", "scroll_down":
		move = 1
	case "up", "scroll_up":
		move = -1
	case "top":
		if a.details {
			a.reader.GotoTop()
			return nil
		}
		move = -len(a.rows)
	case "bottom":
		if a.details {
			a.reader.GotoBottom()
			return nil
		}
		move = len(a.rows)
	case "half_page_down":
		move = max(a.reader.Height()/6, 1)
	case "half_page_up":
		move = -max(a.reader.Height()/6, 1)
	case "page_down":
		move = max(a.reader.Height()/3, 1)
	case "page_up":
		move = -max(a.reader.Height()/3, 1)
	}
	if move != 0 {
		if a.details {
			if move > 0 {
				a.reader.ScrollDown(move)
			} else {
				a.reader.ScrollUp(-move)
			}
			return nil
		}
		a.cursor = max(0, min(a.cursor+move, len(a.rows)-1))
		m.renderActivityDetail(false)
	}
	return nil
}

// cycleActivityPane moves focus to the next or, with back, previous of the
// views panel, the list and the event reader.
func (m *Model) cycleActivityPane(back bool) {
	a := m.activity
	first, i := 1, 1
	if m.showNav() {
		first = 0
	}
	switch {
	case m.focusNav:
		i = 0
	case a.details:
		i = 2
	}
	dir := 1
	if back {
		dir = -1
	}
	n := 3 - first
	i = first + (i-first+dir+n)%n
	m.focusNav, a.details = false, i == 2
	if i == 0 {
		m.focusNavPanel()
	}
}

func (m *Model) updateActivitySearch(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.activity.query, cmd = m.activity.query.Update(msg)
	m.filterActivity()
	return cmd
}

func (m *Model) openActivityPeriod() {
	items := []pickItem{{Label: "Last 24 hours", Value: 1}, {Label: "Last 7 days", Value: 7}, {Label: "Last 30 days", Value: 30}}
	m.modal = newPicker(pickValue(func(days int) tea.Cmd {
		m.modal = nil
		// The cache holds the longest period; shorter ones filter it.
		m.activity.days = days
		m.filterActivity()
		return nil
	}), "Activity period", items)
	m.modal.searching = false
	m.modal.input.Blur()
	m.modal.cursor = slices.Index([]int{1, 7, 30}, m.activity.days)
}

func (m *Model) activityList(width, height int) string {
	a := m.activity
	// An error or partial scan leads the list, above any older results.
	var banner string
	rows := height
	if note := a.note(); note != "" {
		banner = warnStyle.Render(ansi.Wrap(note, max(width, 1), "")) + "\n"
		rows -= lipgloss.Height(banner) - 1
	}
	box := lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height)
	if len(a.rows) == 0 {
		text := "No matching activity."
		if a.loading {
			text = "Loading workspace activity…"
		} else if a.err != "" {
			text = ""
		}
		return box.Render(banner + ansi.Wrap(text, max(width, 1), ""))
	}
	// The window ends at the cursor's row once it would pass the bottom.
	first, used := a.cursor, activityRowHeight(a.rows[a.cursor])
	for first > 0 && used+activityRowHeight(a.rows[first-1]) <= rows {
		first--
		used += activityRowHeight(a.rows[first])
	}
	var lines []string
	for i := first; i < len(a.rows) && len(lines) < rows; i++ {
		lines = append(lines, m.activityRowLines(i, width)...)
	}
	return box.Render(banner + strings.Join(lines, "\n"))
}

func activityRowHeight(r activityRow) int {
	if r.child {
		return 2
	}
	return 3
}

// activityRowLines renders list row i: an event with its ticket, or, for an
// older event of an expanded run, the event alone, indented under its run.
func (m *Model) activityRowLines(i, width int) []string {
	a := m.activity
	row := a.rows[i]
	e := a.visible[row.entry]
	lead := "  "
	if i == a.cursor {
		lead = m.sym.cursor + " "
	}
	kind := "RELATED"
	if e.Mine {
		kind = "MINE"
	}
	if e.Automatic {
		kind = "AUTO"
	}
	summary := activitySummary(e)
	at := e.At.Local().Format("01/02 15:04")
	selected := i == a.cursor && !a.details && !m.focusNav
	// A collapsed run is new when any of its events is.
	events := a.visible[row.entry : row.entry+1]
	collapsed := !row.child && row.more > 0 && !a.expanded[e.Task.GID]
	if collapsed {
		events = a.visible[row.entry : row.entry+row.more+1]
	}
	var rows []string
	if row.child {
		guide, rail := "├ ", "│ "
		if row.last {
			guide, rail = "└ ", "  "
		}
		rows = []string{lead + guide + summary, "  " + rail + at + "  " + kind}
	} else {
		icon := m.sym.icon(iconView)
		switch e.Story.ResourceSubtype {
		case "due_date_changed", "due_today":
			icon = m.sym.icon(iconDue)
		case "section_changed", "added_to_project":
			icon = m.sym.icon(iconFolder)
		}
		rows = []string{lead + icon + ticket.OneLine(e.Task.Name), "  " + summary, "  " + at + "  " + kind}
	}
	// NEW trails the first line; a collapsed run's count trails the last,
	// just above where expanding puts its older events.
	flags := make([]string, len(rows))
	if slices.ContainsFunc(events, func(e activity.Entry) bool { return e.NewSince(a.lastSeen) }) {
		flags[0] = newPill(selected)
	}
	if collapsed {
		flags[len(rows)-1] = moreStyle.Render(fmt.Sprintf(" +%d more ", row.more))
	}
	for j, line := range rows {
		flag, room := flags[j], width
		if flag != "" {
			room = max(width-ansi.StringWidth(flag)-1, 1)
		}
		line = ansi.Truncate(line, max(room, 1), "…")
		fill := strings.Repeat(" ", max(room-ansi.StringWidth(line), 0))
		switch {
		case selected && flag != "":
			line = selectedStyle.Render(line+" ") + flag + selectedStyle.Render(fill)
		case selected:
			line = selectedStyle.Render(line + fill)
		case j > 0 || row.child:
			line = dimStyle.Render(line)
		}
		if !selected && flag != "" {
			line += " " + flag
		}
		rows[j] = line
	}
	return rows
}

// note is the load error or partial-scan warning, if any.
func (a *activityView) note() string {
	if a.err != "" {
		return "Error: " + a.err
	}
	if a.report.Warning != "" {
		return "Partial: " + ticket.OneLine(a.report.Warning)
	}
	return ""
}

func (m *Model) activityPanes(h int) string {
	a := m.activity
	listW, readerW, split := m.paneWidths()
	if !split {
		a.reader.SetHeight(h)
		body := m.activityList(listW, h)
		if a.details {
			body = a.reader.View()
		}
		if m.showNav() {
			nav := m.panel("[0] Views", "", m.navView(navW-panelFrame, max(h-panelFrame, 1)), navW, h, m.focusNav, nil)
			return lipgloss.JoinHorizontal(lipgloss.Top, nav, lipgloss.NewStyle().Width(listW).Height(h).Render(body))
		}
		return lipgloss.NewStyle().Width(m.width).Height(h).Render(body)
	}
	inner := max(h-panelFrame, 1)
	a.reader.SetHeight(inner)
	count := fmt.Sprintf("%d/%d", a.cursor+1, len(a.rows))
	if len(a.rows) == 0 {
		count = "0/0"
	}
	list := m.panel("[1] Activity · "+a.mode, count, m.activityList(listW, inner), listW+panelFrame, h, !a.details && !m.focusNav, nil)
	reader := m.panel("[2] Event", "enter ticket", a.reader.View(), readerW+panelFrame, h, a.details, nil)
	if m.showNav() {
		nav := m.panel("[0] Views", "", m.navView(navW-panelFrame, inner), navW, h, m.focusNav, nil)
		return lipgloss.JoinHorizontal(lipgloss.Top, nav, list, reader)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list, reader)
}

// activityMode is mode for the Activity view.
func (m *Model) activityMode() (name string, pill lipgloss.Style, hints [][2]string) {
	switch a := m.activity; {
	case a.filtering:
		return "FILTER", filterPill, [][2]string{{typedLabel("filter", "done"), "apply"}, {typedLabel("filter", "cancel"), "cancel"}}
	case m.focusNav:
		return "VIEWS", normalPill, [][2]string{{keyLabel("views", "down", "up"), "move"}, {keyLabel("views", "open"), "open"}, {keyLabel("main", "focus_list"), "activity"}}
	case a.details:
		return "ACTIVITY", readPill, [][2]string{{keyLabel("main", "down", "up"), "scroll"}, {keyLabel("main", "open"), "ticket"}, {keyLabel("main", "focus_list"), "list"}}
	}
	return "ACTIVITY", normalPill, [][2]string{{keyLabel("main", "down", "up"), "move"}, {keyLabel("activity", "prev_mode", "next_mode"), "filter"},
		{keyLabel("activity", "period"), "period"}, {keyLabel("activity", "expand"), "expand"}, {keyLabel("main", "filter"), "search"}, {keyLabel("main", "reload"), "refresh"},
		{keyLabel("main", "activity"), "back"}, {keyLabel("main", "help"), "keys"}}
}

// activitySegs are the statusline segments for the Activity view: what it
// shows and how complete the last scan was.
func (m *Model) activitySegs() []string {
	a := m.activity
	period := fmt.Sprintf("last %d days", a.days)
	if a.days == 1 {
		period = "last 24 hours"
	}
	segs := []string{m.sym.icon(iconView) + "Activity · " + a.mode, period}
	if a.filtering {
		segs = append(segs, a.query.View())
	} else if q := a.query.Value(); q != "" {
		segs = append(segs, m.sym.icon(iconFilter)+ticket.OneLine(q))
	}
	if a.automated {
		segs = append(segs, "automated shown")
	}
	segs = append(segs, fmt.Sprintf("%d events", len(a.visible)))
	switch {
	case a.loading:
		loading := "scanning"
		if a.report.Total > 0 {
			loading = fmt.Sprintf("scanning %d/%d", a.report.Scanned, a.report.Total)
		}
		if len(m.sym.spinner) > 0 {
			loading = m.sym.spinner[m.frame%len(m.sym.spinner)] + " " + loading
		}
		segs = append(segs, loading)
	case !a.cache.Until.IsZero():
		segs = append(segs, "updated "+a.cache.Until.Local().Format("15:04"))
	}
	return segs
}

func (m *Model) activityHelp() string {
	rows := [][3]string{
		{"main", "activity", "return to tickets"},
		{"activity", "prev_mode", "previous filter: Both / Mine / Related"},
		{"activity", "next_mode", "next filter: Both / Mine / Related"},
		{"activity", "period", "last 24 hours / 7 days / 30 days"},
		{"main", "filter", "search ticket, actor, and activity text"},
		{"activity", "automated", "include / hide automated reminders"},
		{"activity", "expand", "show / hide a ticket's earlier events in a row"},
		{"main", "down", "move down; scroll event reader"},
		{"main", "up", "move up; scroll event reader"},
		{"main", "next_pane", "next pane: views, list, event reader"},
		{"main", "open", "open ticket; narrow list first shows event"},
		{"main", "focus_list", "event list; from opened ticket, Activity"},
		{"main", "toggle_reader", "show / hide preview"},
		{"main", "toggle_sidebar", "show / hide views panel"},
		{"main", "reload", "rescan the period from the API"},
		{"main", "open_browser", "open ticket in browser"},
		{"main", "yank", "yank menu"},
	}
	lines := []string{m.accentStyle.Render("Activity"), ""}
	for _, row := range rows {
		if label := keyLabel(row[0], row[1]); label != "" {
			lines = append(lines, m.accentStyle.Width(12).Render(label)+row[2])
		}
	}
	return strings.Join(lines, "\n") + "\n\n" + dimStyle.Render("Workspace search requires paid Asana access.\nCurrent ownership/mentions are labelled as current context.\nAsana consolidates stories; this is not an exhaustive audit log.\nActivity scans in the background and caches 30 days on disk.\n\nAny key closes")
}

// activitySummary describes an event in one line: "Amy commented", or the
// story's own text, with "You" for the user's own actions.
func activitySummary(e activity.Entry) string {
	summary := ticket.OneLine(e.Story.Text)
	if e.Story.ResourceSubtype == "comment_added" {
		summary = ticket.Author(e.Story) + " commented"
	}
	if e.Mine && e.Story.CreatedBy != nil {
		summary = strings.Replace(summary, ticket.OneLine(e.Story.CreatedBy.Name), "You", 1)
	}
	return summary
}
