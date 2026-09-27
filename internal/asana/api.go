package asana

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const (
	listFields       = "name,completed,due_on,assignee.name,assignee_section.name,memberships.project.name,memberships.section.name,tags.name,permalink_url"
	detailFields     = listFields + ",html_notes,custom_fields.name,custom_fields.display_value,custom_fields.resource_subtype,custom_fields.enum_options.name,dependencies.name,dependents.name,parent.name,created_at,modified_at"
	attachmentFields = "name,host,download_url,permanent_url,view_url"
)

func fields(f string) url.Values { return url.Values{"opt_fields": {f}} }

// Me returns the authenticated user and their workspaces.
func (c *Client) Me(ctx context.Context) (User, error) {
	return getOne[User](ctx, c, "/users/me", fields("name,workspaces.name"))
}

// MyTasks returns the user's My Tasks list: incomplete tasks plus tasks completed after since.
func (c *Client) MyTasks(ctx context.Context, workspace string, since time.Time) ([]Task, error) {
	list, err := getOne[Ref](ctx, c, "/users/me/user_task_list", url.Values{"workspace": {workspace}})
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

// Sections returns a project's sections.
func (c *Client) Sections(ctx context.Context, projectGID string) ([]Ref, error) {
	return getAll[Ref](ctx, c, "/projects/"+projectGID+"/sections", fields("name"))
}

// AddComment posts a plain-text comment on a task.
func (c *Client) AddComment(ctx context.Context, taskGID, text string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/stories", nil, map[string]any{"text": text}, nil)
}

// AddToSection moves a task into a section.
func (c *Client) AddToSection(ctx context.Context, sectionGID, taskGID string) error {
	return c.do(ctx, http.MethodPost, "/sections/"+sectionGID+"/addTask", nil, map[string]any{"task": taskGID}, nil)
}

// SetCustomField sets one custom field on a task; a nil value clears it.
func (c *Client) SetCustomField(ctx context.Context, taskGID, fieldGID string, value any) error {
	body := map[string]any{"custom_fields": map[string]any{fieldGID: value}}
	return c.do(ctx, http.MethodPut, "/tasks/"+taskGID, nil, body, nil)
}
