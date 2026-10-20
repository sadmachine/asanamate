package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setupServer(t *testing.T, assignmentCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("setup performed remote write")
			w.WriteHeader(500)
			return
		}
		switch r.URL.Path {
		case "/users/me":
			fmt.Fprint(w, `{"id":55,"first_name":"Test","last_name":"User"}`)
		case "/users/me/project_assignments":
			assignmentCalls.Add(1)
			// Same name, distinct IDs. Repeated projects count once.
			fmt.Fprintf(w, `{"project_assignments":%s,"next_page":null}`, strings.ReplaceAll(assignmentJSON, "Support", "Development"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestNativeSetupAndRepeatedSetup(t *testing.T) {
	nativeTestEnvironment(t)
	var assignmentCalls atomic.Int32
	server := setupServer(t, &assignmentCalls)
	defer server.Close()
	var out bytes.Buffer
	if err := setupHarvest(context.Background(), strings.NewReader("3\n1\ny\n"), &out, server.URL); err != nil {
		t.Fatal(err)
	}
	settings, path, data, err := readHarvestSettings()
	if err != nil || settings != (harvestSettings{AccountID: "789", TaskID: "456"}) {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
	for _, want := range []string{"Verified user Test User", "Development (ID 456; 1 eligible projects)", "Development (ID 7; 1 eligible projects)", "[time_tracking]", `command = "asanamate-hrvst"`, "preserving other commands", "--task-id ID", "future shells", "Esc without submitting"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "native-secret") || strings.Contains(string(data), "native-secret") {
		t.Fatal("token leaked")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private permissions: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "config.toml")); !os.IsNotExist(err) {
		t.Fatal("setup wrote core config")
	}
	out.Reset()
	if err := setupHarvest(context.Background(), strings.NewReader("\n\ny\n"), &out, server.URL); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(info, after) || !after.ModTime().Equal(info.ModTime()) || !strings.Contains(out.String(), "settings unchanged") {
		t.Fatal("repeated setup rewrote settings")
	}
	if backups, _ := filepath.Glob(path + ".*.bak"); len(backups) != 0 {
		t.Fatal("unchanged setup made backup")
	}
}

func TestFeedOnlySetupPromptsAccountAndPreservesTask(t *testing.T) {
	nativeTestEnvironment(t)
	os.Unsetenv("HARVEST_ACCOUNT_ID") // Setenv cleanup restores original state.
	writeProviderSettings(t, `task_id = "456"`)
	var calls atomic.Int32
	server := setupServer(t, &calls)
	defer server.Close()
	var out bytes.Buffer
	if err := setupHarvest(context.Background(), strings.NewReader("789\n2\ny\n"), &out, server.URL); err != nil {
		t.Fatal(err)
	}
	settings, path, _, err := readHarvestSettings()
	if err != nil || settings.AccountID != "789" || settings.TaskID != "456" || calls.Load() != 0 {
		t.Fatalf("feed setup=%+v calls=%d err=%v", settings, calls.Load(), err)
	}
	backups, _ := filepath.Glob(path + ".*.bak")
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
	old, _ := os.ReadFile(backups[0])
	if string(old) != `task_id = "456"` {
		t.Fatalf("backup = %s", old)
	}
	if strings.Contains(out.String(), "[time_tracking]") || strings.Contains(out.String(), "Choose task number") {
		t.Fatal("feed-only setup selected task")
	}
	// No task at all also works for a new feed-only setup.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out.Reset()
	if err := setupHarvest(context.Background(), strings.NewReader("789\n2\ny\n"), &out, server.URL); err != nil {
		t.Fatal(err)
	}
	settings, _, _, err = readHarvestSettings()
	if err != nil || settings.TaskID != "" {
		t.Fatalf("feed-only task=%q err=%v", settings.TaskID, err)
	}
}

func TestSetupCancellationAndNetworkFailureKeepSettings(t *testing.T) {
	nativeTestEnvironment(t)
	original := "# keep comments\naccount_id = \"789\"\ntask_id = \"456\"\n"
	path := writeProviderSettings(t, original)
	var calls atomic.Int32
	server := setupServer(t, &calls)
	defer server.Close()
	for _, input := range []string{"2\nn\n", "2\n", "", "4\n", "1\n999\n"} {
		var out bytes.Buffer
		if err := setupHarvest(context.Background(), strings.NewReader(input), &out, server.URL); err == nil {
			t.Fatalf("accepted %q", input)
		}
		data, _ := os.ReadFile(path)
		if string(data) != original {
			t.Fatal("cancelled/invalid setup changed file")
		}
	}
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer failed.Close()
	if err := setupHarvest(context.Background(), strings.NewReader("2\ny\n"), &bytes.Buffer{}, failed.URL); err == nil {
		t.Fatal("network failure accepted")
	}
	data, _ := os.ReadFile(path)
	backups, _ := filepath.Glob(path + ".*.bak")
	if string(data) != original || len(backups) != 0 {
		t.Fatal("failed setup changed settings/backups")
	}
}

func TestLegacySetupRetainsLoginWithoutNativeAccount(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, _ := harvestConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	login := `{"accessToken":"legacy-secret","accountId":"123"}`
	if err := os.WriteFile(path, []byte(login), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := setupServer(t, &calls)
	defer server.Close()
	var out bytes.Buffer
	if err := setupHarvest(context.Background(), strings.NewReader("y\n1\n1\ny\n"), &out, server.URL); err != nil {
		t.Fatal(err)
	}
	settings, _, data, err := readHarvestSettings()
	if err != nil || settings.AccountID != "" || settings.TaskID != "456" || strings.Contains(string(data), "account_id") {
		t.Fatalf("legacy setup=%+v err=%v", settings, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != login || strings.Contains(out.String(), "legacy-secret") {
		t.Fatal("legacy login changed or leaked")
	}
}

func TestSetupRequiresNativeTokenWithoutFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	writeProviderSettings(t, `account_id = "789"`)
	var calls atomic.Int32
	server := setupServer(t, &calls)
	defer server.Close()
	var out bytes.Buffer
	err := setupHarvest(context.Background(), strings.NewReader(""), &out, server.URL)
	if err == nil || !strings.Contains(err.Error(), "HARVEST_ACCESS_TOKEN") || calls.Load() != 0 || !strings.Contains(out.String(), "export HARVEST_ACCESS_TOKEN") {
		t.Fatalf("absent token: %v calls=%d", err, calls.Load())
	}
}

func TestSaveHarvestSettingsGuardsAndBackups(t *testing.T) {
	for _, scenario := range []string{"edit", "replace", "delete", "new file", "lock", "backup collision", "success"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "harvest.toml")
			original := []byte("# old\naccount_id = \"123\"\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := snapshotHarvestSettings(path, original)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { before.file.Close() })
			expected := string(original)
			switch scenario {
			case "edit":
				expected = `account_id = "456"`
				os.WriteFile(path, []byte(expected), 0o600)
			case "replace":
				os.Remove(path)
				os.WriteFile(path, original, 0o600)
			case "delete":
				os.Remove(path)
			case "new file":
				// The file appeared after an originally absent snapshot.
			case "lock":
				os.WriteFile(path+".lock", []byte("occupied"), 0o600)
			case "backup collision":
				// Cover the whole possible clock boundary without sleeping.
				for _, when := range []time.Time{time.Now(), time.Now().Add(time.Second)} {
					os.WriteFile(path+when.Format(".2006-01-02--15-04-05.bak"), []byte("existing backup"), 0o600)
				}
			}
			attempt := before
			if scenario == "new file" {
				attempt = harvestSnapshot{}
			}
			backup, changed, err := saveHarvestSettings(path, attempt, harvestSettings{AccountID: "789", TaskID: "456"})
			if scenario == "success" {
				if err != nil || !changed || backup == "" {
					t.Fatalf("save: %s %v %v", backup, changed, err)
				}
				data, _ := os.ReadFile(backup)
				info, _ := os.Stat(backup)
				if string(data) != string(original) || info.Mode().Perm() != 0o600 {
					t.Fatal("backup bytes/permissions wrong")
				}
			} else {
				if err == nil || changed {
					t.Fatalf("unsafe save accepted: %v", err)
				}
				data, readErr := os.ReadFile(path)
				if scenario == "delete" {
					if !os.IsNotExist(readErr) {
						t.Fatal("deleted file recreated")
					}
				} else if string(data) != expected {
					t.Fatalf("file overwritten: %s", data)
				}
				if scenario == "backup collision" {
					backups, _ := filepath.Glob(path + ".*.bak")
					for _, name := range backups {
						data, _ := os.ReadFile(name)
						if string(data) != "existing backup" {
							t.Fatal("backup overwritten")
						}
					}
				}
			}
		})
	}
}

func TestHarvestSnapshotRejectsReplacementWithSameContentsAndTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harvest.toml")
	original := []byte("account_id = \"123\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotHarvestSettings(path, original)
	if err != nil {
		t.Fatal(err)
	}
	defer before.file.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.info.ModTime(), before.info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := saveHarvestSettings(path, before, harvestSettings{AccountID: "789"}); err == nil || changed {
		t.Fatal("replacement with preserved bytes and mtime was overwritten")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("concurrently replaced settings were changed")
	}
}

func TestHarvestSnapshotRequiresTheOriginalReadAndOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harvest.toml")
	original := []byte("account_id = \"123\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotHarvestSettings(path, []byte("account_id = \"456\"\n")); err == nil {
		t.Fatal("snapshot accepted settings changed since parsing")
	}
	before, err := snapshotHarvestSettings(path, original)
	if err != nil {
		t.Fatal(err)
	}
	before.file.Close()
	if _, changed, err := saveHarvestSettings(path, before, harvestSettings{AccountID: "789"}); err == nil || changed {
		t.Fatal("save accepted a snapshot after its identity protection was closed")
	}
}
