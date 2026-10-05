package timetracking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/form"
)

type harvestExternalReference struct {
	ID        string `json:"id"`
	GroupID   string `json:"group_id"`
	Permalink string `json:"permalink"`
}

type harvestEntryRequest struct {
	ProjectID         string                   `json:"project_id"`
	TaskID            string                   `json:"task_id"`
	SpentDate         string                   `json:"spent_date"`
	Hours             float64                  `json:"hours"`
	Notes             string                   `json:"notes"`
	ExternalReference harvestExternalReference `json:"external_reference"`
}

// RunHarvest serves the provider protocol using hrvst's project assignments
// and credentials. Harvest's API needs a nested external_reference object;
// hrvst's create command flattens its bracketed flags and drops the link.
func RunHarvest(ctx context.Context, in io.Reader, out io.Writer, taskID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return runHarvest(ctx, in, out, taskID, filepath.Join(home, ".hrvst", "config.json"), "https://api.harvestapp.com/v2/time_entries")
}

func runHarvest(ctx context.Context, in io.Reader, out io.Writer, taskID, configPath, endpoint string) error {
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
	case "summary":
		if req.Asana == nil || !digits(req.Asana.TaskGID) {
			return errors.New("hrvst summary needs a numeric Asana task GID")
		}
		summary, err := harvestSummary(ctx, configPath, endpoint, req.Asana.TaskGID)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(summary)
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
		return createHarvestEntry(ctx, configPath, endpoint, projectID, selectedTask, hours, *req.Asana)
	default:
		return fmt.Errorf("unknown time provider operation %q", req.Operation)
	}
}

func createHarvestEntry(ctx context.Context, configPath, endpoint, projectID, taskID string, hours float64, asana Asana) error {
	credentials, err := loadHarvestCredentials(configPath)
	if err != nil {
		return err
	}
	body, err := json.Marshal(harvestEntryRequest{
		ProjectID: projectID, TaskID: taskID, SpentDate: time.Now().Format("2006-01-02"), Hours: hours, Notes: asana.Title,
		ExternalReference: harvestExternalReference{ID: asana.TaskGID, GroupID: asana.ProjectGID, Permalink: asana.URL},
	})
	if err != nil {
		return err
	}
	result, err := credentials.call(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	var entry struct {
		ID                json.Number `json:"id"`
		ExternalReference *struct {
			ID        string `json:"id"`
			Permalink string `json:"permalink"`
		} `json:"external_reference"`
	}
	if err := json.Unmarshal(result, &entry); err != nil {
		return fmt.Errorf("Harvest created time entry, but response could not be checked: %w; do not submit again", err)
	}
	if entry.ExternalReference == nil || entry.ExternalReference.ID != asana.TaskGID || entry.ExternalReference.Permalink == "" {
		return fmt.Errorf("Harvest created time entry %s without the Asana link; do not submit again", entry.ID)
	}
	return nil
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

// harvestCredentials are shared by entry creation and summary retrieval.
type harvestCredentials struct {
	AccessToken string `json:"accessToken"`
	AccountID   string `json:"accountId"`
}

func loadHarvestCredentials(configPath string) (harvestCredentials, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return harvestCredentials{}, fmt.Errorf("hrvst credentials: %w", err)
	}
	var credentials harvestCredentials
	if err := json.Unmarshal(data, &credentials); err != nil || credentials.AccessToken == "" || credentials.AccountID == "" {
		return harvestCredentials{}, errors.New("hrvst credentials: missing access token or account ID; run `hrvst login`")
	}
	return credentials, nil
}

func (credentials harvestCredentials) call(ctx context.Context, method, endpoint string, body io.Reader) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("Harvest-Account-ID", credentials.AccountID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "asanamate")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Harvest time entry: %w", err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("Harvest time entry response: %w", err)
	}
	if len(result) > 1<<20 {
		return nil, errors.New("Harvest response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Harvest time entry: %s: %s", response.Status, strings.TrimSpace(string(result)))
	}
	return result, nil
}

func harvestSummary(ctx context.Context, configPath, endpoint, gid string) (Summary, error) {
	credentials, err := loadHarvestCredentials(configPath)
	if err != nil {
		return Summary{}, err
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		return Summary{}, err
	}
	var hours float64
	for page := 1; ; {
		query := base.Query()
		query.Set("external_reference_id", gid)
		query.Set("is_running", "false")
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		base.RawQuery = query.Encode()
		data, err := credentials.call(ctx, http.MethodGet, base.String(), nil)
		if err != nil {
			return Summary{}, err
		}
		var result struct {
			Entries *[]struct {
				Hours     *float64                  `json:"hours"`
				Running   *bool                     `json:"is_running"`
				Reference *harvestExternalReference `json:"external_reference"`
			} `json:"time_entries"`
			NextPage json.RawMessage `json:"next_page"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return Summary{}, fmt.Errorf("Harvest time summary: %w", err)
		}
		if result.Entries == nil {
			return Summary{}, errors.New("Harvest time summary: missing time_entries")
		}
		for _, entry := range *result.Entries {
			if entry.Reference == nil || entry.Reference.ID != gid {
				continue
			}
			if entry.Running == nil || entry.Hours == nil || *entry.Hours < 0 || math.IsNaN(*entry.Hours) || math.IsInf(*entry.Hours, 0) {
				return Summary{}, errors.New("Harvest time summary: invalid entry duration or timer state")
			}
			if !*entry.Running {
				hours += *entry.Hours
			}
		}
		var nextPage *int
		if len(result.NextPage) == 0 || json.Unmarshal(result.NextPage, &nextPage) != nil {
			return Summary{}, errors.New("Harvest time summary: missing or invalid next_page")
		}
		if nextPage == nil {
			break
		}
		if *nextPage <= page {
			return Summary{}, errors.New("Harvest time summary: invalid next_page")
		}
		page = *nextPage
	}
	seconds := math.Round(hours * 3600)
	if math.IsInf(seconds, 0) || seconds >= float64(math.MaxInt64) {
		return Summary{}, errors.New("Harvest time summary: duration overflow")
	}
	return Summary{TotalSeconds: int64(seconds)}, nil
}
