package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/dateinput"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/writeback"
)

func (c *commentComposer) hasChanges() bool {
	return strings.TrimSpace(c.box.area.Value()) != "" || len(c.changes) > 0 || c.branch != nil
}

// unsaved reports whether closing would lose work: changes, other than an
// edited comment returned to its original text.
func (c *commentComposer) unsaved() bool {
	return c.hasChanges() && (c.storyGID == "" || c.box.area.Value() != c.original)
}

func changeKey(c writeback.Change) string {
	if c.Kind == "project_add" || c.Kind == "project_remove" {
		return "project:" + c.Target
	}
	return c.Kind + ":" + c.Target + ":" + c.Project
}

func (c *commentComposer) change(kind, target, project string) (writeback.Change, bool) {
	key := changeKey(writeback.Change{Kind: kind, Target: target, Project: project})
	for _, change := range c.changes {
		if changeKey(change) == key {
			return change, true
		}
	}
	return writeback.Change{}, false
}

func (c *commentComposer) value(kind, target, project, fallback string) string {
	if change, ok := c.change(kind, target, project); ok {
		return change.Value
	}
	return fallback
}

// stageChange replaces an earlier edit of the same detail. Returning to its
// initial value removes the change, so untouched details never get written.
func (m *Model) stageChange(change writeback.Change, label, original string) tea.Cmd {
	c := m.composer
	c.box.err = ""
	key := changeKey(change)
	c.changes = slices.DeleteFunc(c.changes, func(old writeback.Change) bool { return changeKey(old) == key })
	if change.Value != original {
		c.changes = append(c.changes, change)
		if c.labels == nil {
			c.labels = map[string]string{}
		}
		c.labels[key] = label
	} else {
		delete(c.labels, key)
	}
	// Move within My Tasks before assigning away. Remove memberships last.
	slices.SortStableFunc(c.changes, func(a, b writeback.Change) int {
		rank := func(c writeback.Change) int {
			if c.Kind == "move" {
				return 0
			}
			if c.Kind == "project_remove" {
				return 2
			}
			return 1
		}
		return rank(a) - rank(b)
	})
	m.returnToDraft()
	return nil
}

func (m *Model) beginDraftEdit(op editOp) tea.Cmd {
	m.input, m.modal = nil, nil
	m.edit = &pendingEdit{ticket: ticket.Ticket{Task: *m.composer.task}}
	return m.pickedEdit(op)
}

func (m *Model) returnToDraft() {
	m.input, m.modal, m.edit = nil, nil, nil
	if m.composer.reading {
		return
	}
	if m.composer.review != nil {
		m.openBatchReview()
	} else {
		m.input = m.composer.box
	}
}

func (m *Model) openBatchReview() {
	c := m.composer
	t := *c.task
	items := []pickItem{}
	add := func(label, value string, change writeback.Change, edit func() tea.Cmd) {
		if pending, ok := c.change(change.Kind, change.Target, change.Project); ok {
			value = c.labels[changeKey(pending)]
			if value == "" {
				value = pending.Value
			}
			label = "* " + label
		}
		if value == "" {
			value = "(empty)"
		}
		items = append(items, pickItem{Label: ticket.OneLine(label), Hint: ticket.OneLine(value), Value: edit,
			Help: "Edits stay here until submitted. * marks a pending change."})
	}
	assignee := "(unassigned)"
	if t.Assignee != nil {
		assignee = t.Assignee.Name
	}
	add("Assign to", assignee, writeback.Change{Kind: "assign"}, func() tea.Cmd { return m.beginDraftEdit(editAssignee) })
	due := ""
	if t.DueOn != nil {
		due = *t.DueOn
	}
	add("Due date", due, writeback.Change{Kind: "due"}, func() tea.Cmd { return m.beginDraftEdit(editDue) })
	add("Completed", strconv.FormatBool(t.Completed), writeback.Change{Kind: "complete"}, func() tea.Cmd { return m.beginDraftEdit(editComplete) })
	add("Title", t.Name, writeback.Change{Kind: "title"}, func() tea.Cmd { return m.beginDraftEdit(editTitle) })
	add("Description", ticket.Ticket{Task: t}.DescriptionMarkdown(), writeback.Change{Kind: "description"}, func() tea.Cmd { return m.beginDraftEdit(editDescription) })
	for _, project := range m.sectionTargets(t) {
		gid := project.GID
		value := t.SectionIn(gid)
		if gid == "" {
			gid, value = writeback.MyTasks, t.AssigneeSection.Name
		}
		if change, ok := c.change("project_remove", project.GID, ""); ok && change.Kind == "project_remove" {
			continue
		}
		add(project.Name+" section", value, writeback.Change{Kind: "move", Project: gid}, func() tea.Cmd {
			m.input, m.modal = nil, nil
			m.edit = &pendingEdit{ticket: ticket.Ticket{Task: t}}
			return m.pickedEditProject(project)
		})
	}
	for _, f := range t.CustomFields {
		if editable(f) {
			add(ticket.FieldName(f), f.Value(), writeback.Change{Kind: "field", Target: f.GID}, func() tea.Cmd {
				m.input, m.modal = nil, nil
				m.edit = &pendingEdit{ticket: ticket.Ticket{Task: t}}
				return m.pickedField(f)
			})
		}
	}
	branch := m.deps.State.TaskBranches[t.GID]
	branchLabel := "Branch (local)"
	if c.branch != nil {
		branch, branchLabel = *c.branch, "* "+branchLabel
	}
	add(branchLabel, branch, writeback.Change{}, func() tea.Cmd { return m.beginDraftEdit(editBranch) })
	for _, op := range []struct {
		label string
		op    editOp
	}{{"Add to project", editAddProject}, {"Remove from project", editRemoveProject}} {
		items = append(items, pickItem{Label: op.label, Value: func() tea.Cmd { return m.beginDraftEdit(op.op) }})
	}
	for _, change := range c.changes {
		if change.Kind == "project_add" || change.Kind == "project_remove" {
			pending := change
			items = append(items, pickItem{Label: "* " + ticket.OneLine(c.labels[changeKey(change)]), Hint: "enter to undo", Value: func() tea.Cmd {
				c.changes = slices.DeleteFunc(c.changes, func(c writeback.Change) bool { return changeKey(c) == changeKey(pending) })
				m.openBatchReview()
				return nil
			}})
		}
	}
	items = append(items, pickItem{Label: "Comment (optional)", Hint: ticket.OneLine(c.box.area.Value()), Value: func() tea.Cmd {
		m.modal, m.edit, m.input = nil, nil, c.box
		c.footer = -1
		c.box.area.Focus()
		return nil
	}})
	p := newPicker(pickValue(func(run func() tea.Cmd) tea.Cmd { return run() }), "Edit multiple: "+ticket.OneLine(t.Name), items)
	p.extraHints = [][2]string{{keyLabel("input", "submit"), "submit all changes"}}
	p.stopSearch()
	p.err, p.progress = c.box.err, c.box.progress
	p.notice, p.noticeLevel = c.box.notice, statusWarning
	if c.review != nil {
		p.cursor = min(c.review.cursor, len(items)-1)
	}
	c.review, m.modal, m.input, m.edit = p, p, nil, nil
}

func (m *Model) draftLogTime(changes []writeback.Change) bool {
	for _, change := range changes {
		if change.Kind == "assign" && change.Value != "" && change.Value != m.meGID && m.deps.Config.TimeTracking.PromptsAfter(config.PromptAssign) {
			return true
		}
		if change.Kind == "complete" && change.Value == "true" && m.deps.Config.TimeTracking.PromptsAfter(config.PromptComplete) {
			return true
		}
	}
	return false
}

func fieldText(f asana.CustomField) string {
	switch f.ResourceSubtype {
	case asana.FieldEnum:
		if f.EnumValue != nil {
			return f.EnumValue.GID
		}
		return ""
	case asana.FieldMultiEnum:
		var ids []string
		for _, option := range f.MultiEnumValues {
			ids = append(ids, option.GID)
		}
		return strings.Join(ids, ",")
	case asana.FieldPeople:
		var ids []string
		for _, user := range f.PeopleValue {
			ids = append(ids, user.GID)
		}
		return strings.Join(ids, ",")
	case asana.FieldDate:
		if f.DateValue != nil {
			return f.DateValue.Date
		}
		return ""
	}
	return f.Value()
}

func (m *Model) stageField(value string) tea.Cmd {
	f := *m.edit.field
	label := value
	if f.ResourceSubtype == asana.FieldDate && value != "" {
		date, err := dateinput.Resolve(context.Background(), value)
		if err != nil {
			m.draftError(err)
			return nil
		}
		value, label = date, date
	}
	if f.ResourceSubtype != asana.FieldPeople {
		if _, err := writeback.FieldValue(context.Background(), f, value); err != nil {
			m.draftError(err)
			return nil
		}
	}
	if f.ResourceSubtype == asana.FieldEnum || f.ResourceSubtype == asana.FieldMultiEnum {
		var names []string
		for _, id := range strings.Split(value, ",") {
			for _, option := range f.EnumOptions {
				if option.GID == id {
					names = append(names, option.Name)
				}
			}
		}
		label = strings.Join(names, ", ")
	} else if f.ResourceSubtype == asana.FieldPeople {
		var names []string
		for _, id := range strings.Split(value, ",") {
			for _, user := range m.users {
				if user.GID == id {
					names = append(names, user.Name)
				}
			}
		}
		label = strings.Join(names, ", ")
	}
	if label == "" {
		label = "(empty)"
	}
	original := fieldText(f)
	if f.ResourceSubtype == asana.FieldMultiEnum || f.ResourceSubtype == asana.FieldPeople {
		sorted := func(text string) string {
			ids := strings.Split(text, ",")
			slices.Sort(ids)
			return strings.Join(ids, ",")
		}
		value, original = sorted(value), sorted(original)
	}
	return m.stageChange(writeback.Change{Kind: "field", Target: f.GID, Value: value}, label, original)
}

func (m *Model) draftError(err error) {
	if m.input != nil {
		m.input.err = err.Error()
	} else {
		m.composer.box.err = err.Error()
		m.returnToDraft()
	}
}

// applyDraftChanges advances the draft's baseline after a partial submission.
// Successfully saved values must remain visible and be editable again.
func (m *Model) applyDraftChanges(changes []writeback.Change) {
	c := m.composer
	if c.task == nil {
		return
	}
	t := c.task
	for _, change := range changes {
		label := c.labels[changeKey(change)]
		switch change.Kind {
		case "title":
			t.Name = change.Value
		case "description":
			t.HTMLNotes, _ = writeback.CommentHTML(change.Value, m.users, nil)
		case "assign":
			t.Assignee = nil
			if change.Value != "" {
				t.Assignee = &asana.Ref{GID: change.Value, Name: label}
			}
			if change.Value != m.meGID {
				t.AssigneeSection = nil
			}
		case "due":
			t.DueOn = nil
			if change.Value != "" {
				value := change.Value
				t.DueOn = &value
			}
		case "complete":
			t.Completed = change.Value == "true"
		case "move":
			section := &asana.Ref{GID: change.Value, Name: label}
			if change.Project == writeback.MyTasks {
				t.AssigneeSection = section
			} else {
				for i := range t.Memberships {
					if t.Memberships[i].Project.GID == change.Project {
						t.Memberships[i].Section = section
					}
				}
			}
		case "field":
			for i := range t.CustomFields {
				f := &t.CustomFields[i]
				if f.GID != change.Target {
					continue
				}
				display := label
				if change.Value == "" {
					display = ""
				}
				f.DisplayValue = &display
				switch f.ResourceSubtype {
				case asana.FieldEnum:
					f.EnumValue = nil
					for _, option := range f.EnumOptions {
						if option.GID == change.Value {
							f.EnumValue = &option
						}
					}
				case asana.FieldMultiEnum:
					f.MultiEnumValues = nil
					for _, option := range f.EnumOptions {
						if slices.Contains(strings.Split(change.Value, ","), option.GID) {
							f.MultiEnumValues = append(f.MultiEnumValues, option)
						}
					}
				case asana.FieldPeople:
					f.PeopleValue = nil
					for _, id := range strings.Split(change.Value, ",") {
						if id == "" {
							continue
						}
						person := asana.Ref{GID: id, Name: id}
						for _, user := range m.users {
							if user.GID == id {
								person = user
							}
						}
						f.PeopleValue = append(f.PeopleValue, person)
					}
				case asana.FieldDate:
					f.DateValue = nil
					if change.Value != "" {
						f.DateValue = &asana.DateValue{Date: change.Value}
					}
				}
			}
		case "project_add":
			project := asana.Ref{GID: change.Target, Name: change.Target}
			for _, p := range m.projects {
				if p.GID == change.Target {
					project.Name = p.Name
				}
			}
			t.Memberships = append(t.Memberships, asana.Membership{Project: project})
		case "project_remove":
			t.Memberships = slices.DeleteFunc(t.Memberships, func(mb asana.Membership) bool { return mb.Project.GID == change.Target })
		}
	}
	if detail, ok := m.details[c.gid]; ok {
		detail.Task = *t
		m.details[c.gid] = detail
	}
}

func (c *commentComposer) setError(message string) {
	c.box.err = message
	if c.review != nil {
		c.review.err = message
	}
}

// view draws the composer as a modal.
func (c *commentComposer) view(width, height int, look modalLook) string {
	c.box.area.MinHeight = 10
	return look.accent.Render(c.box.title) + "\n" + c.body(width, height-1, look, "cancel")
}

// paneView draws the composer as the reader's bottom pane, whose edge shows
// the title. It starts short and grows with its text.
func (c *commentComposer) paneView(width, height int, look modalLook) string {
	c.box.area.MinHeight = 3
	return c.body(width, height, look, "ticket")
}

// body is the composer below its title, with cancel labelling the cancel key.
func (c *commentComposer) body(width, height int, look modalLook, cancel string) string {
	if c.task == nil {
		return c.box.body(width, height, look, "", c.box.hints(cancel)...)
	}
	accent := look.accent
	from, assignee := "", dimStyle.Render("unassigned")
	if c.task.Assignee != nil {
		from = ticket.OneLine(c.task.Assignee.Name)
		assignee = from
	}
	if change, ok := c.change("assign", "", ""); ok {
		assignee = pendingValue(from, c.labels[changeKey(change)])
	}
	section := dimStyle.Render("unchanged")
	for _, change := range c.changes {
		if change.Kind == "move" {
			from := c.task.SectionIn(change.Project)
			if change.Project == writeback.MyTasks && c.task.AssigneeSection != nil {
				from = c.task.AssigneeSection.Name
			}
			section = pendingValue(ticket.OneLine(from), c.labels[changeKey(change)])
			break
		}
	}
	pending := len(c.changes)
	if c.branch != nil {
		pending++
	}
	w := max(width-2, 1)
	head := dimStyle.Render("── ") + accent.Render("Task updates") + " "
	if pending > 0 {
		head += dimStyle.Render(fmt.Sprintf("%d pending ", pending))
	}
	lines := []string{ansi.Truncate(head+dimStyle.Render(strings.Repeat("─", max(w-ansi.StringWidth(head), 0))), w, "…")}
	for i, field := range [][2]string{{"Assignee", assignee}, {"Section", section}, {"More", dimStyle.Render("all task details…")}} {
		marker, label := "  ", dimStyle.Render(fmt.Sprintf("%-10s", field[0]))
		if c.footer == i {
			marker, label = accent.Render("▌ "), accent.Bold(true).Render(fmt.Sprintf("%-10s", field[0]))
		}
		lines = append(lines, ansi.Truncate(marker+label+field[1], w, "…"))
	}
	hints := [][2]string{{keyLabel("input", "submit"), "submit"}, {keyLabel("input", "paste_image"), "image"}, {keyLabel("input", "save_draft"), "draft"}, {keyLabel("input", "external_editor"), "editor"}, {"tab", "fields"}, {keyLabel("input", "cancel"), cancel}}
	if c.footer >= 0 {
		hints = [][2]string{{"enter", "change"}, {"tab", "next"}, {keyLabel("input", "submit"), "submit"}, {keyLabel("input", "save_draft"), "draft"}, {keyLabel("input", "cancel"), cancel}}
	}
	return c.box.body(width, height, look, strings.Join(lines, "\n"), hints...)
}

// pendingValue shows a staged edit as "old → new ●", or "new ●" without an old value.
func pendingValue(from, to string) string {
	if from == "" {
		return warnStyle.Render(ticket.OneLine(to) + " ●")
	}
	return dimStyle.Render(from+" → ") + warnStyle.Render(ticket.OneLine(to)+" ●")
}
