package tui

import (
	"context"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/pullrequests"
)

// prRun is a pull request command's last results, kept when a later run
// fails.
type prRun struct {
	prs    []pullrequests.PullRequest
	seq    uint64
	failed bool
}

type (
	// prTicketsMsg carries a lookup's tickets, their remotes read off the
	// UI goroutine, to the commands.
	prTicketsMsg struct {
		seq     uint64
		tickets []pullrequests.Ticket
	}
	// prMsg is one command's answer.
	prMsg struct {
		index int
		seq   uint64
		prs   []pullrequests.PullRequest
		err   error
	}
)

// prSectionKey is the reader's Pull requests section; each pull request in
// it is a target of its own, prKeyPrefix and its URL.
const (
	prSectionKey = "section:pull-requests"
	prKeyPrefix  = "pr:"
)

// checkPullRequests looks up the pull requests of the loaded tickets with
// linked repos, with every pull_requests command. It reads the repos'
// remotes first, once each.
func (m *Model) checkPullRequests() tea.Cmd {
	commands := m.deps.Config.PullRequests.Commands
	if len(commands) == 0 {
		return nil
	}
	var tickets []pullrequests.Ticket
	for _, t := range m.gitTargets() {
		ticket := pullrequests.Ticket{GID: t.gid, URL: t.url, Branch: t.branch}
		for _, r := range t.repos {
			ticket.Repos = append(ticket.Repos, pullrequests.Repo{Path: r})
		}
		tickets = append(tickets, ticket)
	}
	if len(tickets) == 0 {
		return nil
	}
	if len(m.feed.prRuns) != len(commands) {
		m.feed.prRuns = make([]prRun, len(commands))
	}
	m.feed.prSeq++
	seq := m.feed.prSeq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), feed.Timeout)
		defer cancel()
		return prTicketsMsg{seq: seq, tickets: pullrequests.WithRemotes(ctx, tickets)}
	}
}

// lookUpPullRequests runs every command concurrently on msg's tickets.
func (m *Model) lookUpPullRequests(msg prTicketsMsg) tea.Cmd {
	if msg.seq != m.feed.prSeq {
		return nil
	}
	var cmds []tea.Cmd
	for i, command := range m.deps.Config.PullRequests.Commands {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), feed.Timeout)
			defer cancel()
			prs, _, err := pullrequests.Lookup(ctx, command, msg.tickets)
			return prMsg{index: i, seq: msg.seq, prs: prs, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// gotPullRequests stores a command's answer, or marks it failed and keeps
// its previous results. The list refilters, and the reader redraws, only
// when the merged results change.
func (m *Model) gotPullRequests(msg prMsg) {
	if msg.index >= len(m.feed.prRuns) {
		return
	}
	r := &m.feed.prRuns[msg.index]
	if msg.seq < r.seq {
		return // a run that started earlier answered late
	}
	r.seq = msg.seq
	if msg.err != nil {
		r.failed = true
		return
	}
	r.prs, r.failed = msg.prs, false
	replies := make([][]pullrequests.PullRequest, len(m.feed.prRuns))
	for i, run := range m.feed.prRuns {
		replies[i] = run.prs
	}
	byGID := map[string][]pullrequests.PullRequest{}
	for _, p := range pullrequests.Merge(replies) {
		byGID[p.TicketGID] = append(byGID[p.TicketGID], p)
	}
	if maps.EqualFunc(m.feed.prs, byGID, slices.Equal) {
		return
	}
	shown := !slices.Equal(m.feed.prs[m.shownGID], byGID[m.shownGID])
	m.feed.prs = byGID
	// Filters and groupings can use pr:, checks:, and review: terms.
	m.applyFilter()
	if shown {
		m.renderDetail(true)
	}
}

// prsFailed reports whether a pull request command's last run failed.
func (m *Model) prsFailed() bool {
	return slices.ContainsFunc(m.feed.prRuns, func(r prRun) bool { return r.failed })
}

// prValues is a filter lookup: field's value on each of a ticket's pull
// requests, only the open and draft ones when active, without empty values.
func (m *Model) prValues(active bool, field func(pullrequests.PullRequest) string) func(asana.Task) []string {
	return func(t asana.Task) []string {
		var values []string
		for _, p := range m.feed.prs[t.GID] {
			if v := field(p); v != "" && (!active || p.Active()) && !slices.Contains(values, v) {
				values = append(values, v)
			}
		}
		return values
	}
}

// prWords name checks and reviews in the reader.
var (
	prChecksWords = map[string]string{
		pullrequests.ChecksPassing: "checks passing",
		pullrequests.ChecksFailing: "checks failing",
		pullrequests.ChecksPending: "checks pending",
	}
	prReviewWords = map[string]string{
		pullrequests.ReviewApproved: "approved",
		pullrequests.ReviewChanges:  "changes requested",
		pullrequests.ReviewPending:  "review required",
	}
)

// prStatus is p's state, then for an open or draft one its checks and
// review, such as "open · checks failing · changes requested", each styled
// when style is set.
func prStatus(p pullrequests.PullRequest, style bool) string {
	render := func(s lipgloss.Style, text string) string {
		if style {
			return s.Render(text)
		}
		return text
	}
	stateStyle := okStyle
	if p.State != pullrequests.StateOpen {
		stateStyle = dimStyle
	}
	parts := []string{render(stateStyle, p.State)}
	if p.Active() {
		tones := map[string]lipgloss.Style{
			pullrequests.ChecksPassing: okStyle, pullrequests.ChecksFailing: errorStyle, pullrequests.ChecksPending: warnStyle,
		}
		if w := prChecksWords[p.Checks]; w != "" {
			parts = append(parts, render(tones[p.Checks], w))
		}
		tones = map[string]lipgloss.Style{
			pullrequests.ReviewApproved: okStyle, pullrequests.ReviewChanges: errorStyle, pullrequests.ReviewPending: warnStyle,
		}
		if w := prReviewWords[p.Review]; w != "" {
			parts = append(parts, render(tones[p.Review], w))
		}
	}
	sep := " · "
	if style {
		sep = dimStyle.Render(sep)
	}
	return strings.Join(parts, sep)
}

// prLine is p's reader line, width columns wide:
// "#42 open · checks failing · changes requested  Fix login redirect".
// A merged or closed pull request is dim; the selected one is marked.
func (m *Model) prLine(p pullrequests.PullRequest, width int) string {
	line := prStatus(p, true) + "  " + p.Title
	if label := p.Label(); label != "" {
		line = label + " " + line
	}
	switch {
	case m.fieldKey == prKeyPrefix+p.URL:
		line = m.fieldStyle().Render(ansi.Strip(line))
	case !p.Active():
		line = dimStyle.Render(ansi.Strip(line))
	}
	return ansi.Truncate(line, width, "…")
}

// prMarkdown is the markdown view's Pull requests section for prs, each a
// link; "" when there are none.
func prMarkdown(prs []pullrequests.PullRequest) string {
	if len(prs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Pull requests\n\n")
	for _, p := range prs {
		label := p.Label()
		if label == "" {
			label = "link"
		}
		link := strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20").Replace(p.URL)
		b.WriteString("- [" + label + "](<" + link + ">) " + prStatus(p, false) + " — " + p.Title + "\n")
	}
	return b.String()
}
