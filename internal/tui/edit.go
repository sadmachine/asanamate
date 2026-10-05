package tui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
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
)

// myTasks stands for the My Tasks list among a ticket's projects.
var myTasks = asana.Ref{Name: "My Tasks"}

// pendingEdit tracks a ticket update between opening the edit menu and
// writing it.
type pendingEdit struct {
	ticket  ticket.Ticket
	field   *asana.CustomField // the field being set, once picked
	due     bool               // editing the task's built-in due date
	myTasks bool               // moving within My Tasks, not a project
}

// startEdit begins an edit of the selected ticket, reporting whether its
// details have loaded.
func (m *Model) startEdit() bool {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
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
		{Label: "Move to section", Key: "s", Value: editSection},
		{Label: "Set custom field", Key: "f", Value: editField},
		{Label: "Assign", Key: "a", Value: editAssignee},
		{Label: "Set due date", Key: "d", Value: editDue},
		{Label: "Add to project", Key: "p", Value: editAddProject},
		{Label: "Remove from project", Key: "r", Value: editRemoveProject},
		{Label: "Set branch", Key: "b", Value: editBranch},
	})
	p.keySelect = true
	m.modal = p
}

func (m *Model) pickedEdit(op editOp) tea.Cmd {
	m.modal = nil
	t := m.edit.ticket
	switch op {
	case editComment:
		m.input = newInputBox("Comment on "+ticket.Clean(t.Name), "comment text")
		m.input.area.DynamicHeight = true
		m.input.area.MinHeight = 10
		// Keep the viewport limit separate from the amount of text accepted.
		m.input.area.MaxContentHeight = math.MaxInt
	case editSection:
		targets := m.sectionTargets(t.Task)
		switch len(targets) {
		case 0:
			m.edit, m.status = nil, "ticket is not in any project or your My Tasks"
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
		m.input = newInputBox("Due date", "YYYY-MM-DD; empty clears the date")
		if t.DueOn != nil {
			m.input.area.SetValue(*t.DueOn)
		}
	case editAddProject:
		return m.requestProjects(func() tea.Cmd { m.openAddProjectPicker(); return nil })
	case editRemoveProject:
		m.openRemoveProjectPicker()
	case editBranch:
		m.openBranchInput(t.Task)
	}
	return nil
}

// openBranchInput edits the branch saved for t, which overrides its branch
// field and fallback. Submitting it empty removes the saved branch.
func (m *Model) openBranchInput(t asana.Task) {
	fallback := action.Branch(t, "", m.deps.Config.BranchField, m.projectFields[gidOf(m.viewProject)])
	b := newInputBox("Branch", fmt.Sprintf("empty uses %q", fallback))
	b.area.SetValue(m.deps.State.TaskBranches[t.GID])
	b.onSubmit = func(value string) tea.Cmd {
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
	m.status = "branch saved"
	if branch == "" {
		m.status = "branch cleared"
	}
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving branch: " + err.Error()
	}
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
		m.edit, m.status = nil, "no other member projects available"
		return
	}
	m.modal = newPicker(pickValue(m.addToProject), "Add to which project?", items)
}

func (m *Model) openRemoveProjectPicker() {
	if len(m.edit.ticket.Memberships) == 0 {
		m.edit, m.status = nil, "ticket is not in any project"
		return
	}
	items := make([]pickItem, len(m.edit.ticket.Memberships))
	for i, mb := range m.edit.ticket.Memberships {
		items[i] = pickItem{Label: ticket.Clean(mb.Project.Name), Value: mb.Project}
	}
	m.modal = newPicker(pickValue(m.removeFromProject), "Remove from which project?", items)
}

func (m *Model) addToProject(project asana.Ref) tea.Cmd {
	m.modal = nil
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("add to "+ticket.Clean(project.Name), func(ctx context.Context) error {
		return c.AddToProject(ctx, gid, project.GID)
	})
}

func (m *Model) removeFromProject(project asana.Ref) tea.Cmd {
	m.modal = nil
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("remove from "+ticket.Clean(project.Name), func(ctx context.Context) error {
		return c.RemoveFromProject(ctx, gid, project.GID)
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
	m.edit.myTasks = project.GID == ""
	m.status = "loading sections…"
	return loadSections(m.deps.Client, m.deps.Config.Workspace, project)
}

// requestUsers opens the user picker, first loading the workspace users.
func (m *Model) requestUsers() tea.Cmd {
	if m.users != nil {
		m.openUserPicker(nil)
		return nil
	}
	m.status = "loading people…"
	return loadUsers(m.deps.Client, m.deps.Config.Workspace)
}

func (m *Model) openSectionPicker(msg sectionsMsg) {
	if m.edit == nil {
		return
	}
	if msg.err != nil {
		m.edit, m.status = nil, "loading sections: "+msg.err.Error()
		return
	}
	m.status = ""
	current := m.edit.ticket.SectionIn(msg.project.GID)
	if m.edit.myTasks {
		current = m.edit.ticket.AssigneeSection.Name
	}
	items := make([]pickItem, len(msg.sections))
	for i, sec := range msg.sections {
		items[i] = pickItem{Label: ticket.Clean(sec.Name), Value: sec}
		if sec.Name == current {
			items[i].Hint = "current"
		}
	}
	m.modal = newPicker(pickValue(m.pickedSection), "Move to section of "+ticket.Clean(msg.project.Name), items)
}

func (m *Model) pickedSection(sec asana.Ref) tea.Cmd {
	m.modal = nil
	c, gid, mine := m.deps.Client, m.edit.ticket.GID, m.edit.myTasks
	return m.saveEdit("move to "+ticket.Clean(sec.Name), func(ctx context.Context) error {
		if mine {
			return c.SetMyTasksSection(ctx, gid, sec.GID)
		}
		return c.AddToSection(ctx, sec.GID, gid)
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
		m.edit, m.status = nil, "ticket has no editable custom fields"
		return
	}
	m.modal = newPicker(pickValue(m.pickedField), "Set which field?", items)
}

// editable reports whether the edit flow can set custom field f. ID and
// formula fields are read-only text and number fields.
func editable(f asana.CustomField) bool {
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
	switch f.ResourceSubtype {
	case asana.FieldEnum:
	case asana.FieldMultiEnum:
		items := make([]pickItem, len(f.EnumOptions))
		checked := map[int]bool{}
		for i, o := range f.EnumOptions {
			items[i] = pickItem{Label: ticket.Clean(o.Name), Value: o.GID}
			checked[i] = slices.ContainsFunc(f.MultiEnumValues, func(v asana.EnumOption) bool { return v.GID == o.GID })
		}
		m.modal = newMultiPicker(m.pickedValues, ticket.FieldName(f), items, checked)
		return nil
	case asana.FieldPeople:
		return m.requestUsers()
	case asana.FieldDate:
		m.input = newInputBox(ticket.FieldName(f), "YYYY-MM-DD; empty clears the field")
		if f.DateValue != nil {
			m.input.area.SetValue(f.DateValue.Date)
		}
		return nil
	default:
		m.input = newInputBox(ticket.FieldName(f), "empty clears the field")
		m.input.area.SetValue(f.Value())
		return nil
	}
	items := []pickItem{{Label: "(none)", Hint: "clear", Value: ""}}
	for _, o := range f.EnumOptions {
		it := pickItem{Label: ticket.Clean(o.Name), Value: o.Name}
		if o.Name == f.Value() {
			it.Hint = "current"
		}
		items = append(items, it)
	}
	m.modal = newPicker(pickValue(m.setField), ticket.FieldName(f), items)
	return nil
}

func (m *Model) setField(value string) tea.Cmd {
	m.modal = nil
	f := *m.edit.field
	v, err := writeback.FieldValue(f, value)
	if err != nil {
		m.edit, m.status = nil, err.Error()
		return nil
	}
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("set "+ticket.FieldName(f), func(ctx context.Context) error {
		return c.SetCustomField(ctx, gid, f.GID, v)
	})
}

// typedEdit takes the input box's text: a field value or a comment.
func (m *Model) typedEdit(text string) tea.Cmd {
	m.input = nil
	if m.edit.due {
		if text != "" {
			if _, err := time.Parse(time.DateOnly, text); err != nil {
				m.edit, m.status = nil, "due date needs YYYY-MM-DD"
				return nil
			}
		}
		c, gid := m.deps.Client, m.edit.ticket.GID
		return m.saveEdit("set due date", func(ctx context.Context) error {
			return c.SetDueOn(ctx, gid, text)
		})
	}
	if m.edit.field != nil {
		return m.setField(text)
	}
	if text == "" {
		m.edit, m.status = nil, "comment is empty; nothing posted"
		return nil
	}
	svc := writeback.Service{Client: m.deps.Client, Workspace: m.deps.Config.Workspace, Users: m.users}
	gid := m.edit.ticket.GID
	return m.saveEdit("comment", func(ctx context.Context) error {
		return svc.Comment(ctx, gid, text)
	})
}

// pickedValues sets a multi_enum or people field to the checked option or
// user gids; none checked clears it.
func (m *Model) pickedValues(res pickResult) tea.Cmd {
	m.modal = nil
	f := *m.edit.field
	gids := make([]string, len(res.items))
	for i, it := range res.items {
		gids[i] = it.Value.(string)
	}
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("set "+ticket.FieldName(f), func(ctx context.Context) error {
		return c.SetCustomField(ctx, gid, f.GID, gids)
	})
}

// openUserPicker lists the workspace users once they have loaded: to fill a
// people field when one is being set, otherwise to assign the ticket.
func (m *Model) openUserPicker(err error) {
	if m.edit == nil {
		return
	}
	if err != nil {
		m.edit, m.status = nil, "loading people: "+err.Error()
		return
	}
	m.status = ""
	users := slices.Clone(m.users)
	slices.SortFunc(users, func(a, b asana.Ref) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	if f := m.edit.field; f != nil {
		items := make([]pickItem, len(users))
		checked := map[int]bool{}
		for i, u := range users {
			items[i] = pickItem{Label: ticket.Clean(u.Name), Value: u.GID}
			checked[i] = slices.ContainsFunc(f.PeopleValue, func(v asana.Ref) bool { return v.GID == u.GID })
		}
		m.modal = newMultiPicker(m.pickedValues, ticket.FieldName(*f), items, checked)
		return
	}
	items := []pickItem{{Label: "(unassigned)", Value: asana.Ref{}}}
	for _, u := range users {
		items = append(items, pickItem{Label: ticket.Clean(u.Name), Value: u})
	}
	if a := m.edit.ticket.Assignee; a != nil {
		for i := range items {
			if items[i].Value.(asana.Ref).GID == a.GID {
				items[i].Hint = "current"
			}
		}
	} else {
		items[0].Hint = "current"
	}
	m.modal = newPicker(pickValue(m.pickedUser), "Assign to", items)
}

func (m *Model) pickedUser(u asana.Ref) tea.Cmd {
	m.modal = nil
	what := "unassign"
	if u.GID != "" {
		what = "assign to " + ticket.Clean(u.Name)
	}
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit(what, func(ctx context.Context) error {
		return c.SetAssignee(ctx, gid, u.GID)
	})
}

func (m *Model) saveEdit(what string, write func(context.Context) error) tea.Cmd {
	gid := m.edit.ticket.GID
	m.edit, m.status = nil, what+"…"
	return saveEdit(gid, what, write)
}
