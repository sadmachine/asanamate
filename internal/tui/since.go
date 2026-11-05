package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// keepSinceCutoff saves, once per visit to the ticket with gid, the version
// it had when last seen, before markSeen moves that on. Later renders of the
// same ticket, such as refreshes, keep it, so the Since you looked section
// holds still while you read.
func (m *Model) keepSinceCutoff(gid string) {
	if gid == m.sinceGID {
		return
	}
	m.sinceGID = gid
	m.sinceCutoff, _ = m.deps.State.SeenAt(gid)
}

// isNew reports whether story s on the ticket with gid arrived after the
// saved cutoff and came from someone else. Nothing is new while the section
// is off, or for a ticket never seen, so a first visit lists nothing.
func (m *Model) isNew(gid string, s asana.Story) bool {
	if !m.sinceShow || gid != m.sinceGID || m.sinceCutoff == "" {
		return false
	}
	if s.CreatedBy != nil && m.meGID != "" && s.CreatedBy.GID == m.meGID {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, s.CreatedAt)
	if err != nil {
		return false
	}
	cutoff, err := time.Parse(time.RFC3339Nano, m.sinceCutoff)
	return err == nil && created.After(cutoff)
}

// keyedEntry is a timeline entry with its cards view target key, or "" for
// a system change, which is not a target.
type keyedEntry struct {
	ticket.Entry
	key string
}

// splitActivity splits t's timeline, oldest first, into the entries new
// since you looked, comments and changes alike, and the rest the activity
// lists: every entry in the All tab, only comments in the Comments tab.
// Comment keys count every comment, so they match in either list and tab.
func (m *Model) splitActivity(t ticket.Ticket) (fresh, rest []keyedEntry) {
	comments := 0
	for _, e := range t.Timeline() {
		k := keyedEntry{Entry: e}
		if e.Comment {
			k.key = commentTarget(e.Story, comments)
			comments++
		}
		switch {
		case m.isNew(t.GID, e.Story):
			fresh = append(fresh, k)
		case e.Comment || m.allActivity:
			rest = append(rest, k)
		}
	}
	return fresh, rest
}

// lastCommentKey is the target key of t's newest comment, which the
// activity never cuts to a preview; "" when it has none.
func lastCommentKey(t ticket.Ticket) string {
	if len(t.Comments) == 0 {
		return ""
	}
	return commentTarget(t.Comments[len(t.Comments)-1], len(t.Comments)-1)
}

// entryBlocks renders entries width columns wide, the first starting on
// reader line line, as blocks to join with blank lines: each comment alone,
// and consecutive changes together, a line each. It records each comment's
// lines and target. Comments but lastKey and opened ones keep preview lines
// of text; 0 shows them in full.
func (m *Model) entryBlocks(entries []keyedEntry, width, line, preview int, lastKey string) []string {
	var blocks []string
	event := false
	for _, e := range entries {
		if !e.Comment {
			row := dimStyle.Render(ansi.Truncate(ticket.Author(e.Story)+" · "+m.storyTime(e.CreatedAt)+" · "+ticket.OneLine(e.Text), width, "…"))
			if event {
				blocks[len(blocks)-1] += "\n" + row
				line++
			} else {
				blocks = append(blocks, row)
				line += 2
			}
			event = true
			continue
		}
		event = false
		m.fieldLines[e.key] = line
		m.cardTargets = append(m.cardTargets, e.key)
		cut := 0
		if e.key != lastKey && !m.openComments[e.key] {
			cut = preview
		}
		block, clipped := m.comment(e.Story, width, m.fieldKey == e.key, cut)
		m.clipped[e.key] = clipped
		m.fieldEnds[e.key] = line + lipgloss.Height(block) - 1
		line += lipgloss.Height(block) + 1
		blocks = append(blocks, block)
	}
	return blocks
}

// sinceCard boxes fresh in the Since you looked card, width columns wide,
// with its top edge on reader line top; "" when nothing is new. Comments
// show in full, oldest first.
func (m *Model) sinceCard(fresh []keyedEntry, width, top int) string {
	if len(fresh) == 0 {
		return ""
	}
	body := strings.Join(m.entryBlocks(fresh, width-4, top+1, 0, ""), "\n\n")
	title := warnStyle.Bold(true).Render("Since you looked") + dimStyle.Render(fmt.Sprintf(" %d", len(fresh)))
	return m.box(title, body, width, warnStyle.GetForeground())
}

// sinceMarkdown is the markdown view's Since you looked section for fresh;
// "" when nothing is new.
func sinceMarkdown(fresh []keyedEntry) string {
	if len(fresh) == 0 {
		return ""
	}
	entries := make([]ticket.Entry, len(fresh))
	for i, e := range fresh {
		entries[i] = e.Entry
	}
	return "\n## Since you looked\n\n" + ticket.ActivityMarkdown(entries) + "\n"
}
