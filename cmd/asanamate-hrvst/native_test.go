package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeProviderSettings(t *testing.T, data string) string {
	t.Helper()
	_, path, _, err := readHarvestSettings()
	if err != nil && path == "" {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func nativeTestEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HARVEST_ACCESS_TOKEN", "native-secret")
	t.Setenv("HARVEST_ACCOUNT_ID", "789")
	t.Setenv("PATH", t.TempDir()) // Neither Node nor hrvst-cli is available.
}

func TestNativeCredentialsPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, settings         string
		token, account         *string
		wantToken, wantAccount string
		wantError              bool
	}{
		{name: "legacy", wantToken: "legacy-secret", wantAccount: "123"},
		{name: "native", token: ptr("native-secret"), account: ptr("789"), wantToken: "native-secret", wantAccount: "789"},
		{name: "saved account", token: ptr("native-secret"), settings: `account_id = "789"`, wantToken: "native-secret", wantAccount: "789"},
		{name: "environment account wins", token: ptr("native-secret"), account: ptr("789"), settings: `account_id = "456"`, wantToken: "native-secret", wantAccount: "789"},
		{name: "saved task allows legacy", settings: `task_id = "456"`, wantToken: "legacy-secret", wantAccount: "123"},
		{name: "token only never mixes", token: ptr("native-secret"), wantError: true},
		{name: "account only never falls back", account: ptr("789"), wantError: true},
		{name: "saved account never falls back", settings: `account_id = "789"`, wantError: true},
		{name: "empty token supplied", token: ptr(""), wantError: true},
		{name: "empty account supplied", token: ptr("native-secret"), account: ptr(""), settings: `account_id = "789"`, wantError: true},
		{name: "invalid account", token: ptr("native-secret"), account: ptr("0"), wantError: true},
		{name: "malformed settings blocks legacy", settings: `task_id = 456`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tc.token != nil {
				t.Setenv("HARVEST_ACCESS_TOKEN", *tc.token)
			}
			if tc.account != nil {
				t.Setenv("HARVEST_ACCOUNT_ID", *tc.account)
			}
			legacy := filepath.Join(t.TempDir(), "legacy.json")
			if err := os.WriteFile(legacy, []byte(`{"accessToken":"legacy-secret","accountId":"123"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.settings != "" {
				writeProviderSettings(t, tc.settings)
			}
			credentials, err := loadHarvestCredentials(legacy)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v", err, tc.wantError)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("credential leaked")
				}
			} else if credentials.AccessToken != tc.wantToken || credentials.AccountID != tc.wantAccount {
				t.Fatal("wrong credential source")
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestHarvestSettingsValidation(t *testing.T) {
	for _, tc := range []struct{ data, key string }{
		{`unknown = "123"`, "unknown"}, {`[extra]` + "\n" + `key = "123"`, "extra"},
		{`account_id = "0"`, "account_id"}, {`task_id = "-1"`, "task_id"},
		{`task_id = ""`, "task_id"}, {`task_id = "abc"`, "task_id"},
		{`account_id = 123`, "TOML"}, {`not TOML`, "TOML"},
	} {
		t.Run(tc.data, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := writeProviderSettings(t, tc.data)
			_, _, _, err := readHarvestSettings()
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())
	settings, path, data, err := readHarvestSettings()
	if err != nil || settings != (harvestSettings{}) || data != nil || path != filepath.Join(os.Getenv("HOME"), ".config", "asanamate", "harvest.toml") {
		t.Fatalf("missing defaults = %+v %s %v", settings, path, err)
	}
}

func TestNativeAssignmentsPaginationAndEligibility(t *testing.T) {
	nativeTestEnvironment(t)
	var pages []string
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			t.Error("unexpected write")
			return
		}
		if r.URL.Path != "/v2/users/me/project_assignments" || r.Header.Get("Authorization") != "Bearer native-secret" || r.Header.Get("Harvest-Account-ID") != "789" {
			t.Errorf("wrong path or credentials: %s", r.URL.Path)
		}
		pages = append(pages, r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprintf(w, `{"project_assignments":%s,"next_page":2}`, assignmentJSON)
		} else {
			fmt.Fprint(w, `{"project_assignments":[{"is_active":true,"project":{"id":222,"name":"Second"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]}],"next_page":null}`)
		}
	}))
	defer server.Close()
	all, err := harvestAssignments(context.Background(), "", server.URL+"/v2/time_entries")
	if err != nil {
		t.Fatal(err)
	}
	projects, task, err := all.options("456")
	if err != nil || len(projects) != 2 || task.Name != "Development" || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("projects=%+v task=%+v pages=%v err=%v", projects, task, pages, err)
	}
	request := `{"operation":"log","values":{"project_id":"999","task_id":"456","hours":"1.25","spent_date":"2026-10-05"},"asana":{"task_gid":"1","project_gid":"2","url":"https://app.asana.com/0/2/1"}}`
	for _, project := range []string{"999", "321", "777"} {
		var out bytes.Buffer
		err := runHarvest(context.Background(), strings.NewReader(strings.Replace(request, `"999"`, fmt.Sprintf("%q", project), 1)), &out, "456", "", server.URL+"/v2/time_entries")
		if err == nil || !strings.Contains(err.Error(), "inactive or unassigned") || posts.Load() != 0 {
			t.Fatalf("project %s: %v posts=%d", project, err, posts.Load())
		}
	}
}

func TestAssignmentsRejectInvalidOrPartialPages(t *testing.T) {
	nativeTestEnvironment(t)
	for _, response := range []string{
		`bad`, `{}`, `{"project_assignments":null,"next_page":null}`, `{"project_assignments":[],"next_page":1}`,
		`{"project_assignments":[],"next_page":3}`, `{"project_assignments":[]}`,
		`{"project_assignments":[{"project":{"id":1,"name":"Web"},"task_assignments":[]}],"next_page":null}`,
		`{"project_assignments":[{"is_active":true,"project":{"id":1,"name":"Web"},"task_assignments":[{"is_active":true,"task":{"id":0,"name":"Bad"}}]}],"next_page":null}`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "1" {
					fmt.Fprintf(w, `{"project_assignments":%s,"next_page":2}`, assignmentJSON)
				} else {
					fmt.Fprint(w, response)
				}
			}))
			defer server.Close()
			all, err := harvestAssignments(context.Background(), "", server.URL)
			if err == nil || all != nil {
				t.Fatalf("partial success: %d assignments, %v", len(all), err)
			}
		})
	}
}

func TestTaskOverrideAndLocalOnlineChecks(t *testing.T) {
	nativeTestEnvironment(t)
	writeProviderSettings(t, `task_id = "8"`)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Error("check wrote remotely")
		}
		if r.URL.Path == "/users/me" {
			fmt.Fprint(w, `{"id":55,"first_name":"Test","last_name":"User"}`)
		} else {
			fmt.Fprintf(w, `{"project_assignments":%s,"next_page":null}`, assignmentJSON)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := runHarvest(context.Background(), strings.NewReader(`{"operation":"form"}`), &out, "456", "", server.URL); err != nil || !strings.Contains(out.String(), "Development") {
		t.Fatalf("override failed: %v", err)
	}
	out.Reset()
	calls.Store(0)
	if err := checkHarvest(context.Background(), &out, false, server.URL); err != nil || calls.Load() != 0 || !strings.Contains(out.String(), "unverified") {
		t.Fatalf("local check: %v calls=%d", err, calls.Load())
	}
	if err := checkHarvest(context.Background(), &out, true, server.URL); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("online eligibility: %v", err)
	}
	writeProviderSettings(t, "")
	out.Reset()
	if err := checkHarvest(context.Background(), &out, true, server.URL); err != nil || !strings.Contains(out.String(), "Verified user Test User") || !strings.Contains(out.String(), "no default task") {
		t.Fatalf("feed-only check: %v output=%s", err, out.String())
	}
}

func TestHTTPFailureConfidentialityAndNoRetries(t *testing.T) {
	credentials := harvestCredentials{AccessToken: "secret-sentinel", AccountID: "789"}
	for _, status := range []int{401, 403, 404, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, credentials.AccessToken)
			}))
			defer server.Close()
			_, err := credentials.call(context.Background(), http.MethodPost, server.URL, strings.NewReader("{}"))
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), credentials.AccessToken) || calls.Load() != 1 {
				t.Fatalf("status %d: %v calls=%d", status, err, calls.Load())
			}
		})
	}
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err := credentials.call(context.Background(), http.MethodGet, redirect.URL, nil); err == nil || forwarded.Load() {
		t.Fatal("redirect followed")
	}
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", (1<<20)+1)) }))
	defer oversized.Close()
	if _, err := credentials.call(context.Background(), http.MethodGet, oversized.URL, nil); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("size limit: %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := credentials.call(ctx, http.MethodGet, slow.URL, nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := credentials.call(ctx, http.MethodGet, slow.URL, nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation: %v", err)
	}
}
