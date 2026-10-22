// Package ticket assembles an Asana task with its comments, attachments, and subtasks.
package ticket

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/sadmachine/asanamate/internal/asana"
)

// RecentlyCompleted is how far back completed tasks are included in lists.
const RecentlyCompleted = 7 * 24 * time.Hour

// List returns the tasks for a view: My Tasks when projectGID is empty,
// otherwise the project's tasks. Incomplete tasks and recently completed ones
// are included.
func List(ctx context.Context, c *asana.Client, workspace, projectGID string) ([]asana.Task, error) {
	since := time.Now().Add(-RecentlyCompleted)
	if projectGID == "" {
		return c.MyTasks(ctx, workspace, since)
	}
	return c.ProjectTasks(ctx, projectGID, since)
}

// Ticket is a task plus everything the reading pane and actions need. Its
// stories split into Comments and Events, the system changes, each oldest
// first.
type Ticket struct {
	asana.Task
	Comments    []asana.Story      `json:"comments"`
	Events      []asana.Story      `json:"events"`
	Attachments []asana.Attachment `json:"attachments"`
	Subtasks    []asana.Task       `json:"subtasks"`
}

// Fetch loads a ticket, calling the four endpoints in parallel.
func Fetch(ctx context.Context, c *asana.Client, gid string) (Ticket, error) {
	var t Ticket
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { t.Task, err = c.Task(ctx, gid); return err })
	g.Go(func() error {
		stories, err := c.Stories(ctx, gid, time.Time{})
		for _, s := range stories {
			if s.Type == "comment" {
				t.Comments = append(t.Comments, s)
			} else {
				t.Events = append(t.Events, s)
			}
		}
		return err
	})
	g.Go(func() (err error) { t.Attachments, err = c.Attachments(ctx, gid); return err })
	g.Go(func() (err error) { t.Subtasks, err = c.Subtasks(ctx, gid); return err })
	return t, g.Wait()
}

// Entry is a story on a ticket's timeline: a comment, or a system change
// when Comment is false.
type Entry struct {
	asana.Story
	Comment bool
}

// Timeline returns the ticket's comments and events merged, oldest first.
func (t Ticket) Timeline() []Entry {
	all := make([]Entry, 0, len(t.Comments)+len(t.Events))
	for _, c := range t.Comments {
		all = append(all, Entry{c, true})
	}
	for _, e := range t.Events {
		all = append(all, Entry{e, false})
	}
	slices.SortStableFunc(all, func(a, b Entry) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })
	return all
}

// Clean removes control characters (except newline and tab) so untrusted
// Asana text cannot inject terminal escape sequences.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

var flatten = strings.NewReplacer("\t", " ", "\n", " ")

// OneLine cleans s and replaces tabs and newlines with spaces, for text that
// must stay on one line (list rows, TSV cells).
func OneLine(s string) string { return flatten.Replace(Clean(s)) }

// AttachmentURL returns the best browser URL for an attachment.
func AttachmentURL(a asana.Attachment) string {
	if a.PermanentURL != "" {
		return a.PermanentURL
	}
	return a.ViewURL
}
