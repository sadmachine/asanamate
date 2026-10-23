package asana

import (
	"bytes"
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"time"
)

const (
	listFields       = "name,completed,due_on,modified_at,assignee.name,assignee_section.name,memberships.project.name,memberships.project.color,memberships.section.name,tags.name,permalink_url,custom_fields.name,custom_fields.display_value,custom_fields.is_value_read_only,custom_fields.is_formula_field,custom_fields.enum_value.color"
	detailFields     = listFields + ",html_notes,custom_fields.resource_subtype,custom_fields.representation_type,custom_fields.enum_options.name,custom_fields.multi_enum_values.name,custom_fields.people_value.name,custom_fields.date_value.date,dependencies.name,dependents.name,parent.name,created_at"
	attachmentFields = "name,host,download_url,permanent_url,view_url"
)

func fields(f string) url.Values { return url.Values{"opt_fields": {f}} }

// Me returns the authenticated user and their workspaces.
func (c *Client) Me(ctx context.Context) (User, error) {
	return getOne[User](ctx, c, "/users/me", fields("name,workspaces.name"))
}

// MyTaskList returns the user's My Tasks list. Project endpoints such as
// Sections accept its gid.
func (c *Client) MyTaskList(ctx context.Context, workspace string) (Ref, error) {
	return getOne[Ref](ctx, c, "/users/me/user_task_list", url.Values{"workspace": {workspace}})
}

// MyTasks returns the user's My Tasks list: incomplete tasks plus tasks completed after since.
func (c *Client) MyTasks(ctx context.Context, workspace string, since time.Time) ([]Task, error) {
	list, err := c.MyTaskList(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return c.tasksIn(ctx, "/user_task_lists/"+list.GID+"/tasks", since)
}

// ProjectTasks returns a project's incomplete tasks plus tasks completed after since.
func (c *Client) ProjectTasks(ctx context.Context, projectGID string, since time.Time) ([]Task, error) {
	return c.tasksIn(ctx, "/projects/"+projectGID+"/tasks", since)
}

func (c *Client) tasksIn(ctx context.Context, path string, since time.Time) ([]Task, error) {
	q := fields(listFields)
	q.Set("completed_since", since.UTC().Format(time.RFC3339))
	return getAll[Task](ctx, c, path, q)
}

// Projects returns all accessible, unarchived projects in workspace, including
// projects the authenticated user has not joined.
func (c *Client) Projects(ctx context.Context, workspace string) ([]Project, error) {
	q := fields("name,color,members.gid")
	q.Set("workspace", workspace)
	q.Set("archived", "false")
	return getAll[Project](ctx, c, "/projects", q)
}

// Project returns one project's name, including projects the user is not a
// member of and archived ones.
func (c *Client) Project(ctx context.Context, gid string) (Ref, error) {
	return getOne[Ref](ctx, c, "/projects/"+gid, fields("name"))
}

// Task returns one task with every field the reading pane shows.
func (c *Client) Task(ctx context.Context, gid string) (Task, error) {
	return getOne[Task](ctx, c, "/tasks/"+gid, fields(detailFields))
}

// Subtasks returns a task's direct subtasks.
func (c *Client) Subtasks(ctx context.Context, gid string) ([]Task, error) {
	return getAll[Task](ctx, c, "/tasks/"+gid+"/subtasks", fields("name,completed"))
}

// Attachments returns a task's attachments.
func (c *Client) Attachments(ctx context.Context, taskGID string) ([]Attachment, error) {
	q := fields(attachmentFields)
	q.Set("parent", taskGID)
	return getAll[Attachment](ctx, c, "/attachments", q)
}

// UploadAttachment attaches a file to a task.
func (c *Client) UploadAttachment(ctx context.Context, taskGID, name string, data []byte) (Attachment, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("parent", taskGID); err != nil {
		return Attachment{}, err
	}
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name})},
		"Content-Type":        {http.DetectContentType(data)},
	})
	if err != nil {
		return Attachment{}, err
	}
	if _, err := part.Write(data); err != nil {
		return Attachment{}, err
	}
	if err := w.Close(); err != nil {
		return Attachment{}, err
	}
	var r struct {
		Data Attachment `json:"data"`
	}
	err = c.send(ctx, http.MethodPost, "/attachments", nil, body.Bytes(), w.FormDataContentType(), &r)
	return r.Data, err
}

// Attachment returns one attachment with a fresh download URL.
func (c *Client) Attachment(ctx context.Context, gid string) (Attachment, error) {
	return getOne[Attachment](ctx, c, "/attachments/"+gid, fields(attachmentFields))
}

// WorkspaceUsers returns the users in a workspace.
func (c *Client) WorkspaceUsers(ctx context.Context, workspace string) ([]Ref, error) {
	return getAll[Ref](ctx, c, "/workspaces/"+workspace+"/users", fields("name"))
}

// Sections returns a project's sections.
func (c *Client) Sections(ctx context.Context, projectGID string) ([]Ref, error) {
	return getAll[Ref](ctx, c, "/projects/"+projectGID+"/sections", fields("name"))
}

// ProjectFields returns the custom fields attached to a project, even when
// the project has no tasks or the fields have no values.
func (c *Client) ProjectFields(ctx context.Context, projectGID string) ([]Ref, error) {
	settings, err := getAll[struct {
		CustomField Ref `json:"custom_field"`
	}](ctx, c, "/projects/"+projectGID+"/custom_field_settings", fields("custom_field.gid,custom_field.name"))
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(settings))
	for _, s := range settings {
		refs = append(refs, s.CustomField)
	}
	return refs, nil
}

// ProjectFieldGIDs returns the gids of the custom fields attached to a project.
func (c *Client) ProjectFieldGIDs(ctx context.Context, projectGID string) (map[string]bool, error) {
	refs, err := c.ProjectFields(ctx, projectGID)
	if err != nil {
		return nil, err
	}
	gids := make(map[string]bool, len(refs))
	for _, f := range refs {
		gids[f.GID] = true
	}
	return gids, nil
}

// AddComment posts a plain-text comment on a task.
func (c *Client) AddComment(ctx context.Context, taskGID, text string) error {
	return c.addStory(ctx, taskGID, map[string]any{"text": text})
}

// AddCommentHTML posts a rich-text comment, such as one with @mentions.
func (c *Client) AddCommentHTML(ctx context.Context, taskGID, html string) error {
	return c.addStory(ctx, taskGID, map[string]any{"html_text": html})
}

// UpdateCommentHTML replaces a comment's rich text, keeping its story identity.
func (c *Client) UpdateCommentHTML(ctx context.Context, storyGID, html string) error {
	return c.do(ctx, http.MethodPut, "/stories/"+storyGID, nil, map[string]any{"html_text": html}, nil)
}

func (c *Client) addStory(ctx context.Context, taskGID string, body map[string]any) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/stories", nil, body, nil)
}

// DeleteStory deletes a story the user created.
func (c *Client) DeleteStory(ctx context.Context, gid string) error {
	return c.do(ctx, http.MethodDelete, "/stories/"+gid, nil, nil, nil)
}

// Story returns one story with its author, text, edit permission, target, and reactions.
func (c *Client) Story(ctx context.Context, gid string) (Story, error) {
	return getOne[Story](ctx, c, "/stories/"+gid, fields("type,created_by.name,text,html_text,is_editable,target.gid,reaction_summary.emoji_base,reaction_summary.variant,reaction_summary.count,reaction_summary.reacted"))
}

// SetReaction adds the user's emoji reaction to a story, or removes it when
// reacted is false. The reactions field is undocumented, and Asana ignores
// unknown fields, so a 200 does not prove the reaction changed; read the
// story back to check.
func (c *Client) SetReaction(ctx context.Context, storyGID, emoji string, reacted bool) error {
	body := map[string]any{"reactions": []map[string]any{{"emoji": emoji, "reacted": reacted}}}
	return c.do(ctx, http.MethodPut, "/stories/"+storyGID, nil, body, nil)
}

// AddToSection moves a task into a section.
func (c *Client) AddToSection(ctx context.Context, sectionGID, taskGID string) error {
	return c.do(ctx, http.MethodPost, "/sections/"+sectionGID+"/addTask", nil, map[string]any{"task": taskGID}, nil)
}

// AddToProject adds a task to a project without changing its other memberships.
func (c *Client) AddToProject(ctx context.Context, taskGID, projectGID string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/addProject", nil, map[string]any{"project": projectGID}, nil)
}

// RemoveFromProject removes a task from one project.
func (c *Client) RemoveFromProject(ctx context.Context, taskGID, projectGID string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/removeProject", nil, map[string]any{"project": projectGID}, nil)
}

// SetCustomField sets one custom field on a task; a nil value clears it.
func (c *Client) SetCustomField(ctx context.Context, taskGID, fieldGID string, value any) error {
	return c.updateTask(ctx, taskGID, map[string]any{"custom_fields": map[string]any{fieldGID: value}})
}

// SetAssignee assigns a task to a user; an empty userGID unassigns it.
func (c *Client) SetAssignee(ctx context.Context, taskGID, userGID string) error {
	var assignee any
	if userGID != "" {
		assignee = userGID
	}
	return c.updateTask(ctx, taskGID, map[string]any{"assignee": assignee})
}

// SetName replaces a task's title.
func (c *Client) SetName(ctx context.Context, taskGID, name string) error {
	return c.updateTask(ctx, taskGID, map[string]any{"name": name})
}

// SetHTMLNotes replaces a task's rich-text description, including an empty body.
func (c *Client) SetHTMLNotes(ctx context.Context, taskGID, html string) error {
	return c.updateTask(ctx, taskGID, map[string]any{"html_notes": html})
}

// SetDueOn sets a task's due date (YYYY-MM-DD); an empty date clears it.
func (c *Client) SetDueOn(ctx context.Context, taskGID, date string) error {
	var dueOn any
	if date != "" {
		dueOn = date
	}
	return c.updateTask(ctx, taskGID, map[string]any{"due_on": dueOn})
}

// SetCompleted marks a task complete, or reopens it when done is false.
func (c *Client) SetCompleted(ctx context.Context, taskGID string, done bool) error {
	return c.updateTask(ctx, taskGID, map[string]any{"completed": done})
}

// SetMyTasksSection moves a task into a section of its assignee's My Tasks.
// Only the assignee can do this.
func (c *Client) SetMyTasksSection(ctx context.Context, taskGID, sectionGID string) error {
	return c.updateTask(ctx, taskGID, map[string]any{"assignee_section": sectionGID})
}

// NewTask is a task to create. Empty fields are left out. Section, a gid,
// needs Project. Workspace is needed when the task has no project or parent.
type NewTask struct {
	Name      string
	HTMLNotes string
	Workspace string
	Project   string
	Section   string
	Assignee  string // a user gid or "me"
	DueOn     string // YYYY-MM-DD
}

// CreateTask creates a task and returns it with its list fields.
func (c *Client) CreateTask(ctx context.Context, t NewTask) (Task, error) {
	return c.createTask(ctx, "/tasks", t)
}

// CreateSubtask creates a subtask of the parent task and returns it.
func (c *Client) CreateSubtask(ctx context.Context, parentGID string, t NewTask) (Task, error) {
	return c.createTask(ctx, "/tasks/"+parentGID+"/subtasks", t)
}

func (c *Client) createTask(ctx context.Context, path string, t NewTask) (Task, error) {
	body := map[string]any{"name": t.Name}
	for key, value := range map[string]string{"html_notes": t.HTMLNotes, "workspace": t.Workspace, "assignee": t.Assignee, "due_on": t.DueOn} {
		if value != "" {
			body[key] = value
		}
	}
	if t.Project != "" {
		body["projects"] = []string{t.Project}
	}
	if t.Section != "" {
		body["memberships"] = []map[string]string{{"project": t.Project, "section": t.Section}}
	}
	var r struct {
		Data Task `json:"data"`
	}
	err := c.do(ctx, http.MethodPost, path, fields(listFields), body, &r)
	return r.Data, err
}

func (c *Client) updateTask(ctx context.Context, taskGID string, body map[string]any) error {
	return c.do(ctx, http.MethodPut, "/tasks/"+taskGID, nil, body, nil)
}
