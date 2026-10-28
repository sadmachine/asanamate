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
	"charm.land/lipgloss/v2"

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
	footer    int              // -1 focuses the comment; otherwise an optional task control
	events    []hookEvent      // hook events from staged edits already applied
	reading   bool             // the reader has focus beside the composer pane
	resume    *tea.KeyPressMsg // the key that left the ticket, run once the composer closes
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
	events   []hookEvent
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
	b.maxTextWidth = m.deps.Config.Reader.MaxTextWidth
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
	if m.composerFits() {
		m.focusReaderPane()
	}
}

// composerPaneMinH is the least pane height that fits the composer pane, at
// most half of it, with the reader still readable above it.
const composerPaneMinH = 24

// composerFits reports whether the reader can hold the composer as its bottom
// pane: the reader is on, tall enough, and shows the composer's ticket.
func (m *Model) composerFits() bool {
	c := m.composer
	if c == nil || m.activityOpen || !m.readerOn() || m.paneHeight() < composerPaneMinH {
		return false
	}
	t, ok := m.selected()
	return ok && t.GID == c.gid
}

// composerPane reports whether the composer shows as the reader's bottom
// pane rather than a modal: it fits, and the reader is on screen, beside the
// list or focused full screen.
func (m *Model) composerPane() bool {
	return m.composerFits() && (m.readerShown() || m.focusReader)
}

// composerFocused reports whether the composer pane has focus.
func (m *Model) composerFocused() bool {
	return m.composerPane() && !m.composer.reading
}

// focusComposer returns focus from the reader to the composer.
func (m *Model) focusComposer() {
	c := m.composer
	c.reading, m.input = false, c.box
	if c.footer < 0 {
		c.box.area.Focus()
	}
}

// cancelComment handles the cancel key. A pane with unsaved work hands focus
// to the reader and stays open; otherwise the composer closes, asking first
// when it has work to keep.
func (m *Model) cancelComment() tea.Cmd {
	c := m.composer
	if !m.composerPane() || !c.unsaved() {
		return m.leaveComment(false)
	}
	c.reading, m.input = true, nil
	c.box.area.Blur()
	return nil
}

// readingKeys are the [keys.main] bindings that work in the reader beside
// the composer pane. They only show the ticket, so the draft stays open.
var readingKeys = map[string]bool{
	"down": true, "up": true, "scroll_down": true, "scroll_up": true, "top": true, "bottom": true,
	"half_page_down": true, "half_page_up": true, "page_down": true, "page_up": true,
	"focus_reader": true, "jump": true, "reader_view": true, "reload": true, "help": true,
	"yank": true, "copy_selection": true, "copy_link": true, "copy_comment": true, "open_browser": true,
}

// readBesideComment handles a key while the reader has focus beside the
// composer pane. Reading keys work and the comment key returns to the
// composer. Any other bound key leaves the ticket: it asks about the draft
// first and runs once the composer closes.
func (m *Model) readBesideComment(msg tea.KeyPressMsg) tea.Cmd {
	c := m.composer
	k := msg.String()
	name := keys.Name("main", k)
	switch {
	case name == "":
		return nil
	case name == "comment", name == "open" && m.fieldKey == commentKey && c.storyGID == "":
		m.focusComposer()
		return nil
	case readingKeys[name], name == "open" && m.readsInPlace():
		if b, ok := m.bindingFor(k); ok {
			return b.run(m, msg)
		}
		return nil
	}
	if _, ok := m.bindingFor(k); !ok && name != "focus_list" {
		// Unavailable here, as list keys are in ticket-only mode. esc still
		// leaves the comment.
		return nil
	}
	c.resume = &msg
	return m.leaveComment(false)
}

// keepComment returns to the composer from its leave confirmation.
func (m *Model) keepComment() {
	c := m.composer
	c.confirm, c.quitting, c.resume = nil, false, nil
	if c.reading {
		m.focusComposer()
	}
}

// checkDraft notes whether the selected ticket has a saved comment draft,
// which the reader's edge marks.
func (m *Model) checkDraft() {
	t, ok := m.selected()
	m.draftSaved = ok && drafts.HasComment(m.deps.StateDir, m.deps.Config.Workspace, t.GID)
}

// composerPaneView draws the composer pane's body, at most half the reader's
// height, to fit inside its panel below the reader.
func (m *Model) composerPaneView() string {
	_, w, split := m.paneWidths()
	if !m.readerFramed(split) {
		w -= panelFrame // a bare reader's panel-free width
	}
	body := m.composer.paneView(max(w-2, 1), max(m.paneHeight()/2-panelFrame, 3), m.modalLook())
	return lipgloss.NewStyle().Padding(0, 1).Render(body)
}

// syncComposer fits the reader to the composer pane. The reader scrolls by
// the change in the pane's height, so its bottom line stays in view as the
// pane opens, grows, or closes. A composer that loses its pane, as when the
// terminal shortens, takes focus again as a modal.
func (m *Model) syncComposer() {
	if c := m.composer; c != nil && c.reading && m.modal == nil && !m.composerPane() {
		m.focusComposer()
	}
	h := 0
	if m.composerPane() {
		h = lipgloss.Height(m.composerPaneView()) + panelFrame
	}
	offset, grown := m.reader.YOffset(), h-m.composerH
	m.composerH = h
	m.reader.SetHeight(m.readerHeight())
	if grown != 0 {
		m.reader.SetYOffset(offset + grown)
	}
}

// afterComposer finishes leaving the closed composer c: it refreshes the
// draft marker, then quits or runs the key that left the ticket.
func (m *Model) afterComposer(c *commentComposer) tea.Cmd {
	m.checkDraft()
	if c.quitting {
		m.closeActivity()
		return tea.Quit
	}
	if c.resume != nil {
		return m.handleKey(*c.resume)
	}
	return nil
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
			// j/k move between task controls without leaving them; tab
			// still cycles back to the comment.
			switch bound("picker", key, false) {
			case "down":
				c.footer = min(c.footer+1, 2)
				return nil
			case "up":
				c.footer = max(c.footer-1, 0)
				return nil
			}
			switch bound("input", key, true) {
			case "submit":
				return m.postComment()
			case "save_draft":
				return c.box.onSaveDraft()
			case "cancel":
				return m.cancelComment()
			}
			return nil
		}
	}
	if c.confirm != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			res, cmd := c.confirm.update(key)
			if res.cancelled {
				m.keepComment()
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
		return m.cancelComment()
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
	if !c.unsaved() {
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
			m.keepComment()
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
	var base asana.Task
	if c.task != nil {
		base = *c.task
	}
	eventsAfter := make([][]hookEvent, len(draft.Changes)+1)
	for i := range draft.Changes {
		eventsAfter[i+1] = m.changeEvents(base, draft.Changes[:i+1])
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
			msg.draft, msg.err, msg.events = draft, err, eventsAfter[done]
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
	c.events = append(c.events, msg.events...)
	c.deliveryHint()
	if msg.err != nil {
		// Empty input has nothing to protect. Close even if an old draft
		// cannot be removed, preserving that file and reporting the failure.
		if msg.op == commentDiscard && !c.hasChanges() {
			m.composer, m.input, m.modal, m.edit = nil, nil, nil, nil
			m.setStatus(statusWarning, "comment closed; draft retained: "+msg.err.Error())
			return m.afterComposer(c)
		}
		c.quitting, c.resume = false, nil
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
		m.checkDraft()
		what := "comment"
		if c.storyGID != "" {
			what = "edit comment"
		}
		if c.task != nil && (applied > 0 || len(c.events) > 0 || c.review != nil) {
			what = "update ticket"
		}
		cmd := m.editDone(editDoneMsg{gid: c.gid, what: what, events: c.events})
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
	return m.afterComposer(c)
}
