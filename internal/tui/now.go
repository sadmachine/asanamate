package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/activity"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
)

// nowKey is the Now place's key in state, beside project gids and "" for My
// Tasks.
const nowKey = "now"

// Values of the new: and waiting: filter terms.
const (
	newTicket    = "ticket"   // never shown in the reader
	newChange    = "change"   // changed since last shown
	newComment   = "comment"  // someone else commented since
	newMention   = "mention"  // one of those comments mentions you
	newAssigned  = "assigned" // someone assigned it to you since
	waitingReply = "reply"    // your comment is the newest
)

// viewKey names the open list in state: the viewed project's gid, "" for My
// Tasks, or nowKey for Now.
func (m *Model) viewKey() string {
	if m.nowOpen {
		return nowKey
	}
	return gidOf(m.viewProject)
}

// openNow opens the Now place: My Tasks tickets that match a section of the
// now grouping.
func (m *Model) openNow() tea.Cmd {
	return m.openList(nil, true)
}

// toggleNow opens Now, or from Now returns to My Tasks.
func (m *Model) toggleNow() tea.Cmd {
	if m.nowOpen && !m.activityOpen {
		return m.openList(nil, false)
	}
	return m.openNow()
}

// nowGrouping is the grouping that decides what Now lists.
func (m *Model) nowGrouping() config.Grouping {
	g, _ := m.deps.Config.Grouping(config.NowGrouping)
	return g
}

// nowTasks keeps the tasks that match a section of the now grouping.
func (m *Model) nowTasks(tasks []asana.Task, env filter.Env) []asana.Task {
	var queries []filter.Filter
	for _, s := range m.nowGrouping().Sections {
		for _, q := range s.Match {
			queries = append(queries, filter.Parse(q))
		}
	}
	var out []asana.Task
	for _, t := range tasks {
		if slices.ContainsFunc(queries, func(f filter.Filter) bool { return f.Match(t, env) }) {
			out = append(out, t)
		}
	}
	return out
}

// defaultFolds lists the sections of the grouping named by that start folded.
func (m *Model) defaultFolds(by string) []string {
	if g, ok := m.deps.Config.Grouping(by); ok && by != "" {
		return g.FoldedSections()
	}
	return nil
}

// seenCutoff is the modified_at the ticket with gid had when last shown, and
// whether it was. The Now ticket being read keeps the cutoff its visit began
// with, so it holds its section until the cursor leaves.
func (m *Model) seenCutoff(gid string) (string, bool) {
	if gid == m.nowHeld && gid == m.sinceGID {
		return m.sinceCutoff, m.sinceCutoff != ""
	}
	return m.deps.State.SeenAt(gid)
}

// attentionLookups returns the new: and waiting: lookups. They cover only
// your My Tasks tickets: seen state tracks those, and Activity caches every
// story on them. The cached stories are indexed on first use.
func (m *Model) attentionLookups() (newValues, waitingValues func(asana.Task) []string) {
	me := m.meGID
	var byTask map[string][]activity.Entry
	entries := func(gid string) []activity.Entry {
		if byTask == nil {
			byTask = map[string][]activity.Entry{}
			for _, e := range m.activity.cache.Entries {
				byTask[e.Task.GID] = append(byTask[e.Task.GID], e)
			}
		}
		return byTask[gid]
	}
	mine := func(t asana.Task) bool {
		return m.changed != nil && me != "" && t.Assignee != nil && t.Assignee.GID == me
	}
	newValues = func(t asana.Task) []string {
		if !mine(t) {
			return nil
		}
		seen, ok := m.seenCutoff(t.GID)
		if !ok {
			return []string{newTicket}
		}
		var values []string
		if t.ModifiedAt != "" && t.ModifiedAt != seen {
			values = append(values, newChange)
		}
		cutoff, err := time.Parse(time.RFC3339Nano, seen)
		if err != nil {
			return values
		}
		for _, e := range entries(t.GID) {
			if e.Mine || e.Automatic || !e.At.After(cutoff) {
				continue
			}
			s := e.Story
			if s.ResourceSubtype == "comment_added" {
				values = append(values, newComment)
				if activity.Mentions(s.HTMLText, me) {
					values = append(values, newMention)
				}
			}
			if s.Assignee != nil && s.Assignee.GID == me {
				values = append(values, newAssigned)
			}
		}
		return values
	}
	waitingValues = func(t asana.Task) []string {
		if !mine(t) {
			return nil
		}
		var newest *activity.Entry
		for _, e := range entries(t.GID) {
			if e.Story.ResourceSubtype == "comment_added" && (newest == nil || e.At.After(newest.At)) {
				newest = &e
			}
		}
		if newest != nil && newest.Mine {
			return []string{waitingReply}
		}
		return nil
	}
	return newValues, waitingValues
}
