package writeback

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/dateinput"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Change is one staged task edit. Value uses the same text syntax as the
// individual write commands. Target identifies a field or project; Project
// scopes a section move (MyTasks selects the assignee's My Tasks).
type Change struct {
	Kind    string `json:"kind"`
	Target  string `json:"target,omitempty"`
	Value   string `json:"value"`
	Project string `json:"project,omitempty"`
}

// Batch validates every change before writing and asks for confirmation once.
// It stops at the first failure and returns the number of successful changes.
// Writes are sequential, not transactional. Retrying changes[done:] preserves
// progress; comments are deliberately submitted separately, after task edits.
func (s Service) Batch(ctx context.Context, gid string, changes []Change) (done int, err error) {
	if len(changes) == 0 {
		return 0, nil
	}
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return 0, err
	}
	writes := make([]func(context.Context) error, len(changes))
	var details strings.Builder
	for i, change := range changes {
		write, label, err := s.prepareChange(ctx, t, change)
		if err != nil {
			return 0, fmt.Errorf("change %d (%s): %w", i+1, change.Kind, err)
		}
		writes[i] = write
		fmt.Fprintf(&details, "\n- %s", ticket.OneLine(label))
	}
	if err := s.confirm(fmt.Sprintf("Update %q?%s", t.Name, details.String())); err != nil {
		return 0, err
	}
	for i, write := range writes {
		if err := write(ctx); err != nil {
			return i, fmt.Errorf("change %d (%s) failed; %d of %d changes saved: %w", i+1, changes[i].Kind, i, len(changes), err)
		}
	}
	return len(changes), nil
}

func (s Service) prepareChange(ctx context.Context, t asana.Task, c Change) (func(context.Context) error, string, error) {
	var write func(context.Context) error
	label := c.Kind + ": " + c.Value
	switch c.Kind {
	case "title":
		title := strings.TrimSpace(c.Value)
		if err := validateTitle(title); err != nil {
			return nil, "", err
		}
		label = "Title: " + title
		write = func(ctx context.Context) error { return s.Client.SetName(ctx, t.GID, title) }
	case "description":
		html, err := s.descriptionHTML(ctx, strings.TrimSpace(c.Value))
		if err != nil {
			return nil, "", err
		}
		write = func(ctx context.Context) error { return s.Client.SetHTMLNotes(ctx, t.GID, html) }
	case "assign":
		user := strings.TrimSpace(c.Value)
		if user != "" {
			u, err := s.assignee(ctx, user)
			if err != nil {
				return nil, "", err
			}
			user, label = u.GID, "Assign to "+u.Name
		} else {
			label = "Unassign"
		}
		write = func(ctx context.Context) error { return s.Client.SetAssignee(ctx, t.GID, user) }
	case "due":
		date := strings.TrimSpace(c.Value)
		if date != "" {
			var err error
			date, err = dateinput.Resolve(ctx, date)
			if err != nil {
				return nil, "", fmt.Errorf("due date: %w", err)
			}
		}
		write = func(ctx context.Context) error { return s.Client.SetDueOn(ctx, t.GID, date) }
	case "complete":
		done, err := strconv.ParseBool(c.Value)
		if err != nil {
			return nil, "", errors.New("complete value must be true or false")
		}
		write = func(ctx context.Context) error { return s.Client.SetCompleted(ctx, t.GID, done) }
	case "field":
		f, err := s.resolveField(ctx, t, c.Target, c.Project)
		if err != nil {
			return nil, "", err
		}
		value, err := s.fieldValue(ctx, f, c.Value)
		if err != nil {
			return nil, "", err
		}
		label = ticket.FieldName(f) + ": " + c.Value
		write = func(ctx context.Context) error { return s.Client.SetCustomField(ctx, t.GID, f.GID, value) }
	case "move":
		list, err := s.sectionList(ctx, t, c.Project)
		if err != nil {
			return nil, "", err
		}
		section, err := s.findSection(ctx, list, c.Value)
		if err != nil {
			return nil, "", err
		}
		label = "Move to " + list.Name + " / " + section.Name
		write = func(ctx context.Context) error {
			if c.Project == MyTasks {
				return s.Client.SetMyTasksSection(ctx, t.GID, section.GID)
			}
			return s.Client.AddToSection(ctx, section.GID, t.GID)
		}
	case "project_add", "project_remove":
		if c.Target == "" {
			return nil, "", errors.New("project gid is empty")
		}
		label = c.Kind + ": " + c.Target
		write = func(ctx context.Context) error {
			if c.Kind == "project_add" {
				return s.Client.AddToProject(ctx, t.GID, c.Target)
			}
			return s.Client.RemoveFromProject(ctx, t.GID, c.Target)
		}
	default:
		return nil, "", fmt.Errorf("unknown change kind %q", c.Kind)
	}
	return write, label, nil
}
