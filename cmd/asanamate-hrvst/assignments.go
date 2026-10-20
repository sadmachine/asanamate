package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sadmachine/asanamate/internal/form"
)

type harvestAssignment struct {
	IsActive        *bool      `json:"is_active"`
	Project         harvestRef `json:"project"`
	TaskAssignments []struct {
		IsActive *bool      `json:"is_active"`
		Task     harvestRef `json:"task"`
	} `json:"task_assignments"`
}
type assignments []harvestAssignment

func harvestAssignments(ctx context.Context, configPath, endpoint string) (assignments, error) {
	credentials, err := loadHarvestCredentials(configPath)
	if err != nil {
		return nil, err
	}
	return credentials.assignments(ctx, endpoint)
}

func (credentials harvestCredentials) assignments(ctx context.Context, endpoint string) (assignments, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("invalid Harvest endpoint")
	}
	base = base.ResolveReference(&url.URL{Path: "users/me/project_assignments"})
	var all assignments
	err = eachPage(ctx, credentials, base, url.Values{}, "Harvest assignments: invalid API response", func(data []byte) error {
		var result struct {
			Assignments *assignments `json:"project_assignments"`
		}
		if json.Unmarshal(data, &result) != nil || result.Assignments == nil {
			return errors.New("missing or invalid project_assignments")
		}
		for _, a := range *result.Assignments {
			if a.IsActive == nil || !positiveID(a.Project.ID.String()) || a.Project.Name == "" || a.TaskAssignments == nil {
				return errors.New("missing project ID, name, active state, or task assignments")
			}
			for _, task := range a.TaskAssignments {
				if task.IsActive == nil || !positiveID(task.Task.ID.String()) || task.Task.Name == "" {
					return errors.New("missing task ID, name, or active state")
				}
			}
		}
		all = append(all, (*result.Assignments)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (all assignments) options(taskID string) ([]form.Option, form.Option, error) {
	seen := map[string]bool{}
	var projects []form.Option
	task := form.Option{ID: taskID}
	active := 0
	for _, a := range all {
		if a.IsActive == nil || !*a.IsActive {
			continue
		}
		active++
		for _, ta := range a.TaskAssignments {
			if ta.Task.ID.String() != taskID || ta.IsActive == nil || !*ta.IsActive {
				continue
			}
			task.Name = ta.Task.Name
			id := a.Project.ID.String()
			if !seen[id] {
				seen[id] = true
				projects = append(projects, form.Option{ID: id, Name: a.Project.Name})
			}
		}
	}
	if active == 0 {
		return nil, task, errors.New("Harvest lists no active projects for you; ask a Harvest admin to assign you to one")
	}
	if len(projects) == 0 {
		return nil, task, fmt.Errorf("Harvest task %s is not active on any of your projects; run asanamate-hrvst setup to choose an eligible task", taskID)
	}
	return projects, task, nil
}

type harvestUser struct {
	ID        json.Number `json:"id"`
	FirstName string      `json:"first_name"`
	LastName  string      `json:"last_name"`
}

func (credentials harvestCredentials) user(ctx context.Context, endpoint string) (harvestUser, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return harvestUser{}, errors.New("invalid Harvest endpoint")
	}
	data, err := credentials.call(ctx, http.MethodGet, base.ResolveReference(&url.URL{Path: "users/me"}).String(), nil)
	if err != nil {
		return harvestUser{}, err
	}
	var user harvestUser
	if json.Unmarshal(data, &user) != nil || !positiveID(user.ID.String()) || user.FirstName == "" {
		return user, errors.New("Harvest invalid API response: missing user identity")
	}
	return user, nil
}
