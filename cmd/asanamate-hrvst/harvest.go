package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/projectlinks"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

// harvestEndpoint is Harvest's time entries API; tests point it at a fake.
var harvestEndpoint = "https://api.harvestapp.com/v2/time_entries"

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

// harvestConfigPath is where hrvst 3.x keeps its login; it has no override.
func harvestConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hrvst", "config.json"), nil
}

// runHarvest serves one time tracking or feed request. Entry creation uses
// Harvest's nested external_reference to preserve the Asana link.
func runHarvest(ctx context.Context, in io.Reader, out io.Writer, taskID, configPath, endpoint string) error {
	settings, _, _, err := readHarvestSettings()
	if err != nil {
		return err
	}
	if taskID == "" {
		taskID = settings.TaskID
	}
	if taskID != "" && !positiveID(taskID) {
		return errors.New("Harvest default task ID must be positive numeric (--task-id or harvest.toml task_id)")
	}
	var req struct {
		timetracking.Request
		Date string `json:"date"`
	}
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		return fmt.Errorf("time provider request: %w", err)
	}
	switch req.Operation {
	case "feed":
		reply, err := harvestFeed(ctx, configPath, endpoint, req.Date)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(reply)
	case "form":
		if !digits(taskID) {
			return errors.New("Harvest needs a default task; run asanamate-hrvst setup or pass --task-id")
		}
		assignments, err := harvestAssignments(ctx, configPath, endpoint)
		if err != nil {
			return err
		}
		projects, task, err := assignments.options(taskID)
		if err != nil {
			return err
		}
		project := form.Field{ID: "project_id", Label: "Harvest project", Type: form.Select, Options: projects, Remember: true}
		for _, id := range projectlinks.Values(req.Links, projectlinks.HarvestProject) {
			if project.HasOption(id) {
				project.Default = id
				break
			}
		}
		return json.NewEncoder(out).Encode(form.Spec{Info: []string{harvestClientsToday(ctx, configPath, endpoint)}, Fields: []form.Field{
			project,
			{ID: "task_id", Label: "Harvest task", Type: form.Select, Options: []form.Option{task}, Remember: true},
			{ID: "hours", Label: "Hours", Type: form.Hours},
			{ID: "spent_date", Label: "Date", Type: form.Date},
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
			return errors.New("Harvest needs a default task; run asanamate-hrvst setup or pass --task-id")
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
		spentDate := req.Values["spent_date"]
		if _, err := time.Parse(time.DateOnly, spentDate); err != nil {
			return errors.New("hrvst log needs spent_date as YYYY-MM-DD")
		}
		credentials, err := loadHarvestCredentials(configPath)
		if err != nil {
			return err
		}
		assignments, err := credentials.assignments(ctx, endpoint)
		if err != nil {
			return err
		}
		projects, _, err := assignments.options(taskID)
		if err != nil {
			return err
		}
		eligible := false
		for _, project := range projects {
			if project.ID == projectID {
				eligible = true
			}
		}
		if !eligible {
			return errors.New("Harvest project/task is inactive or unassigned; reopen the time form")
		}
		return credentials.createEntry(ctx, endpoint, projectID, selectedTask, spentDate, hours, *req.Asana)
	default:
		return fmt.Errorf("unknown time provider operation %q", req.Operation)
	}
}

func (credentials harvestCredentials) createEntry(ctx context.Context, endpoint, projectID, taskID, spentDate string, hours float64, asana timetracking.Asana) error {
	body, err := json.Marshal(harvestEntryRequest{
		ProjectID: projectID, TaskID: taskID, SpentDate: spentDate, Hours: hours, Notes: asana.Title,
		ExternalReference: harvestExternalReference{ID: asana.TaskGID, GroupID: asana.ProjectGID, Permalink: asana.URL},
	})
	if err != nil {
		return err
	}
	result, err := credentials.call(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w; entry creation may have succeeded; do not submit again without checking Harvest", err)
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
	if !positiveID(entry.ID.String()) || entry.ExternalReference == nil || entry.ExternalReference.ID != asana.TaskGID || entry.ExternalReference.Permalink != asana.URL {
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

// harvestRef is the id and name of a Harvest project or task.
type harvestRef struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}

// harvestCredentials are shared by entry creation and summary retrieval.
type harvestCredentials struct {
	AccessToken string `json:"accessToken"`
	AccountID   string `json:"accountId"`
}

func loadHarvestCredentials(configPath string) (harvestCredentials, error) {
	settings, _, _, err := readHarvestSettings()
	if err != nil {
		return harvestCredentials{}, err
	}
	if credentials, native, err := nativeCredentials(settings); native || err != nil {
		return credentials, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return harvestCredentials{}, errors.New("Harvest credentials unavailable; run asanamate-hrvst setup with HARVEST_ACCESS_TOKEN and HARVEST_ACCOUNT_ID")
	}
	var credentials harvestCredentials
	if err := json.Unmarshal(data, &credentials); err != nil || !validToken(credentials.AccessToken) || !positiveID(credentials.AccountID) {
		return harvestCredentials{}, errors.New("legacy Harvest credentials invalid; run asanamate-hrvst setup")
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
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, errors.New("Harvest request cancelled")
		}
		var networkError interface{ Timeout() bool }
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, errors.New("Harvest request timed out; check Harvest status and network")
		}
		return nil, errors.New("Harvest network failure; check connectivity and Harvest status")
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, errors.New("Harvest response could not be read; check connectivity")
	}
	if len(result) > 1<<20 {
		return nil, errors.New("Harvest response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case 401:
			return nil, errors.New("Harvest 401: authentication rejected; check your access token")
		case 403, 404:
			return nil, fmt.Errorf("Harvest %d: account or resource inaccessible; check account ID and permissions", response.StatusCode)
		case 429:
			return nil, errors.New("Harvest 429: rate limited; wait before trying again (no automatic retry)")
		default:
			return nil, fmt.Errorf("Harvest HTTP %d; check Harvest status (credentials not rechecked)", response.StatusCode)
		}
	}
	return result, nil
}

func harvestSummary(ctx context.Context, configPath, endpoint, gid string) (timetracking.Summary, error) {
	today := time.Now().Format(time.DateOnly)
	credentials, err := loadHarvestCredentials(configPath)
	if err != nil {
		return timetracking.Summary{}, err
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		return timetracking.Summary{}, err
	}
	var hours, todayHours float64
	query := url.Values{"external_reference_id": {gid}, "is_running": {"false"}}
	err = eachPage(ctx, credentials, base, query, "Harvest time summary", func(data []byte) error {
		var result struct {
			Entries *[]struct {
				Hours     *float64                  `json:"hours"`
				Running   *bool                     `json:"is_running"`
				SpentDate string                    `json:"spent_date"`
				Reference *harvestExternalReference `json:"external_reference"`
			} `json:"time_entries"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.Entries == nil {
			return errors.New("missing time_entries")
		}
		for _, entry := range *result.Entries {
			if entry.Reference == nil || entry.Reference.ID != gid {
				continue
			}
			if entry.Running == nil || entry.Hours == nil || *entry.Hours < 0 || math.IsNaN(*entry.Hours) || math.IsInf(*entry.Hours, 0) {
				return errors.New("invalid entry duration or timer state")
			}
			if !*entry.Running {
				if _, err := time.Parse(time.DateOnly, entry.SpentDate); err != nil {
					return errors.New("missing or invalid spent_date")
				}
				hours += *entry.Hours
				if entry.SpentDate == today {
					todayHours += *entry.Hours
				}
			}
		}
		return nil
	})
	if err != nil {
		return timetracking.Summary{}, err
	}
	seconds := math.Round(hours * 3600)
	if math.IsInf(seconds, 0) || seconds >= float64(math.MaxInt64) {
		return timetracking.Summary{}, errors.New("Harvest time summary: duration overflow")
	}
	todaySeconds := int64(math.Round(todayHours * 3600))
	return timetracking.Summary{TotalSeconds: int64(seconds), TodaySeconds: &todaySeconds}, nil
}

// eachPage gets every page at base with query, 100 per
// page, and passes each page's body to fn. It fails rather than return part
// of the pages. label prefixes errors.
func eachPage(ctx context.Context, credentials harvestCredentials, base *url.URL, query url.Values, label string, fn func([]byte) error) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for page, count := 1, 0; ; count++ {
		if count >= 1000 {
			return fmt.Errorf("%s: pagination exceeds 1000 pages", label)
		}
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		u := *base
		u.RawQuery = query.Encode()
		data, err := credentials.call(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		if err := fn(data); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		var result struct {
			NextPage json.RawMessage `json:"next_page"`
		}
		var nextPage *int
		if json.Unmarshal(data, &result) != nil || len(result.NextPage) == 0 || json.Unmarshal(result.NextPage, &nextPage) != nil {
			return fmt.Errorf("%s: missing or invalid next_page", label)
		}
		if nextPage == nil {
			return nil
		}
		if *nextPage != page+1 {
			return fmt.Errorf("%s: invalid next_page", label)
		}
		page = *nextPage
	}
}

// harvestEntry is one of the user's own time entries.
type harvestEntry struct {
	Hours     *float64                  `json:"hours"`
	Running   bool                      `json:"is_running"`
	Notes     string                    `json:"notes"`
	CreatedAt time.Time                 `json:"created_at"`
	Client    struct{ Name string }     `json:"client"`
	Project   struct{ Name string }     `json:"project"`
	Reference *harvestExternalReference `json:"external_reference"`
}

// harvestUserEntries gets every entry the user spent on date, each with a
// valid duration. label prefixes errors.
func harvestUserEntries(ctx context.Context, configPath, endpoint, date, label string) ([]harvestEntry, error) {
	credentials, err := loadHarvestCredentials(configPath)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	data, err := credentials.call(ctx, http.MethodGet, base.ResolveReference(&url.URL{Path: "users/me"}).String(), nil)
	if err != nil {
		return nil, err
	}
	var me struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(data, &me); err != nil || !positiveID(me.ID.String()) {
		return nil, fmt.Errorf("%s: missing user id", label)
	}
	var entries []harvestEntry
	query := url.Values{"user_id": {me.ID.String()}, "from": {date}, "to": {date}}
	err = eachPage(ctx, credentials, base, query, label, func(data []byte) error {
		var result struct {
			Entries *[]harvestEntry `json:"time_entries"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.Entries == nil {
			return errors.New("missing time_entries")
		}
		for _, entry := range *result.Entries {
			if entry.Hours == nil || *entry.Hours < 0 || math.IsNaN(*entry.Hours) || math.IsInf(*entry.Hours, 0) {
				return errors.New("invalid entry duration")
			}
		}
		entries = append(entries, *result.Entries...)
		return nil
	})
	return entries, err
}

// harvestFeed answers a feed request: the user's time entries spent on date,
// each linked to its Asana ticket, and the day's total as the summary.
func harvestFeed(ctx context.Context, configPath, endpoint, date string) (feed.Reply, error) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return feed.Reply{}, errors.New("hrvst feed needs date as YYYY-MM-DD")
	}
	entries, err := harvestUserEntries(ctx, configPath, endpoint, date, "Harvest feed")
	if err != nil {
		return feed.Reply{}, err
	}
	reply := feed.Reply{Source: "hrvst", Icon: "clock", Items: []feed.Item{}}
	var total float64
	for _, entry := range entries {
		if entry.CreatedAt.IsZero() {
			return feed.Reply{}, errors.New("Harvest feed: invalid entry time")
		}
		total += *entry.Hours
		item := feed.Item{Time: entry.CreatedAt, Text: hoursLabel(*entry.Hours) + " · " + entry.Project.Name, Tone: feed.ToneOK}
		if entry.Running {
			item.Text, item.Tone = "running · "+item.Text, feed.ToneWarn
		}
		// A linked entry's notes are its ticket's title, kept for tickets
		// the panel has not loaded.
		if notes := feed.OneLine(entry.Notes); notes != "" {
			item.Text += " · " + notes
		}
		if ref := entry.Reference; ref != nil && digits(ref.ID) {
			item.TicketGID, item.URL = ref.ID, ref.Permalink
		}
		reply.Items = append(reply.Items, item)
	}
	if math.IsInf(total*3600, 0) || total*3600 >= float64(math.MaxInt64) {
		return feed.Reply{}, errors.New("Harvest feed: duration overflow")
	}
	reply.Summary = durationLabel(total) + " logged"
	return reply, nil
}

// harvestClientsToday is the form's info line: hours spent today per
// client, most first, running timers included as in the feed. Failure only
// costs the line, never the form.
func harvestClientsToday(ctx context.Context, configPath, endpoint string) string {
	entries, err := harvestUserEntries(ctx, configPath, endpoint, time.Now().Format(time.DateOnly), "Harvest today")
	if err != nil {
		return "Today by client unavailable: " + err.Error()
	}
	hours := map[string]float64{}
	for _, entry := range entries {
		hours[cmp.Or(strings.TrimSpace(entry.Client.Name), "No client")] += *entry.Hours
	}
	if len(hours) == 0 {
		return "Today: nothing logged"
	}
	clients := slices.Collect(maps.Keys(hours))
	slices.SortFunc(clients, func(a, b string) int {
		return cmp.Or(cmp.Compare(hours[b], hours[a]), strings.Compare(a, b))
	})
	parts := make([]string, len(clients))
	for i, client := range clients {
		parts[i] = client + " " + durationLabel(hours[client])
	}
	return "Today: " + strings.Join(parts, " · ")
}

// hoursLabel shows decimal hours briefly: "1.5h", "0.25h", "2h".
func hoursLabel(hours float64) string {
	return strconv.FormatFloat(math.Round(hours*100)/100, 'f', -1, 64) + "h"
}

// durationLabel shows decimal hours as "3h 10m", "45m", or "0m".
func durationLabel(hours float64) string {
	minutes := int(math.Round(hours * 60))
	switch {
	case minutes >= 60 && minutes%60 != 0:
		return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
	case minutes >= 60:
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}
