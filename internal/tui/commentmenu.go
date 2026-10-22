package tui

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

type commentOp int

const (
	commentReact commentOp = iota
	commentEdit
	commentDelete
)

// quickReactions are the emoji the react picker offers on every comment.
var quickReactions = []struct{ emoji, name string }{
	{"👍", "thumbs up"}, {"❤️", "heart"}, {"🎉", "party"}, {"😂", "laugh"},
	{"👀", "eyes"}, {"🙏", "thanks"}, {"✅", "done"},
}

// openCommentMenu lists what can be done to comment c of the ticket with gid.
func (m *Model) openCommentMenu(gid string, c asana.Story) {
	items := []pickItem{{Label: "React", Key: "r", Value: commentReact}}
	if c.IsEditable {
		items = append(items,
			pickItem{Label: "Edit", Key: "e", Value: commentEdit},
			pickItem{Label: "Delete…", Key: "d", Value: commentDelete})
	}
	p := newPicker(pickValue(func(op commentOp) tea.Cmd {
		m.modal = nil
		switch op {
		case commentReact:
			m.openReactPicker(gid, c)
		case commentEdit:
			m.openCommentEditor(gid, c)
		case commentDelete:
			m.confirmDeleteComment(gid, c)
		}
		return nil
	}), "Comment by "+ticket.Author(c), items)
	p.keySelect = true
	m.modal = p
}

func (m *Model) confirmDeleteComment(gid string, c asana.Story) {
	p := newPicker(pickValue(func(confirmed bool) tea.Cmd {
		m.modal = nil
		if !confirmed {
			return nil
		}
		svc := m.writer()
		m.setProgress("delete comment…")
		return saveEdit(gid, "delete comment", func(ctx context.Context) error {
			return svc.DeleteComment(ctx, c.GID)
		})
	}), "Delete "+ticket.Author(c)+"'s comment?", []pickItem{
		{Label: "Cancel", Key: "c", Value: false},
		{Label: "Delete", Key: "d", Value: true, Help: "Permanently delete this comment and its reactions."},
	})
	p.keySelect = true
	p.stopSearch()
	m.modal = p
}

// openReactPicker offers the quick reactions, then any other emoji already on
// comment c. Picking one the user reacted with removes it.
func (m *Model) openReactPicker(gid string, c asana.Story) {
	mine := make(map[string]bool)
	for _, r := range c.Reactions {
		mine[r.Emoji] = mine[r.Emoji] || r.Reacted
	}
	var items []pickItem
	add := func(emoji, name string) {
		label := ticket.OneLine(emoji)
		if name != "" {
			label += "  " + name
		}
		item := pickItem{Label: label, Value: emoji}
		if len(items) < 9 {
			item.Key = strconv.Itoa(len(items) + 1)
		}
		if mine[emoji] {
			item.Hint = "remove"
		}
		items = append(items, item)
	}
	quick := make(map[string]bool, len(quickReactions))
	for _, q := range quickReactions {
		add(q.emoji, q.name)
		quick[q.emoji] = true
	}
	for _, r := range c.Reactions {
		if r.Count > 0 && !quick[r.Emoji] {
			add(r.Emoji, "")
		}
	}
	p := newPicker(pickValue(func(emoji string) tea.Cmd {
		m.modal = nil
		svc, story, reacted := m.writer(), c.GID, !mine[emoji]
		what := "react " + ticket.OneLine(emoji)
		if !reacted {
			what = "remove " + ticket.OneLine(emoji)
		}
		m.setProgress(what + "…")
		return saveEdit(gid, what, func(ctx context.Context) error {
			return svc.React(ctx, story, emoji, reacted)
		})
	}), "React to "+ticket.Author(c)+"'s comment", items)
	p.keySelect = true
	m.modal = p
}
