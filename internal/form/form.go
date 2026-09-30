// Package form defines the small control language shared by actions and
// external extensions.
package form

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	Select = "select"
	Hours  = "hours"
)

var fieldID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Option is one stable choice in a select control.
type Option struct {
	ID   string `json:"id" toml:"id"`
	Name string `json:"name" toml:"name"`
}

// Field describes one required control. Remember associates a select choice
// with the chosen Asana project after submission.
type Field struct {
	ID       string   `json:"id" toml:"id"`
	Label    string   `json:"label" toml:"label"`
	Type     string   `json:"type" toml:"type"`
	Options  []Option `json:"options,omitempty" toml:"options"`
	Remember bool     `json:"remember,omitempty" toml:"remember"`
}

// Spec contains the controls rendered in one modal.
type Spec struct {
	Fields []Field `json:"fields" toml:"fields"`
}

// Validate checks field IDs and supported control types.
func (s Spec) Validate() error {
	if len(s.Fields) == 0 {
		return errors.New("form has no fields")
	}
	seen := map[string]bool{}
	for _, field := range s.Fields {
		if !fieldID.MatchString(field.ID) || field.Label == "" || seen[field.ID] {
			return errors.New("form field IDs must be unique snake_case names with nonempty labels")
		}
		seen[field.ID] = true
		switch field.Type {
		case Select:
			if len(field.Options) == 0 {
				return fmt.Errorf("form select %q has no options", field.ID)
			}
			options := map[string]bool{}
			for _, option := range field.Options {
				if option.ID == "" || option.Name == "" || options[option.ID] {
					return fmt.Errorf("form select %q has empty or duplicate options", field.ID)
				}
				options[option.ID] = true
			}
		case Hours:
			if field.Remember || len(field.Options) != 0 {
				return fmt.Errorf("form hours %q cannot have options or be remembered", field.ID)
			}
		default:
			return fmt.Errorf("form field %q has unknown type %q", field.ID, field.Type)
		}
	}
	return nil
}

// ValidateValues checks all submitted values against the current options.
func (s Spec) ValidateValues(values map[string]string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	for _, field := range s.Fields {
		value := strings.TrimSpace(values[field.ID])
		switch field.Type {
		case Select:
			if !field.HasOption(value) {
				return fmt.Errorf("%s: choose a current option", field.Label)
			}
		case Hours:
			hours, err := strconv.ParseFloat(value, 64)
			if err != nil || hours <= 0 || math.IsNaN(hours) || math.IsInf(hours, 0) {
				return fmt.Errorf("%s: enter positive decimal hours", field.Label)
			}
		}
	}
	return nil
}

// HasOption reports whether id is a current select choice.
func (f Field) HasOption(id string) bool {
	for _, option := range f.Options {
		if option.ID == id {
			return true
		}
	}
	return false
}

// LabelFor returns a selected option's label, or empty string when absent.
func (f Field) LabelFor(id string) string {
	for _, option := range f.Options {
		if option.ID == id {
			return option.Name
		}
	}
	return ""
}
