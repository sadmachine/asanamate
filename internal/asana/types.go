package asana

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
type EnumOption struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// CustomField is a custom field value on a task.
type CustomField struct {
	GID             string       `json:"gid"`
	Name            string       `json:"name"`
	ResourceSubtype string       `json:"resource_subtype"`
	DisplayValue    *string      `json:"display_value"`
	EnumOptions     []EnumOption `json:"enum_options,omitempty"`
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
