package asana

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// RecentTasks searches all accessible workspace tasks changed since since,
// passing each page of new tasks to page as it arrives. Search uses
// creation-time cursors, not offset pagination. Pages delivered before an
// error remain valid, so callers can disclose incomplete coverage.
func (c *Client) RecentTasks(ctx context.Context, workspace string, since time.Time, page func([]Task)) error {
	q := fields("name,assignee.name,html_notes,created_at,modified_at,permalink_url,memberships.project.name")
	q.Set("modified_at.after", since.UTC().Add(-time.Second).Format(time.RFC3339Nano))
	q.Set("sort_by", "created_at")
	q.Set("sort_ascending", "true")
	q.Set("limit", "100")
	seen := map[string]bool{}
	var cursor time.Time
	for {
		var resp struct {
			Data []Task `json:"data"`
		}
		if err := c.do(ctx, http.MethodGet, "/workspaces/"+workspace+"/tasks/search", q, nil, &resp); err != nil {
			return err
		}
		var fresh []Task
		for _, t := range resp.Data {
			if !seen[t.GID] {
				seen[t.GID] = true
				fresh = append(fresh, t)
			}
		}
		if len(fresh) > 0 {
			page(fresh)
		}
		if len(resp.Data) < 100 {
			return nil
		}
		last, err := time.Parse(time.RFC3339Nano, resp.Data[len(resp.Data)-1].CreatedAt)
		if err != nil || !last.After(cursor) {
			return fmt.Errorf("task search cursor did not advance; results are incomplete")
		}
		cursor = last
		// Keep boundary tasks in the next page and de-duplicate by gid. This
		// avoids silently losing tasks sharing the boundary creation time.
		q.Set("created_at.after", last.Add(-time.Millisecond).UTC().Format(time.RFC3339Nano))
	}
}

// Stories returns task activity since since, or all of it when since is
// zero, oldest first, including comments and system changes. The caller uses
// resource_subtype rather than parsing localized text.
func (c *Client) Stories(ctx context.Context, gid string, since time.Time) ([]Story, error) {
	q := fields("type,resource_subtype,created_at,created_by.name,text,html_text,is_editable,assignee.name,old_dates,new_dates,reaction_summary.variant,reaction_summary.count,reaction_summary.reacted")
	if !since.IsZero() {
		q.Set("created_after", since.UTC().Format(time.RFC3339Nano))
	}
	return getAll[Story](ctx, c, "/tasks/"+gid+"/stories", q)
}
