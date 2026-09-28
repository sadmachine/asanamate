package tui

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

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
)

// myTasks stands for the My Tasks list among a ticket's projects.
var myTasks = asana.Ref{Name: "My Tasks"}

// pendingEdit tracks a ticket update between opening the edit menu and
// writing it.
type pendingEdit struct {
	ticket  ticket.Ticket
	field   *asana.CustomField // the field being set, once picked
	myTasks bool               // moving within My Tasks, not a project
}

func (m *Model) openEditMenu() {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return
	}
	p := newPicker(pickEdit, "Edit: "+ticket.Clean(t.Name), []pickItem{
		{Label: "Add comment", Key: "c", Value: editComment},
		{Label: "Move to section", Key: "s", Value: editSection},
		{Label: "Set custom field", Key: "f", Value: editField},
		{Label: "Assign", Key: "a", Value: editAssignee},
	})
	p.keySelect = true
	m.modal = p
	m.edit, m.run = &pendingEdit{ticket: t}, nil
}

func (m *Model) pickedEdit(op editOp) tea.Cmd {
	m.modal = nil
	t := m.edit.ticket
	switch op {
	case editComment:
		m.input = newInputBox("Comment on "+ticket.Clean(t.Name), "comment text")
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
			m.modal = newPicker(pickEditProject, "Move within which project?", items)
		}
	case editField:
		m.openFieldPicker()
	case editAssignee:
		return m.requestUsers()
	}
	return nil
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
	m.modal = newPicker(pickSection, "Move to section of "+ticket.Clean(msg.project.Name), items)
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
			items = append(items, pickItem{Label: fieldName(f), Hint: ticket.OneLine(f.Value()), Value: f})
		}
	}
	if len(items) == 0 {
		m.edit, m.status = nil, "ticket has no editable custom fields"
		return
	}
	m.modal = newPicker(pickField, "Set which field?", items)
}

// editable reports whether the edit flow can set custom field f.
func editable(f asana.CustomField) bool {
	switch f.ResourceSubtype {
	case "text", "number", "enum", "multi_enum", "date", "people":
		return true
	}
	return false
}

// commentKey is the field key of the cards view's Comments heading, which
// adds a comment.
const commentKey = "comment"

// fieldTargets are the keys of the rows the cards view can tab to, in the
// order they render: editable details rows, then the Comments heading.
func fieldTargets(t ticket.Ticket) []string {
	var keys []string
	for _, f := range append(t.Meta(), t.FieldValues()...) {
		if f.Key == "" {
			continue
		}
		if gid, ok := strings.CutPrefix(f.Key, "field:"); ok {
			i := slices.IndexFunc(t.CustomFields, func(c asana.CustomField) bool { return c.GID == gid })
			if i < 0 || !editable(t.CustomFields[i]) {
				continue
			}
		}
		keys = append(keys, f.Key)
	}
	return append(keys, commentKey)
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
	case "assignee":
		return m.pickedEdit(editAssignee)
	case commentKey:
		return m.pickedEdit(editComment)
	case "my_tasks":
		return m.pickedEditProject(myTasks)
	case "project":
		for _, mb := range t.Memberships {
			if mb.Project.GID == gid {
				return m.pickedEditProject(mb.Project)
			}
		}
	case "field":
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
	case "enum":
	case "multi_enum":
		items := make([]pickItem, len(f.EnumOptions))
		checked := map[int]bool{}
		for i, o := range f.EnumOptions {
			items[i] = pickItem{Label: ticket.Clean(o.Name), Value: o.GID}
			checked[i] = slices.ContainsFunc(f.MultiEnumValues, func(v asana.EnumOption) bool { return v.GID == o.GID })
		}
		m.modal = newMultiPicker(pickMultiEnum, fieldName(f), items, checked)
		return nil
	case "people":
		return m.requestUsers()
	case "date":
		m.input = newInputBox(fieldName(f), "YYYY-MM-DD; empty clears the field")
		if f.DateValue != nil {
			m.input.area.SetValue(f.DateValue.Date)
		}
		return nil
	default:
		m.input = newInputBox(fieldName(f), "empty clears the field")
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
	m.modal = newPicker(pickEnumOption, fieldName(f), items)
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
	return m.saveEdit("set "+fieldName(f), func(ctx context.Context) error {
		return c.SetCustomField(ctx, gid, f.GID, v)
	})
}

// typedEdit takes the input box's text: a field value or a comment.
func (m *Model) typedEdit(text string) tea.Cmd {
	m.input = nil
	if m.edit.field != nil {
		return m.setField(text)
	}
	if text == "" {
		m.edit, m.status = nil, "comment is empty; nothing posted"
		return nil
	}
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("comment", func(ctx context.Context) error {
		return c.AddComment(ctx, gid, text)
	})
}

// pickedValues sets a multi_enum or people field to the checked option or
// user gids; none checked clears it.
func (m *Model) pickedValues(items []pickItem) tea.Cmd {
	m.modal = nil
	f := *m.edit.field
	gids := make([]string, len(items))
	for i, it := range items {
		gids[i] = it.Value.(string)
	}
	c, gid := m.deps.Client, m.edit.ticket.GID
	return m.saveEdit("set "+fieldName(f), func(ctx context.Context) error {
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
		m.modal = newMultiPicker(pickPeople, fieldName(*f), items, checked)
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
	m.modal = newPicker(pickUser, "Assign to", items)
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

func fieldName(f asana.CustomField) string {
	return ticket.Clean(strings.TrimSpace(f.Name))
}
