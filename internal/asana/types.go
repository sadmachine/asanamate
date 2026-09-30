package asana

import "strings"

// Ref is a compact Asana object: a gid and a display name.
type Ref struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// Membership places a task in a project section.
type Membership struct {
	Project Ref  `json:"project"`
	Section *Ref `json:"section,omitempty"`
}

// EnumOption is one choice of an enum custom field.
type EnumOption = Ref

// Custom field types (resource_subtype values).
const (
	FieldText      = "text"
	FieldNumber    = "number"
	FieldEnum      = "enum"
	FieldMultiEnum = "multi_enum"
	FieldDate      = "date"
	FieldPeople    = "people"
)

// CustomField is a custom field value on a task.
type CustomField struct {
	GID                string       `json:"gid"`
	Name               string       `json:"name"`
	ResourceSubtype    string       `json:"resource_subtype"`
	RepresentationType string       `json:"representation_type,omitempty"` // refines ResourceSubtype: custom_id, formula
	DisplayValue       *string      `json:"display_value"`
	EnumOptions        []EnumOption `json:"enum_options,omitempty"`
	MultiEnumValues    []EnumOption `json:"multi_enum_values,omitempty"`
	PeopleValue        []Ref        `json:"people_value,omitempty"`
	DateValue          *DateValue   `json:"date_value,omitempty"`
}

// DateValue is a date custom field's value.
type DateValue struct {
	Date string `json:"date"` // YYYY-MM-DD
}

// Task is an Asana task. List endpoints fill a subset of the fields.
type Task struct {
	GID             string        `json:"gid"`
	Name            string        `json:"name"`
	Completed       bool          `json:"completed"`
	DueOn           *string       `json:"due_on,omitempty"`
	Assignee        *Ref          `json:"assignee,omitempty"`
	AssigneeSection *Ref          `json:"assignee_section,omitempty"`
	Memberships     []Membership  `json:"memberships,omitempty"`
	Tags            []Ref         `json:"tags,omitempty"`
	PermalinkURL    string        `json:"permalink_url,omitempty"`
	HTMLNotes       string        `json:"html_notes,omitempty"`
	CustomFields    []CustomField `json:"custom_fields,omitempty"`
	Dependencies    []Ref         `json:"dependencies,omitempty"`
	Dependents      []Ref         `json:"dependents,omitempty"`
	Parent          *Ref          `json:"parent,omitempty"`
	CreatedAt       string        `json:"created_at,omitempty"`
	ModifiedAt      string        `json:"modified_at,omitempty"`
}

// Status is "done" for completed tasks, else "open".
func (t Task) Status() string {
	if t.Completed {
		return "done"
	}
	return "open"
}

// SectionIn returns the task's section name in the given project, or "".
func (t Task) SectionIn(projectGID string) string {
	for _, m := range t.Memberships {
		if m.Project.GID == projectGID && m.Section != nil {
			return m.Section.Name
		}
	}
	return ""
}

// SectionFor returns the section shown for the task in a view: the My Tasks
// section when projectGID is empty, otherwise the section in that project.
func (t Task) SectionFor(projectGID string) string {
	if projectGID != "" {
		return t.SectionIn(projectGID)
	}
	if t.AssigneeSection != nil {
		return t.AssigneeSection.Name
	}
	return ""
}

// Field returns the task's custom field with the given name, ignoring case and
// surrounding spaces. Fields are separate objects that may share a name (for
// example one per project); see PickField for which one wins.
func (t Task) Field(name string, preferred map[string]bool) (CustomField, bool) {
	matches := t.FieldsNamed(name)
	if len(matches) == 0 {
		return CustomField{}, false
	}
	return PickField(matches, preferred), true
}

// FieldsNamed returns the task's custom fields with the given name, ignoring
// case and surrounding spaces, in task order.
func (t Task) FieldsNamed(name string) []CustomField {
	var matches []CustomField
	for _, f := range t.CustomFields {
		if SameFieldName(f.Name, name) {
			matches = append(matches, f)
		}
	}
	return matches
}

// PickField chooses among same-named fields: one whose gid is in preferred
// (the fields of the active project), else the first with a value, else the
// first.
func PickField(fields []CustomField, preferred map[string]bool) CustomField {
	for _, f := range fields {
		if preferred[f.GID] {
			return f
		}
	}
	for _, f := range fields {
		if f.Value() != "" {
			return f
		}
	}
	return fields[0]
}

// SameFieldName compares custom field names, ignoring case and surrounding spaces.
func SameFieldName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Value returns the field's display value, or "" when unset.
func (f CustomField) Value() string {
	if f.DisplayValue == nil {
		return ""
	}
	return *f.DisplayValue
}

// Names returns the refs' names, in order.
func Names(refs []Ref) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names
}

// Story is an entry in a task's activity feed; comments have Type "comment".
type Story struct {
	GID       string `json:"gid"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	CreatedBy *Ref   `json:"created_by,omitempty"`
	HTMLText  string `json:"html_text"`
}

// Attachment is a file attached to a task.
type Attachment struct {
	GID          string  `json:"gid"`
	Name         string  `json:"name"`
	Host         string  `json:"host"`
	DownloadURL  *string `json:"download_url,omitempty"`
	PermanentURL string  `json:"permanent_url,omitempty"`
	ViewURL      string  `json:"view_url,omitempty"`
}

// User is the authenticated user.
type User struct {
	GID        string `json:"gid"`
	Name       string `json:"name"`
	Workspaces []Ref  `json:"workspaces"`
}

// Project is an Asana project with its members.
type Project struct {
	GID     string `json:"gid"`
	Name    string `json:"name"`
	Members []Ref  `json:"members"`
}
