package asana

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const (
	listFields       = "name,completed,due_on,assignee.name,assignee_section.name,memberships.project.name,memberships.section.name,tags.name,permalink_url,custom_fields.name,custom_fields.display_value,custom_fields.is_value_read_only,custom_fields.is_formula_field"
	detailFields     = listFields + ",html_notes,custom_fields.resource_subtype,custom_fields.representation_type,custom_fields.enum_options.name,custom_fields.multi_enum_values.name,custom_fields.people_value.name,custom_fields.date_value.date,dependencies.name,dependents.name,parent.name,created_at,modified_at"
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

// MemberProjects returns unarchived projects in workspace that userGID is a member of.
func (c *Client) MemberProjects(ctx context.Context, workspace, userGID string) ([]Project, error) {
	q := fields("name,members.gid")
	q.Set("workspace", workspace)
	q.Set("archived", "false")
	all, err := getAll[Project](ctx, c, "/projects", q)
	if err != nil {
		return nil, err
	}
	var mine []Project
	for _, p := range all {
		for _, m := range p.Members {
			if m.GID == userGID {
				mine = append(mine, p)
				break
			}
		}
	}
	return mine, nil
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

// Comments returns a task's comment stories, oldest first.
func (c *Client) Comments(ctx context.Context, gid string) ([]Story, error) {
	all, err := getAll[Story](ctx, c, "/tasks/"+gid+"/stories", fields("type,created_at,created_by.name,html_text"))
	if err != nil {
		return nil, err
	}
	var comments []Story
	for _, s := range all {
		if s.Type == "comment" {
			comments = append(comments, s)
		}
	}
	return comments, nil
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

// ProjectFieldGIDs returns the gids of the custom fields attached to a project.
func (c *Client) ProjectFieldGIDs(ctx context.Context, projectGID string) (map[string]bool, error) {
	settings, err := getAll[struct {
		CustomField Ref `json:"custom_field"`
	}](ctx, c, "/projects/"+projectGID+"/custom_field_settings", fields("custom_field.gid"))
	if err != nil {
		return nil, err
	}
	gids := make(map[string]bool, len(settings))
	for _, s := range settings {
		gids[s.CustomField.GID] = true
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

func (c *Client) addStory(ctx context.Context, taskGID string, body map[string]any) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/stories", nil, body, nil)
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

// SetDueOn sets a task's due date (YYYY-MM-DD); an empty date clears it.
func (c *Client) SetDueOn(ctx context.Context, taskGID, date string) error {
	var dueOn any
	if date != "" {
		dueOn = date
	}
	return c.updateTask(ctx, taskGID, map[string]any{"due_on": dueOn})
}

// SetMyTasksSection moves a task into a section of its assignee's My Tasks.
// Only the assignee can do this.
func (c *Client) SetMyTasksSection(ctx context.Context, taskGID, sectionGID string) error {
	return c.updateTask(ctx, taskGID, map[string]any{"assignee_section": sectionGID})
}

func (c *Client) updateTask(ctx context.Context, taskGID string, body map[string]any) error {
	return c.do(ctx, http.MethodPut, "/tasks/"+taskGID, nil, body, nil)
}
