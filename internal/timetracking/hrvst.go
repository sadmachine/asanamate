package timetracking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/form"
)

// RunHarvest serves the provider protocol using the installed hrvst CLI.
func RunHarvest(ctx context.Context, in io.Reader, out io.Writer, taskID string) error {
	var req Request
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		return fmt.Errorf("time provider request: %w", err)
	}
	switch req.Operation {
	case "form":
		if !digits(taskID) {
			return errors.New("hrvst needs a numeric default task ID (--task-id)")
		}
		cmd := exec.CommandContext(ctx, "hrvst", "users", "project-assignments", "me", "--output=json", "--page=all")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("hrvst project assignments: %s: %w", strings.TrimSpace(stderr.String()), err)
		}
		projects, err := parseAssignments(data)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(form.Spec{Fields: []form.Field{
			{ID: "project_id", Label: "Harvest project", Type: form.Select, Options: projects, Remember: true},
			{ID: "task_id", Label: "Harvest task", Type: form.Select, Options: []form.Option{{ID: taskID, Name: "Engineering"}}, Remember: true},
			{ID: "hours", Label: "Hours", Type: form.Hours},
		}})
	case "log":
		if !digits(taskID) {
			return errors.New("hrvst needs a numeric default task ID (--task-id)")
		}
		if req.Asana == nil || req.Asana.TaskGID == "" || req.Asana.ProjectGID == "" || req.Asana.URL == "" {
			return errors.New("hrvst log needs an Asana task, project, and URL")
		}
		projectID, selectedTask := req.Values["project_id"], req.Values["task_id"]
		hours, err := strconv.ParseFloat(strings.TrimSpace(req.Values["hours"]), 64)
		if err != nil || hours <= 0 || math.IsNaN(hours) || math.IsInf(hours, 0) {
			return errors.New("hrvst log needs positive decimal hours")
		}
		if !digits(projectID) {
			return errors.New("hrvst project ID must be numeric")
		}
		if selectedTask != taskID {
			return errors.New("hrvst task is not the configured default task")
		}
		args := []string{"time-entries", "create", "--project_id", projectID, "--task_id", selectedTask, "--spent_date", time.Now().Format("2006-01-02"), "--hours", strconv.FormatFloat(hours, 'f', -1, 64), "--notes", req.Asana.Title, "--external_reference[id]", req.Asana.TaskGID, "--external_reference[group_id]", req.Asana.ProjectGID, "--external_reference[permalink]", req.Asana.URL}
		data, err := exec.CommandContext(ctx, "hrvst", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("hrvst time-entries create: %s: %w", strings.TrimSpace(string(data)), err)
		}
		return nil
	default:
		return fmt.Errorf("unknown time provider operation %q", req.Operation)
	}
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseAssignments accepts hrvst's JSON array of active assignments. Some
// versions may print the Harvest response wrapper instead of its array.
func parseAssignments(data []byte) ([]form.Option, error) {
	type assignment struct {
		IsActive *bool `json:"is_active"`
		Project  struct {
			ID   json.Number `json:"id"`
			Name string      `json:"name"`
		} `json:"project"`
	}
	var assignments []assignment
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&assignments); err != nil {
		var wrapped struct {
			Assignments []assignment `json:"project_assignments"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&wrapped); err != nil || wrapped.Assignments == nil {
			return nil, errors.New("hrvst project assignments: invalid JSON response")
		}
		assignments = wrapped.Assignments
	}
	seen := map[string]bool{}
	var projects []form.Option
	for _, a := range assignments {
		if a.IsActive != nil && !*a.IsActive {
			continue
		}
		id := a.Project.ID.String()
		if !digits(id) || a.Project.Name == "" {
			return nil, errors.New("hrvst project assignments: missing project ID or name")
		}
		if !seen[id] {
			seen[id] = true
			projects = append(projects, form.Option{ID: id, Name: a.Project.Name})
		}
	}
	return projects, nil
}
