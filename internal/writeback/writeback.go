// Package writeback performs confirmed updates to Asana tasks.
package writeback

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var (
	// ErrNoTTY means confirmation was required but no terminal was available.
	ErrNoTTY = errors.New("confirmation required but no terminal is available; pass --yes or set confirm_writes = false")
	// ErrDeclined means the user answered no.
	ErrDeclined = errors.New("cancelled")
)

// Confirmer asks the user to approve a write.
type Confirmer func(prompt string) (bool, error)

// NeedsConfirm resolves whether to confirm: --yes wins, then
// ASANAMATE_CONFIRM_WRITES ("1"/"0"), then the config default.
func NeedsConfirm(yes bool, envValue string, configDefault bool) bool {
	if yes {
		return false
	}
	switch envValue {
	case "0":
		return false
	case "1":
		return true
	}
	return configDefault
}

// TTYConfirm asks on /dev/tty, so it works even when stdin is a pipe.
func TTYConfirm(question string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, ErrNoTTY
	}
	defer tty.Close()
	return prompt.Confirm(bufio.NewReader(tty), tty, question)
}

// Service writes to Asana, asking Confirm first when it is set.
type Service struct {
	Client  *asana.Client
	Confirm Confirmer
}

func (s Service) confirm(question string) error {
	if s.Confirm == nil {
		return nil
	}
	ok, err := s.Confirm(question)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDeclined
	}
	return nil
}

// Comment posts text as a comment on the task.
func (s Service) Comment(ctx context.Context, gid, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("comment text is empty")
	}
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	if err := s.confirm(fmt.Sprintf("Comment on %q?\n%s\n", t.Name, text)); err != nil {
		return err
	}
	return s.Client.AddComment(ctx, gid, text)
}

// Move puts the task in the named section of one of its projects.
func (s Service) Move(ctx context.Context, gid, sectionName, projectGID string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	project, err := pickProject(t, projectGID)
	if err != nil {
		return err
	}
	sections, err := s.Client.Sections(ctx, project.GID)
	if err != nil {
		return err
	}
	var names []string
	for _, sec := range sections {
		if strings.EqualFold(strings.TrimSpace(sec.Name), strings.TrimSpace(sectionName)) {
			if err := s.confirm(fmt.Sprintf("Move %q to %s / %s?", t.Name, ticket.Clean(project.Name), ticket.Clean(sec.Name))); err != nil {
				return err
			}
			return s.Client.AddToSection(ctx, sec.GID, gid)
		}
		names = append(names, ticket.Clean(sec.Name))
	}
	return fmt.Errorf("no section %q in %s; sections: %s", sectionName, ticket.Clean(project.Name), strings.Join(names, ", "))
}

func pickProject(t asana.Task, projectGID string) (asana.Ref, error) {
	var choices []string
	for _, m := range t.Memberships {
		if m.Project.GID == projectGID {
			return m.Project, nil
		}
		choices = append(choices, fmt.Sprintf("%s (%s)", ticket.Clean(m.Project.Name), m.Project.GID))
	}
	switch {
	case projectGID != "":
		return asana.Ref{}, fmt.Errorf("task is not in project %s", projectGID)
	case len(t.Memberships) == 0:
		return asana.Ref{}, errors.New("task is not in any project")
	case len(t.Memberships) == 1:
		return t.Memberships[0].Project, nil
	}
	return asana.Ref{}, fmt.Errorf("task is in several projects; pass --project with one of: %s", strings.Join(choices, ", "))
}

// SetField sets a text, number, or enum custom field. An empty value clears it.
// When the task has several fields with that name, projectGID picks the one
// attached to that project.
func (s Service) SetField(ctx context.Context, gid, fieldName, value, projectGID string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	f, err := s.resolveField(ctx, t, fieldName, projectGID)
	if err != nil {
		return err
	}
	v, err := FieldValue(f, value)
	if err != nil {
		return err
	}
	if err := s.confirm(fmt.Sprintf("Set %q on %q to %q?", ticket.Clean(strings.TrimSpace(f.Name)), t.Name, value)); err != nil {
		return err
	}
	return s.Client.SetCustomField(ctx, gid, f.GID, v)
}

// resolveField finds the one field to write. Writes never guess between
// same-named fields: they need the project that owns the intended one.
func (s Service) resolveField(ctx context.Context, t asana.Task, name, projectGID string) (asana.CustomField, error) {
	var matches []asana.CustomField
	for _, f := range t.CustomFields {
		if asana.SameFieldName(f.Name, name) {
			matches = append(matches, f)
		}
	}
	switch {
	case len(matches) == 0:
		return asana.CustomField{}, fmt.Errorf("task has no custom field %q", name)
	case len(matches) == 1:
		return matches[0], nil
	case projectGID == "":
		return asana.CustomField{}, fmt.Errorf("task has %d custom fields named %q; pass --project with the gid of the project whose field to set", len(matches), name)
	}
	onProject, err := s.Client.ProjectFieldGIDs(ctx, projectGID)
	if err != nil {
		return asana.CustomField{}, err
	}
	for _, f := range matches {
		if onProject[f.GID] {
			return f, nil
		}
	}
	return asana.CustomField{}, fmt.Errorf("none of the fields named %q is on project %s", name, projectGID)
}

// FieldValue converts typed text to the API value for a text, number, enum, or
// date (YYYY-MM-DD) field. Empty text is nil, which clears the field.
func FieldValue(f asana.CustomField, value string) (any, error) {
	name := ticket.Clean(strings.TrimSpace(f.Name))
	if value == "" {
		return nil, nil
	}
	switch f.ResourceSubtype {
	case "text":
		return value, nil
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("%s needs a number, got %q", name, value)
		}
		return n, nil
	case "enum":
		var options []string
		for _, o := range f.EnumOptions {
			if strings.EqualFold(o.Name, value) {
				return o.GID, nil
			}
			options = append(options, ticket.Clean(o.Name))
		}
		return nil, fmt.Errorf("%s has no option %q; options: %s", name, value, strings.Join(options, ", "))
	case "date":
		if _, err := time.Parse(time.DateOnly, value); err != nil {
			return nil, fmt.Errorf("%s needs a date as YYYY-MM-DD, got %q", name, value)
		}
		return map[string]any{"date": value}, nil
	}
	return nil, fmt.Errorf("%s: custom field type %q is not supported", name, f.ResourceSubtype)
}
