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
	"time"
)

const (
	Select = "select"
	Hours  = "hours"
	Bool   = "bool"
	Number = "number"
	String = "string"
	Date   = "date"
)

var fieldID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Option is one stable choice in a select control.
type Option struct {
	ID   string `json:"id" toml:"id"`
	Name string `json:"name" toml:"name"`
}

// Field describes one control. Every type except String needs a value.
// Remember associates a select choice with the chosen Asana project after
// submission. Default is the initial value: a select option ID when no
// remembered choice applies, "true" or "false" for Bool, a decimal for
// Number, any text for String, or YYYY-MM-DD for Date. Date values are always
// YYYY-MM-DD; whoever collects one resolves typed text with dateinput.
type Field struct {
	ID       string   `json:"id" toml:"id"`
	Label    string   `json:"label" toml:"label"`
	Type     string   `json:"type" toml:"type"`
	Options  []Option `json:"options,omitempty" toml:"options"`
	Remember bool     `json:"remember,omitempty" toml:"remember"`
	Default  string   `json:"default,omitempty" toml:"default,omitempty"`
}

// Spec contains the controls rendered in one modal. Info lines are context
// shown under the modal's title; only command-supplied forms carry them.
type Spec struct {
	Info   []string `json:"info,omitempty" toml:"-"`
	Fields []Field  `json:"fields" toml:"fields"`
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
			if field.Default != "" && !options[field.Default] {
				return fmt.Errorf("form select %q default is not one of its options", field.ID)
			}
		case Hours:
			if field.Remember || len(field.Options) != 0 || field.Default != "" {
				return fmt.Errorf("form hours %q cannot have options, a default, or be remembered", field.ID)
			}
		case Bool, Number, String, Date:
			if field.Remember || len(field.Options) != 0 {
				return fmt.Errorf("form %s %q cannot have options or be remembered", field.Type, field.ID)
			}
			if field.Default != "" {
				if err := field.check(field.Default); err != nil {
					return fmt.Errorf("form %s %q default: %w", field.Type, field.ID, err)
				}
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
		if err := field.check(strings.TrimSpace(values[field.ID])); err != nil {
			return fmt.Errorf("%s: %w", field.Label, err)
		}
	}
	return nil
}

// check reports whether value is valid for the field's type.
func (f Field) check(value string) error {
	switch f.Type {
	case Select:
		if !f.HasOption(value) {
			return errors.New("choose a current option")
		}
	case Hours:
		if hours, err := decimal(value); err != nil || hours <= 0 {
			return errors.New("enter positive decimal hours")
		}
	case Bool:
		if value != "true" && value != "false" {
			return errors.New(`must be "true" or "false"`)
		}
	case Number:
		if _, err := decimal(value); err != nil {
			return errors.New("enter a decimal number")
		}
	case Date:
		if _, err := time.Parse(time.DateOnly, value); err != nil {
			return errors.New("enter a date as YYYY-MM-DD")
		}
	}
	return nil
}

// decimal parses a finite decimal number.
func decimal(value string) (float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err == nil && (math.IsNaN(n) || math.IsInf(n, 0)) {
		err = errors.New("not finite")
	}
	return n, err
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
