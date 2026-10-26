package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/drafts"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/writeback"
)

// commentComposer owns the editor until a draft save, discard, or successful post.
// Keeping the editor while a write runs prevents failures from losing input.
type commentComposer struct {
	gid       string
	storyGID  string // empty when composing a new comment
	original  string // initial Markdown, used to close an unchanged edit quietly
	box       *inputBox
	store     *drafts.CommentStore
	images    map[string][]byte
	uncertain bool
	busy      bool
	confirm   *picker
	quitting  bool
	task      *asana.Task // new comments can also stage task edits
	changes   []writeback.Change
	labels    map[string]string
	branch    *string
	review    *picker
	footer    int // -1 focuses the comment; otherwise an optional task control
	logTime   bool
}

type commentOperation int

const (
	commentSave commentOperation = iota
	commentDiscard
	commentPost
)

type commentDoneMsg struct {
	composer *commentComposer
	op       commentOperation
	draft    drafts.Comment
	saved    bool
	err      error
	cleanup  error // a successful post whose draft could not be removed
	logTime  bool
}

func (m *Model) openCommentComposer(t asana.Task) {
	m.openCommentInput(t.GID, "", "Comment on "+ticket.Clean(t.Name), "")
	t.CustomFields, t.Memberships = slices.Clone(t.CustomFields), slices.Clone(t.Memberships)
	m.composer.task = &t
}

func (m *Model) openCommentEditor(gid string, story asana.Story) {
	m.openCommentInput(gid, story.GID, "Edit comment by "+ticket.Author(story), ticket.CommentMarkdown(story))
}

func (m *Model) openCommentInput(gid, storyGID, title, initial string) {
	b := newInputBox(title, "comment text")
	b.area.DynamicHeight = true
	b.area.MinHeight = 10
	b.area.MaxContentHeight = math.MaxInt
	b.images = true
	b.area.SetValue(initial)
	c := &commentComposer{gid: gid, storyGID: storyGID, original: initial, box: b, footer: -1}
	identity := gid
	if storyGID != "" {
		identity = "story:" + storyGID
	}
	store, draft, err := drafts.OpenComment(m.deps.StateDir, m.deps.Config.Workspace, identity)
	if err != nil {
		b.err = "loading draft: " + err.Error()
	} else {
		c.store, c.images, c.uncertain = store, draft.Images, draft.Uncertain
		c.changes, c.labels, c.branch = draft.Changes, draft.ChangeLabels, draft.Branch
		if draft.Text != "" || len(draft.Changes) > 0 || draft.Branch != nil {
			b.area.SetValue(draft.Text)
			b.title += " (draft)"
		}
		c.deliveryHint()
	}
	b.onSubmit = func(string) tea.Cmd { return m.postComment() }
	b.onSaveDraft = func() tea.Cmd {
		if !c.hasChanges() {
			return m.leaveComment(false)
		}
		return m.runComment(commentSave)
	}
	m.composer, m.input, m.edit = c, b, nil
}

func (c *commentComposer) deliveryHint() {
	c.box.help, c.box.notice = "", ""
	if c.storyGID != "" {
		c.box.help = "Existing mentions and images use asana:<gid> links. Keep these links to preserve them."
	}
	if c.uncertain {
		c.box.notice = "Delivery uncertain. Check the ticket's comments before submitting again."
	}
}

func (m *Model) updateComment(msg tea.Msg) tea.Cmd {
	c := m.composer
	if c.busy {
		return nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && c.task != nil && c.confirm == nil {
		switch key.String() {
		case "tab", "shift+tab":
			step := 1
			if key.String() == "shift+tab" {
				step = 3
			}
			c.footer = (c.footer+1+step)%4 - 1
			if c.footer == -1 {
				c.box.area.Focus()
				return nil
			}
			c.box.area.Blur()
			return nil
		case "enter":
			switch c.footer {
			case 0:
				return m.beginDraftEdit(editAssignee)
			case 1:
				return m.beginDraftEdit(editSection)
			case 2:
				m.openBatchReview()
				return nil
			}
		}
		if c.footer >= 0 {
			switch bound("input", key, true) {
			case "submit":
				return m.postComment()
			case "save_draft":
				return c.box.onSaveDraft()
			case "cancel":
				return m.leaveComment(false)
			}
			return nil
		}
	}
	if c.confirm != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			res, cmd := c.confirm.update(key)
			if res.cancelled {
				c.confirm, c.quitting = nil, false
			} else if res.done {
				return tea.Batch(cmd, c.confirm.onPick(res))
			}
			return cmd
		}
		return nil
	}
	res, cmd := c.box.update(msg)
	if res.cancelled {
		if c.review != nil {
			m.openBatchReview()
			return cmd
		}
		return m.leaveComment(false)
	}
	if res.done {
		return m.postComment()
	}
	return cmd
}

func (m *Model) leaveComment(quit bool) tea.Cmd {
	c := m.composer
	if c.busy {
		return nil
	}
	c.quitting = quit
	if !c.hasChanges() || (c.storyGID != "" && c.box.area.Value() == c.original) {
		return m.runComment(commentDiscard)
	}
	title := "Save comment as draft?"
	if len(c.changes) > 0 || c.branch != nil || c.review != nil {
		title = "Save changes as draft?"
	}
	c.confirm = newPicker(pickValue(func(choice string) tea.Cmd {
		c.confirm = nil
		switch choice {
		case "keep":
			c.quitting = false
			return nil
		case "save":
			return m.runComment(commentSave)
		default:
			return m.runComment(commentDiscard)
		}
	}), title, []pickItem{
		{Label: "Save draft", Value: "save"},
		{Label: "Discard", Value: "discard"},
		{Label: "Keep editing", Value: "keep"},
	})
	// Confirmations never begin in search mode, even with picker.type_first.
	c.confirm.stopSearch()
	return nil
}

func (m *Model) postComment() tea.Cmd {
	c := m.composer
	if strings.TrimSpace(c.box.area.Value()) == "" {
		if c.storyGID != "" {
			c.box.err = "comment text is empty"
			return nil
		}
		if !c.hasChanges() {
			return m.leaveComment(false)
		}
	}
	if c.uncertain {
		retry := "Post again"
		if c.storyGID != "" {
			retry = "Save again"
		}
		c.confirm = newPicker(pickValue(func(post bool) tea.Cmd {
			c.confirm = nil
			if post {
				return m.runComment(commentPost)
			}
			return nil
		}), "Delivery uncertain: check comments before retrying", []pickItem{
			{Label: "Keep editing", Value: false},
			{Label: retry, Value: true},
		})
		c.confirm.stopSearch()
		return nil
	}
	return m.runComment(commentPost)
}

// runComment does disk and network work off the UI loop. Saving before posting
// also protects against a crash while Asana is receiving the comment.
func (m *Model) runComment(op commentOperation) tea.Cmd {
	c := m.composer
	if c.store == nil {
		if op == commentDiscard {
			return m.commentDone(commentDoneMsg{composer: c, op: op})
		}
		c.setError("draft storage is unavailable; keep this text and fix the draft file before saving or posting")
		return nil
	}
	if op == commentPost && c.branch != nil {
		previous, existed := m.deps.State.TaskBranches[c.gid]
		m.deps.State.SetTaskBranch(c.gid, *c.branch)
		if err := m.deps.State.Save(); err != nil {
			m.deps.State.SetTaskBranch(c.gid, previous)
			if !existed {
				delete(m.deps.State.TaskBranches, c.gid)
			}
			c.setError("saving branch: " + err.Error())
			return nil
		}
		c.branch = nil
		m.linked = nil
		m.renderDetail(true)
	}
	c.busy = true
	c.box.err, c.box.progress = "", "Saving draft…"
	if op == commentDiscard {
		c.box.progress = "Discarding draft…"
	} else if op == commentPost {
		c.box.progress = "Posting comment…"
		if c.task != nil && (len(c.changes) > 0 || c.review != nil) {
			c.box.progress = "Submitting changes…"
		}
		if c.storyGID != "" {
			c.box.progress = "Updating comment…"
		}
	}
	if c.review != nil && m.modal == c.review {
		c.review.err, c.review.progress = "", c.box.progress
	}
	draft := drafts.Comment{Text: c.box.area.Value(), Images: maps.Clone(c.images), Uncertain: c.uncertain,
		Changes: slices.Clone(c.changes), ChangeLabels: maps.Clone(c.labels), Branch: c.branch}
	svc, gid := m.writer(), c.gid
	logAfter := make([]bool, len(draft.Changes)+1)
	for i := range draft.Changes {
		logAfter[i+1] = m.draftLogTime(draft.Changes[:i+1])
	}
	return tea.Batch(m.startSpinner(), request(func(ctx context.Context) tea.Msg {
		msg := commentDoneMsg{composer: c, op: op}
		if op == commentDiscard {
			msg.err = c.store.Remove()
			return msg
		}
		images := map[string][]byte{}
		for _, path := range writeback.LocalImages(draft.Text) {
			data, saved := draft.Images[path]
			if !saved {
				var err error
				data, err = writeback.ReadImage(path)
				if err != nil {
					msg.err = fmt.Errorf("comment image %s: %w", path, err)
					return msg
				}
			}
			images[path] = data
		}
		draft.Images = images
		if msg.err = c.store.Save(draft); msg.err != nil {
			return msg
		}
		msg.saved, msg.draft = true, draft
		if op == commentSave {
			return msg
		}
		if len(draft.Changes) > 0 {
			done, err := svc.Batch(ctx, gid, draft.Changes)
			draft.Changes = draft.Changes[done:]
			msg.draft, msg.err, msg.logTime = draft, err, logAfter[done]
			if saveErr := c.store.Save(draft); saveErr != nil {
				msg.err = errors.Join(err, fmt.Errorf("saving remaining edits: %w", saveErr))
			}
			if msg.err != nil {
				return msg
			}
		}
		if strings.TrimSpace(draft.Text) == "" {
			msg.cleanup = c.store.Remove()
			return msg
		}
		// Only comment delivery is non-idempotent. Mark uncertainty immediately
		// before posting, after checkpointing successful task edits.
		draft.Uncertain = true
		msg.draft = draft
		if msg.err = c.store.Save(draft); msg.err != nil {
			return msg
		}
		svc.Images = images
		if c.storyGID != "" {
			msg.err = svc.EditComment(ctx, c.storyGID, draft.Text)
		} else {
			msg.err = svc.Comment(ctx, gid, draft.Text)
		}
		if msg.err != nil {
			draft.Uncertain = commentDeliveryUncertain(msg.err)
			msg.draft = draft
			if err := c.store.Save(draft); err != nil {
				msg.err = fmt.Errorf("%w; saving recovery draft: %v", msg.err, err)
			}
		} else {
			msg.cleanup = c.store.Remove()
		}
		return msg
	}))
}

func commentDeliveryUncertain(err error) bool {
	var networkError net.Error
	var apiError *asana.APIError
	return errors.As(err, &networkError) || errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		(errors.As(err, &apiError) && apiError.Status >= 500)
}

func (m *Model) commentDone(msg commentDoneMsg) tea.Cmd {
	c := m.composer
	if c == nil || c != msg.composer {
		return nil
	}
	c.busy, c.box.progress = false, ""
	if msg.op == commentPost {
		m.observeAsana("posting comment", msg.err)
	}
	applied := 0
	if msg.saved {
		if msg.op == commentPost {
			applied = len(c.changes) - len(msg.draft.Changes)
			m.applyDraftChanges(c.changes[:applied])
		}
		c.images, c.uncertain = msg.draft.Images, msg.draft.Uncertain
		c.changes, c.labels, c.branch = msg.draft.Changes, msg.draft.ChangeLabels, msg.draft.Branch
	}
	c.logTime = c.logTime || msg.logTime
	c.deliveryHint()
	if msg.err != nil {
		// Empty input has nothing to protect. Close even if an old draft
		// cannot be removed, preserving that file and reporting the failure.
		if msg.op == commentDiscard && !c.hasChanges() {
			m.composer, m.input, m.modal, m.edit = nil, nil, nil, nil
			m.setStatus(statusWarning, "comment closed; draft retained: "+msg.err.Error())
			if c.quitting {
				m.closeActivity()
				return tea.Quit
			}
			return nil
		}
		c.quitting = false
		c.box.err = msg.err.Error()
		if c.review != nil {
			m.openBatchReview()
		}
		if applied > 0 {
			refresh := m.editDone(editDoneMsg{gid: c.gid, what: "update ticket"})
			m.setStatus(statusWarning, "some changes saved; remaining edits kept in draft")
			return refresh
		}
		return nil
	}
	m.composer, m.input, m.modal, m.edit = nil, nil, nil, nil
	switch msg.op {
	case commentSave:
		m.setStatus(statusInfo, "comment draft saved")
	case commentDiscard:
		m.setStatus(statusInfo, "comment draft discarded")
	case commentPost:
		what := "comment"
		if c.storyGID != "" {
			what = "edit comment"
		}
		if c.task != nil && (applied > 0 || c.logTime || c.review != nil) {
			what = "update ticket"
		}
		cmd := m.editDone(editDoneMsg{gid: c.gid, what: what, logTime: c.logTime})
		if msg.cleanup != nil {
			message := "comment posted"
			if what == "update ticket" {
				message = "ticket updated"
			}
			if c.storyGID != "" {
				message = "comment updated"
			}
			m.setStatus(statusWarning, message+"; removing draft failed: "+msg.cleanup.Error())
		}
		return cmd
	}
	if c.quitting {
		m.closeActivity()
		return tea.Quit
	}
	return nil
}
