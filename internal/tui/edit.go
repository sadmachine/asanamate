package tui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/dateinput"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/writeback"
)

type editOp int

const (
	editComment editOp = iota
	editSection
	editField
	editAssignee
	editDue
	editAddProject
	editRemoveProject
	editBranch
	editComplete
	editMultiple
	editTitle
	editDescription
)

// myTasks stands for the My Tasks list among a ticket's projects.
var myTasks = asana.Ref{Name: "My Tasks"}

// pendingEdit tracks a ticket update between opening the edit menu and
// writing it.
type pendingEdit struct {
	ticket  ticket.Ticket
	field   *asana.CustomField // the field being set, once picked
	due     bool               // editing the task's built-in due date
	project string             // the gid of the project to move within, or writeback.MyTasks
}

// writer is the TUI's writeback service. It has no Confirm: choosing a value
// in the TUI is the go-ahead.
func (m *Model) writer() writeback.Service {
	return writeback.Service{Client: m.deps.Client, Workspace: m.deps.Config.Workspace, Users: m.users}
}

// startEdit begins an edit of the selected ticket, reporting whether its
// details have loaded.
func (m *Model) startEdit() bool {
	t, ok := m.selectedDetail()
	if !ok {
		m.setStatus(statusWarning, "ticket details are still loading")
		return false
	}
	m.edit, m.run = &pendingEdit{ticket: t}, nil
	return true
}

// openEdit runs op on the selected ticket without the edit menu.
func (m *Model) openEdit(op editOp) tea.Cmd {
	if !m.startEdit() {
		return nil
	}
	return m.pickedEdit(op)
}

func (m *Model) openEditMenu() {
	if !m.startEdit() {
		return
	}
	t := m.edit.ticket
	p := newPicker(pickValue(m.pickedEdit), "Edit: "+ticket.Clean(t.Name), []pickItem{
		{Label: "Add comment", Key: "c", Value: editComment},
		{Label: "Edit title", Key: "t", Value: editTitle},
		{Label: "Edit description", Key: "n", Value: editDescription},
		{Label: "Move to section", Key: "s", Value: editSection},
		{Label: "Set custom field", Key: "f", Value: editField},
		{Label: "Assign", Key: "a", Value: editAssignee},
		{Label: "Set due date", Key: "d", Value: editDue},
		{Label: "Add to project", Key: "p", Value: editAddProject},
		{Label: "Remove from project", Key: "r", Value: editRemoveProject},
		{Label: "Set branch", Key: "b", Value: editBranch},
		{Label: completeLabel(t.Task), Key: "x", Value: editComplete},
		{Label: "Edit multiple", Key: "m", Value: editMultiple},
	})
	p.keySelect = true
	m.modal = p
}

func (m *Model) pickedEdit(op editOp) tea.Cmd {
	m.modal = nil
	t := m.edit.ticket
	switch op {
	case editTitle, editDescription:
		m.openTextEdit(op)
	case editComment:
		m.openCommentComposer(t.Task)
	case editMultiple:
		m.openCommentComposer(t.Task)
		m.openBatchReview()
	case editSection:
		targets := m.sectionTargets(t.Task)
		switch len(targets) {
		case 0:
			m.edit = nil
			m.setStatus(statusWarning, "ticket is not in any project or your My Tasks")
			if m.composer != nil {
				m.returnToDraft()
			}
		case 1:
			return m.pickedEditProject(targets[0])
		default:
			items := make([]pickItem, len(targets))
			for i, p := range targets {
				items[i] = pickItem{Label: ticket.Clean(p.Name), Value: p}
			}
			m.modal = newPicker(pickValue(m.pickedEditProject), "Move within which project?", items)
		}
	case editField:
		m.openFieldPicker()
	case editAssignee:
		return m.requestUsers()
	case editDue:
		m.edit.due = true
		m.input = newInputBox("Due date", "YYYY-MM-DD or date -d text, such as +1 day; empty clears the date")
		if t.DueOn != nil {
			m.input.area.SetValue(*t.DueOn)
		}
		if c := m.composer; c != nil {
			m.input.area.SetValue(c.value("due", "", "", m.input.area.Value()))
		}
	case editAddProject:
		return m.requestProjects(func() tea.Cmd { m.openAddProjectPicker(); return nil })
	case editRemoveProject:
		m.openRemoveProjectPicker()
	case editBranch:
		m.openBranchInput(t.Task)
	case editComplete:
		return m.toggleComplete()
	}
	return nil
}

// openTextEdit edits the existing title or Markdown description. Unchanged
// text closes without writing, preserving the original rich text in Asana.
func (m *Model) openTextEdit(op editOp) {
	t := m.edit.ticket
	kind, label, original := "title", "Title", t.Name
	if op == editDescription {
		kind, label, original = "description", "Description", t.DescriptionMarkdown()
	}
	b := newInputBox(label, "Markdown; empty clears the description")
	b.area.CharLimit = 0
	b.area.MaxContentHeight = math.MaxInt
	if op == editTitle {
		b.area.Placeholder = "title"
		b.oneLine = true
		b.area.KeyMap.InsertNewline.SetEnabled(false)
	}
	value := original
	if c := m.composer; c != nil {
		value = c.value(kind, "", "", original)
	}
	b.area.SetValue(value)
	if b.area.Value() != value {
		b.err = "Editor changes tabs/control characters. Review before submitting."
	}
	b.onSubmit = func(value string) tea.Cmd {
		if op == editTitle {
			if value == "" || strings.ContainsAny(value, "\r\n") {
				b.err = "title must be nonempty and one line"
				return nil
			}
		} else if len(writeback.LocalImages(value)) > 0 {
			b.err = "description image uploads are not supported; use links"
			return nil
		}
		if m.composer != nil {
			return m.stageChange(writeback.Change{Kind: kind, Value: value}, value, strings.TrimSpace(original))
		}
		m.input = nil
		if value == strings.TrimSpace(original) {
			m.edit = nil
			m.setStatus(statusInfo, "unchanged")
			return nil
		}
		svc := m.writer()
		return m.saveEdit("set "+kind, func(ctx context.Context) error {
			if op == editTitle {
				return svc.SetTitle(ctx, t.GID, value)
			}
			return svc.SetDescription(ctx, t.GID, value)
		})
	}
	m.input = b
}

// completeLabel names the edit that toggles t: Complete when open, Reopen
// when complete.
func completeLabel(t asana.Task) string {
	if t.Completed {
		return "Reopen"
	}
	return "Complete"
}

// toggleComplete completes the ticket being edited, or reopens it when it is
// complete.
func (m *Model) toggleComplete() tea.Cmd {
	t := m.edit.ticket
	if c := m.composer; c != nil {
		done := c.value("complete", "", "", strconv.FormatBool(t.Completed)) != "true"
		return m.stageChange(writeback.Change{Kind: "complete", Value: strconv.FormatBool(done)}, strconv.FormatBool(done), strconv.FormatBool(t.Completed))
	}
	svc, gid, done := m.writer(), t.GID, !t.Completed
	logTime := done && m.deps.Config.TimeTracking.PromptsAfter(config.PromptComplete)
	what, status := "reopen "+ticket.Clean(t.Name), "Reopened "+ticket.Clean(t.Name)
	if done {
		what, status = "complete "+ticket.Clean(t.Name), "Completed "+ticket.Clean(t.Name)
	}
	m.edit = nil
	m.setProgress(what + "…")
	return request(func(ctx context.Context) tea.Msg {
		return editDoneMsg{gid: gid, what: what, done: status, logTime: logTime, err: svc.Complete(ctx, gid, done)}
	})
}

// openBranchInput edits the branch saved for t, which overrides its branch
// field and fallback. Submitting it empty removes the saved branch.
func (m *Model) openBranchInput(t asana.Task) {
	fallback := action.Branch(t, "", m.deps.Config.BranchField, m.projectFields[gidOf(m.viewProject)])
	b := newInputBox("Branch", fmt.Sprintf("empty uses %q", fallback))
	b.area.SetValue(m.deps.State.TaskBranches[t.GID])
	if c := m.composer; c != nil && c.branch != nil {
		b.area.SetValue(*c.branch)
	}
	b.onSubmit = func(value string) tea.Cmd {
		if c := m.composer; c != nil {
			c.branch = &value
			if value == m.deps.State.TaskBranches[t.GID] {
				c.branch = nil
			}
			m.returnToDraft()
			return nil
		}
		m.input, m.edit = nil, nil
		m.saveBranch(t.GID, value)
		return nil
	}
	m.input = b
}

// saveBranch saves branch for the task, or removes its saved branch when
// branch is "", and relinks agents.
func (m *Model) saveBranch(gid, branch string) {
	m.deps.State.SetTaskBranch(gid, branch)
	m.linked = nil
	m.renderDetail(true)
	if err := m.deps.State.Save(); err != nil {
		m.setStatus(statusError, "saving branch: "+err.Error())
		return
	}
	message := "branch saved"
	if branch == "" {
		message = "branch cleared"
	}
	m.setStatus(statusInfo, message)
}

func (m *Model) openAddProjectPicker() {
	if m.edit == nil {
		return
	}
	memberOf := make(map[string]bool, len(m.edit.ticket.Memberships))
	for _, mb := range m.edit.ticket.Memberships {
		memberOf[mb.Project.GID] = true
	}
	var items []pickItem
	for _, p := range m.projects {
		if !memberOf[p.GID] {
			items = append(items, pickItem{Label: ticket.Clean(p.Name), Value: asana.Ref{GID: p.GID, Name: p.Name}})
		}
	}
	if len(items) == 0 {
		m.edit = nil
		m.setStatus(statusWarning, "no other member projects available")
		if m.composer != nil {
			m.returnToDraft()
		}
		return
	}
	m.modal = newPicker(pickValue(m.addToProject), "Add to which project?", items)
}

func (m *Model) openRemoveProjectPicker() {
	if len(m.edit.ticket.Memberships) == 0 {
		m.edit = nil
		m.setStatus(statusWarning, "ticket is not in any project")
		if m.composer != nil {
			m.returnToDraft()
		}
		return
	}
	items := make([]pickItem, len(m.edit.ticket.Memberships))
	for i, mb := range m.edit.ticket.Memberships {
		items[i] = pickItem{Label: ticket.Clean(mb.Project.Name), Value: mb.Project}
	}
	m.modal = newPicker(pickValue(m.removeFromProject), "Remove from which project?", items)
}

func (m *Model) addToProject(project asana.Ref) tea.Cmd {
	if m.composer != nil {
		return m.stageChange(writeback.Change{Kind: "project_add", Target: project.GID, Value: "add"}, "Add to "+ticket.Clean(project.Name), "")
	}
	m.modal = nil
	svc, gid := m.writer(), m.edit.ticket.GID
	return m.saveEdit("add to "+ticket.Clean(project.Name), func(ctx context.Context) error {
		return svc.AddProject(ctx, gid, project.GID)
	})
}

func (m *Model) removeFromProject(project asana.Ref) tea.Cmd {
	if c := m.composer; c != nil {
		c.changes = slices.DeleteFunc(c.changes, func(change writeback.Change) bool { return change.Kind == "move" && change.Project == project.GID })
		return m.stageChange(writeback.Change{Kind: "project_remove", Target: project.GID, Value: "remove"}, "Remove from "+ticket.Clean(project.Name), "")
	}
	m.modal = nil
	svc, gid := m.writer(), m.edit.ticket.GID
	return m.saveEdit("remove from "+ticket.Clean(project.Name), func(ctx context.Context) error {
		return svc.RemoveProject(ctx, gid, project.GID)
	})
}

// sectionTargets are the ticket's projects plus My Tasks when the ticket is
// yours (Asana only returns its My Tasks section to its assignee). My Tasks
// comes first while viewing it.
func (m *Model) sectionTargets(t asana.Task) []asana.Ref {
	var targets []asana.Ref
	for _, mb := range t.Memberships {
		targets = append(targets, mb.Project)
	}
	if t.AssigneeSection == nil {
		return targets
	}
	if m.viewProject == nil {
		return append([]asana.Ref{myTasks}, targets...)
	}
	return append(targets, myTasks)
}

func (m *Model) pickedEditProject(project asana.Ref) tea.Cmd {
	m.modal = nil
	m.edit.project = project.GID
	if project.GID == "" {
		m.edit.project = writeback.MyTasks
	}
	m.setProgress("loading sections…")
	return loadSections(m.deps.Client, m.deps.Config.Workspace, project)
}

// requestUsers opens the user picker, first loading the workspace users.
func (m *Model) requestUsers() tea.Cmd {
	if m.users != nil {
		m.openUserPicker(nil)
		return nil
	}
	m.setProgress("loading people…")
	return loadUsers(m.deps.Client, m.deps.Config.Workspace)
}

func (m *Model) openSectionPicker(msg sectionsMsg) {
	if m.edit == nil {
		return
	}
	project := msg.project.GID
	if project == "" {
		project = writeback.MyTasks
	}
	if m.composer != nil && m.edit.project != project {
		return
	}
	if msg.err != nil {
		m.edit = nil
		if m.composer != nil {
			m.setInlineError("loading sections: " + msg.err.Error())
			m.composer.box.err = m.status
			m.returnToDraft()
		} else {
			m.setStatus(statusError, "loading sections: "+msg.err.Error())
		}
		return
	}
	m.clearProgress()
	current := ""
	if m.edit.project == writeback.MyTasks {
		current = m.edit.ticket.AssigneeSection.GID
	} else {
		for _, mb := range m.edit.ticket.Memberships {
			if mb.Project.GID == project && mb.Section != nil {
				current = mb.Section.GID
			}
		}
	}
	if c := m.composer; c != nil {
		current = c.value("move", "", project, current)
	}
	items := make([]pickItem, len(msg.sections))
	for i, sec := range msg.sections {
		items[i] = pickItem{Label: ticket.Clean(sec.Name), Value: sec}
		if sec.GID == current {
			items[i].Hint = "current"
		}
	}
	m.modal = newPicker(pickValue(m.pickedSection), "Move to section of "+ticket.Clean(msg.project.Name), items)
}

func (m *Model) pickedSection(sec asana.Ref) tea.Cmd {
	if m.composer != nil {
		t, project := m.edit.ticket, m.edit.project
		original := ""
		if project == writeback.MyTasks && t.AssigneeSection != nil {
			original = t.AssigneeSection.GID
		} else {
			for _, mb := range t.Memberships {
				if mb.Project.GID == project && mb.Section != nil {
					original = mb.Section.GID
				}
			}
		}
		return m.stageChange(writeback.Change{Kind: "move", Project: project, Value: sec.GID}, ticket.Clean(sec.Name), original)
	}
	m.modal = nil
	svc, gid, project := m.writer(), m.edit.ticket.GID, m.edit.project
	return m.saveEdit("move to "+ticket.Clean(sec.Name), func(ctx context.Context) error {
		return svc.Move(ctx, gid, sec.GID, project)
	})
}

// openFieldPicker lists the custom fields the edit menu can set.
func (m *Model) openFieldPicker() {
	var items []pickItem
	for _, f := range m.edit.ticket.CustomFields {
		if editable(f) {
			items = append(items, pickItem{Label: ticket.FieldName(f), Hint: ticket.OneLine(f.Value()), Value: f})
		}
	}
	if len(items) == 0 {
		m.edit = nil
		m.setStatus(statusWarning, "ticket has no editable custom fields")
		return
	}
	m.modal = newPicker(pickValue(m.pickedField), "Set which field?", items)
}

// editable reports whether the edit flow can set custom field f. ID and
// formula fields are read-only text and number fields.
func editable(f asana.CustomField) bool {
	if f.IsValueReadOnly || f.IsFormulaField {
		return false
	}
	kind := f.RepresentationType
	if kind == "" {
		kind = f.ResourceSubtype
	}
	switch kind {
	case asana.FieldText, asana.FieldNumber, asana.FieldEnum, asana.FieldMultiEnum, asana.FieldDate, asana.FieldPeople:
		return true
	}
	return false
}

// commentKey is the field key of the cards view's Add comment row.
const commentKey = "comment"

// addProjectKey is the field key of the cards view's Add project row.
const addProjectKey = "add_project"

// emptyFieldsKey is the field key of the cards view's Show empty fields row.
const emptyFieldsKey = "empty_fields"

// otherAgentsKey is the field key of the cards view's Show agents on other
// branches row.
const otherAgentsKey = "other_agents"

// agentsKey is the field key of the cards view's Agents section; enter on
// it opens the agents modal.
const agentsKey = "section:agents"

// fieldTargets are the keys of the rows the cards view can tab to, in the
// order they render: editable details rows, the Show empty fields row while
// empty fields are hidden, then the Add comment row.
func (m *Model) fieldTargets(t ticket.Ticket) []string {
	var keys []string
	rows := append(detailMeta(t), t.FieldValues()...)
	if m.showEmpty {
		rows = append(rows, t.EmptyFields()...)
	}
	for _, f := range rows {
		if f.Key == "" {
			continue
		}
		if gid, ok := strings.CutPrefix(f.Key, ticket.FieldKey("")); ok {
			i := slices.IndexFunc(t.CustomFields, func(c asana.CustomField) bool { return c.GID == gid })
			if i < 0 || !editable(t.CustomFields[i]) {
				continue
			}
		}
		keys = append(keys, f.Key)
	}
	if !m.showEmpty && len(t.EmptyFields()) > 0 {
		keys = append(keys, emptyFieldsKey)
	}
	return append(keys, commentKey)
}

// allCommentsKey is the field key of the cards view's Show all comments row.
const allCommentsKey = "all_comments"

// showAllComments reveals the selected ticket's older comments and selects
// the oldest.
func (m *Model) showAllComments() {
	t, ok := m.selectedDetail()
	if !ok || len(t.Comments) == 0 {
		return
	}
	m.showComments, m.fieldKey = true, commentTarget(t.Comments[0], 0)
	m.showTarget()
}

// openComment shows the comment with key in full instead of its preview.
func (m *Model) openComment(key string) {
	if m.openComments == nil {
		m.openComments = map[string]bool{}
	}
	m.openComments[key] = true
	m.showTarget()
}

// showEmptyFields reveals the selected ticket's empty custom fields and
// selects the first editable one.
func (m *Model) showEmptyFields() {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	m.showEmpty, m.fieldKey = true, ""
	for _, f := range t.EmptyFields() {
		if slices.Contains(m.fieldTargets(t), f.Key) {
			m.fieldKey = f.Key
			break
		}
	}
	m.showTarget()
}

// showOtherAgents reveals the selected ticket's agents on other branches and
// selects the Agents section.
func (m *Model) showOtherAgents() {
	m.showOthers, m.fieldKey = true, agentsKey
	m.showTarget()
}

// openField starts editing the row with key on the selected ticket, skipping
// the edit menu.
func (m *Model) openField(key string) tea.Cmd {
	t, ok := m.selectedDetail()
	if !ok {
		return nil
	}
	m.edit, m.run = &pendingEdit{ticket: t}, nil
	kind, gid, _ := strings.Cut(key, ":")
	switch kind {
	case ticket.KeyAssignee:
		return m.pickedEdit(editAssignee)
	case ticket.KeyDue:
		return m.pickedEdit(editDue)
	case commentKey:
		return m.pickedEdit(editComment)
	case addProjectKey:
		return m.pickedEdit(editAddProject)
	case ticket.KeyMyTasks:
		return m.pickedEditProject(myTasks)
	case ticket.KeyProject:
		for _, mb := range t.Memberships {
			if mb.Project.GID == gid {
				return m.pickedEditProject(mb.Project)
			}
		}
	case ticket.KeyField:
		for _, f := range t.CustomFields {
			if f.GID == gid {
				return m.pickedField(f)
			}
		}
	}
	m.edit = nil
	return nil
}

func (m *Model) pickedField(f asana.CustomField) tea.Cmd {
	m.modal = nil
	m.edit.field = &f
	value := fieldText(f)
	if c := m.composer; c != nil {
		value = c.value("field", f.GID, "", value)
	}
	switch f.ResourceSubtype {
	case asana.FieldEnum:
	case asana.FieldMultiEnum:
		items := make([]pickItem, len(f.EnumOptions))
		checked := map[int]bool{}
		for i, o := range f.EnumOptions {
			items[i] = pickItem{Label: ticket.Clean(o.Name), Value: o.GID}
			checked[i] = slices.Contains(strings.Split(value, ","), o.GID)
		}
		m.modal = newMultiPicker(m.pickedValues, ticket.FieldName(f), items, checked)
		return nil
	case asana.FieldPeople:
		return m.requestUsers()
	case asana.FieldDate:
		m.input = newInputBox(ticket.FieldName(f), "YYYY-MM-DD or date -d text, such as +1 day; empty clears the field")
		m.input.area.SetValue(value)
		return nil
	default:
		m.input = newInputBox(ticket.FieldName(f), "empty clears the field")
		m.input.area.SetValue(value)
		return nil
	}
	items := []pickItem{{Label: "(none)", Hint: "clear", Value: ""}}
	for _, o := range f.EnumOptions {
		it := pickItem{Label: ticket.Clean(o.Name), Value: o.GID}
		if o.GID == value {
			it.Hint = "current"
		}
		items = append(items, it)
	}
	m.modal = newPicker(pickValue(m.setField), ticket.FieldName(f), items)
	return nil
}

// setField sets the field being edited to value, as writeback.FieldValue
// reads it.
func (m *Model) setField(value string) tea.Cmd {
	if m.composer != nil {
		return m.stageField(value)
	}
	m.modal = nil
	svc, gid, f := m.writer(), m.edit.ticket.GID, *m.edit.field
	return m.saveEdit("set "+ticket.FieldName(f), func(ctx context.Context) error {
		return svc.SetField(ctx, gid, f.GID, value, "")
	})
}

// typedEdit takes the input box's text for a due date or custom field.
func (m *Model) typedEdit(text string) tea.Cmd {
	if m.edit.due {
		if m.composer != nil {
			date := strings.TrimSpace(text)
			if date != "" {
				var err error
				date, err = dateinput.Resolve(context.Background(), date)
				if err != nil {
					m.input.err = err.Error()
					return nil
				}
			}
			original := ""
			if m.edit.ticket.DueOn != nil {
				original = *m.edit.ticket.DueOn
			}
			label := date
			if label == "" {
				label = "(empty)"
			}
			return m.stageChange(writeback.Change{Kind: "due", Value: date}, label, original)
		}
		m.input = nil
		svc, gid := m.writer(), m.edit.ticket.GID
		return m.saveEdit("set due date", func(ctx context.Context) error {
			return svc.SetDue(ctx, gid, text)
		})
	}
	if m.edit.field != nil {
		if m.composer == nil {
			m.input = nil
		}
		return m.setField(text)
	}
	return nil
}

// pickedValues sets a multi_enum or people field to the checked option or
// user gids; none checked clears it.
func (m *Model) pickedValues(res pickResult) tea.Cmd {
	gids := make([]string, len(res.items))
	for i, it := range res.items {
		gids[i] = it.Value.(string)
	}
	return m.setField(strings.Join(gids, ","))
}

// openUserPicker lists the workspace users once they have loaded: to fill a
// people field when one is being set, otherwise to assign the ticket.
func (m *Model) openUserPicker(err error) {
	if m.edit == nil {
		return
	}
	if err != nil {
		m.edit = nil
		if m.composer != nil {
			m.setInlineError("loading people: " + err.Error())
			m.composer.box.err = m.status
			m.returnToDraft()
		} else {
			m.setStatus(statusError, "loading people: "+err.Error())
		}
		return
	}
	m.clearProgress()
	users := slices.Clone(m.users)
	slices.SortFunc(users, func(a, b asana.Ref) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	if f := m.edit.field; f != nil {
		items := make([]pickItem, len(users))
		checked := map[int]bool{}
		value := fieldText(*f)
		if c := m.composer; c != nil {
			value = c.value("field", f.GID, "", value)
		}
		for i, u := range users {
			items[i] = pickItem{Label: ticket.Clean(u.Name), Value: u.GID}
			checked[i] = slices.Contains(strings.Split(value, ","), u.GID)
		}
		m.modal = newMultiPicker(m.pickedValues, ticket.FieldName(*f), items, checked)
		return
	}
	items := []pickItem{{Label: "(unassigned)", Value: asana.Ref{}}}
	for _, u := range users {
		items = append(items, pickItem{Label: ticket.Clean(u.Name), Value: u})
	}
	current := ""
	if a := m.edit.ticket.Assignee; a != nil {
		current = a.GID
	}
	if c := m.composer; c != nil {
		current = c.value("assign", "", "", current)
	}
	if current != "" {
		for i := range items {
			if items[i].Value.(asana.Ref).GID == current {
				items[i].Hint = "current"
			}
		}
	} else {
		items[0].Hint = "current"
	}
	m.modal = newPicker(pickValue(m.pickedUser), "Assign to", items)
}

func (m *Model) pickedUser(u asana.Ref) tea.Cmd {
	if m.composer != nil {
		original := ""
		if a := m.edit.ticket.Assignee; a != nil {
			original = a.GID
		}
		label := ticket.Clean(u.Name)
		if u.GID == "" {
			label = "(unassigned)"
		}
		return m.stageChange(writeback.Change{Kind: "assign", Value: u.GID}, label, original)
	}
	m.modal = nil
	what := "unassign"
	if u.GID != "" {
		what = "assign to " + ticket.Clean(u.Name)
	}
	svc, gid := m.writer(), m.edit.ticket.GID
	logTime := u.GID != "" && u.GID != m.meGID && m.deps.Config.TimeTracking.PromptsAfter(config.PromptAssign)
	m.edit = nil
	m.setProgress(what + "…")
	return request(func(ctx context.Context) tea.Msg {
		return editDoneMsg{gid: gid, what: what, logTime: logTime, err: svc.Assign(ctx, gid, u.GID)}
	})
}

func (m *Model) saveEdit(what string, write func(context.Context) error) tea.Cmd {
	gid := m.edit.ticket.GID
	m.edit = nil
	m.setProgress(what + "…")
	return saveEdit(gid, what, write)
}
