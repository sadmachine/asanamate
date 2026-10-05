// Package timetracking calls an optional command provider for a time-entry
// form, completed entries, and ticket totals.
package timetracking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sadmachine/asanamate/internal/form"
)

// Asana identifies the selected ticket and its chosen Asana project.
type Asana struct {
	TaskGID    string `json:"task_gid"`
	ProjectGID string `json:"project_gid"`
	Title      string `json:"title"`
	URL        string `json:"url"`
}

// Request is sent as JSON on provider stdin.
type Request struct {
	Operation string            `json:"operation"`
	Values    map[string]string `json:"values,omitempty"`
	Asana     *Asana            `json:"asana,omitempty"`
}

// Provider executes the configured command. Data goes through stdin, never
// into user-authored shell text.
type Provider struct{ Command string }

// Summary contains all-time, unrounded time from stopped entries visible to
// the authenticated account, across projects for one exact ticket.
type Summary struct {
	TotalSeconds int64 `json:"total_seconds"`
}

// ErrUnsupported means the provider does not offer ticket summaries.
var ErrUnsupported = errors.New("time summary unsupported")

// Summary fetches a ticket total. Missing totals must not become zero.
func (p Provider) Summary(ctx context.Context, asana Asana) (Summary, error) {
	if asana.TaskGID == "" {
		return Summary{}, errors.New("time summary needs an Asana task")
	}
	out, err := p.call(ctx, Request{Operation: "summary", Asana: &asana})
	if err != nil {
		return Summary{}, err
	}
	var response struct {
		TotalSeconds *int64 `json:"total_seconds"`
		Unsupported  bool   `json:"unsupported"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return Summary{}, fmt.Errorf("time summary: %w", err)
	}
	if response.Unsupported {
		return Summary{}, ErrUnsupported
	}
	if response.TotalSeconds == nil || *response.TotalSeconds < 0 {
		return Summary{}, errors.New("time summary needs nonnegative integer total_seconds")
	}
	return Summary{TotalSeconds: *response.TotalSeconds}, nil
}

func (p Provider) call(ctx context.Context, req Request) ([]byte, error) {
	input, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", p.Command)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("time provider: %s: %w", message, err)
		}
		return nil, fmt.Errorf("time provider: %w", err)
	}
	return out, nil
}

// Form fetches fresh controls and choices for an entry.
func (p Provider) Form(ctx context.Context) (form.Spec, error) {
	out, err := p.call(ctx, Request{Operation: "form"})
	if err != nil {
		return form.Spec{}, err
	}
	var spec form.Spec
	if err := json.Unmarshal(out, &spec); err != nil {
		return form.Spec{}, fmt.Errorf("time form: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return form.Spec{}, err
	}
	hours := 0
	for _, field := range spec.Fields {
		if field.Type == form.Hours {
			hours++
		}
	}
	if hours != 1 {
		return form.Spec{}, errors.New("time form needs exactly one hours field")
	}
	return spec, nil
}

// Log sends one completed entry. Never retry a failed call: provider may have
// created an entry before reporting its error.
func (p Provider) Log(ctx context.Context, spec form.Spec, values map[string]string, asana Asana) error {
	if err := spec.ValidateValues(values); err != nil {
		return err
	}
	_, err := p.call(ctx, Request{Operation: "log", Values: values, Asana: &asana})
	return err
}
