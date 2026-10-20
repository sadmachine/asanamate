package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const tokenInstructions = `Create a personal access token at https://id.getharvest.com/developers.
Keep its token secret. Note the account ID shown there.
In the shell that starts asanamate, set:
  export HARVEST_ACCESS_TOKEN='<your personal access token>'
  export HARVEST_ACCOUNT_ID='<your account ID>'
Replace the placeholders privately, then rerun asanamate-hrvst setup.
The account export is optional: setup can prompt for and save its ID.
Do not put the token in config or command arguments. Setup never saves tokens
or edits shell startup files. Current exports do not persist into future shells.`

func checkHarvest(ctx context.Context, out io.Writer, online bool, endpoint string) error {
	settings, _, _, err := readHarvestSettings()
	if err != nil {
		return err
	}
	path, err := harvestConfigPath()
	if err != nil {
		return err
	}
	credentials, err := loadHarvestCredentials(path)
	if err != nil {
		return err
	}
	_, native, _ := nativeCredentials(settings)
	source := "legacy ~/.hrvst/config.json"
	if native {
		source = "native environment/provider settings"
	}
	fmt.Fprintf(out, "ok: %s credentials; remote access unverified by local check.\n", source)
	if settings.TaskID == "" {
		fmt.Fprintln(out, "Task readiness: no default task; feeds/summary available. Run setup for time tracking.")
	} else {
		fmt.Fprintf(out, "Task readiness: default %s configured; eligibility unverified locally.\n", settings.TaskID)
	}
	if !online {
		return nil
	}
	user, err := credentials.user(ctx, endpoint)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Verified user %s %s (ID %s), account %s.\n", ticket.Clean(user.FirstName), ticket.Clean(user.LastName), user.ID, credentials.AccountID)
	all, err := credentials.assignments(ctx, endpoint)
	if err != nil {
		return err
	}
	if settings.TaskID != "" {
		projects, task, err := all.options(settings.TaskID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Task %s (%s): eligible on %d projects.\n", ticket.Clean(task.Name), task.ID, len(projects))
	} else {
		fmt.Fprintf(out, "Read-only assignment check: %d project assignments; no task required for feed.\n", len(all))
		if len(all) == 0 {
			fmt.Fprintln(out, "No assignments; ask a Harvest admin for project access before logging time.")
		}
	}
	return nil
}

func setupHarvest(ctx context.Context, in io.Reader, out io.Writer, endpoint string) error {
	settings, path, original, err := readHarvestSettings()
	if err != nil {
		return err
	}
	before, err := snapshotHarvestSettings(path, original)
	if err != nil {
		return err
	}
	if before.file != nil {
		defer before.file.Close()
	}
	reader := bufio.NewReader(in)
	fmt.Fprintln(out, tokenInstructions)
	token, hasToken := os.LookupEnv("HARVEST_ACCESS_TOKEN")
	account, hasAccount := os.LookupEnv("HARVEST_ACCOUNT_ID")
	if !hasAccount {
		account = settings.AccountID
	}
	var credentials harvestCredentials
	legacy := false
	if !hasToken && !hasAccount && settings.AccountID == "" {
		legacyPath, err := harvestConfigPath()
		if err != nil {
			return err
		}
		saved, err := loadHarvestCredentials(legacyPath)
		if err == nil {
			keep, err := prompt.Confirm(reader, out, "Retain your saved legacy Harvest login (no token or native account saved)?")
			if err != nil {
				return err
			}
			if keep {
				credentials = saved
				legacy = true
			}
		}
	}
	if !legacy {
		if !validToken(token) {
			return errors.New("HARVEST_ACCESS_TOKEN absent or invalid; export it and rerun setup")
		}
		if account == "" && !hasAccount {
			account, err = prompt.Line(reader, out, "Harvest account ID", "")
			if err != nil {
				return err
			}
		}
		if !positiveID(account) {
			return errors.New("HARVEST_ACCOUNT_ID must be a positive numeric ID")
		}
		credentials = harvestCredentials{AccessToken: token, AccountID: account}
		settings.AccountID = account
	}
	user, err := credentials.user(ctx, endpoint)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Verified user %s %s (ID %s), account %s.\n", ticket.Clean(user.FirstName), ticket.Clean(user.LastName), user.ID, credentials.AccountID)
	capability, err := prompt.Line(reader, out, "Set up 1) time tracking, 2) feed, 3) both", "3")
	if err != nil {
		return err
	}
	if capability != "1" && capability != "2" && capability != "3" {
		return errors.New("choose 1, 2, or 3; settings unchanged")
	}
	tracking, feeds := capability != "2", capability != "1"
	if tracking {
		all, err := credentials.assignments(ctx, endpoint)
		if err != nil {
			return err
		}
		// IDs identify unique tasks even when names collide. options counts projects.
		names := map[string]string{}
		for _, a := range all {
			if a.IsActive == nil || !*a.IsActive {
				continue
			}
			for _, task := range a.TaskAssignments {
				if task.IsActive != nil && *task.IsActive {
					names[task.Task.ID.String()] = task.Task.Name
				}
			}
		}
		ids := make([]string, 0, len(names))
		for id := range names {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if names[ids[i]] == names[ids[j]] {
				return ids[i] < ids[j]
			}
			return names[ids[i]] < names[ids[j]]
		})
		if len(ids) == 0 {
			return errors.New("Harvest lists no active tasks; ask an admin for project/task assignments")
		}
		def := ""
		for i, id := range ids {
			projects, _, err := all.options(id)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%d) %s (ID %s; %d eligible projects)\n", i+1, ticket.Clean(names[id]), id, len(projects))
			if id == settings.TaskID {
				def = strconv.Itoa(i + 1)
			}
		}
		choice, err := prompt.Line(reader, out, "Choose task number (blank keeps existing selection when shown)", def)
		if err != nil {
			return err
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index < 1 || index > len(ids) {
			return errors.New("invalid task selection; settings unchanged")
		}
		settings.TaskID = ids[index-1]
	}
	fmt.Fprintf(out, "Save nonsecret defaults to %s: account_id=%q, task_id=%q.\n", path, settings.AccountID, settings.TaskID)
	if legacy {
		fmt.Fprintln(out, "Keep legacy credential source; no native account or token will be saved.")
	}
	ok, err := prompt.Confirm(reader, out, "Save these provider settings?")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("setup cancelled; settings unchanged")
	}
	backup, changed, err := saveHarvestSettings(path, before, settings)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintln(out, "Saved provider settings.")
	} else {
		fmt.Fprintln(out, "Provider settings unchanged.")
	}
	if backup != "" {
		fmt.Fprintf(out, "Previous settings: %s\n", backup)
	}
	fmt.Fprintln(out, "Open core config with asanamate config; see asanamate extensions harvest. Merge into existing tables; never duplicate a TOML table or replace a custom provider accidentally.")
	if tracking {
		fmt.Fprintln(out, `[time_tracking]
id = "hrvst"
command = "asanamate-hrvst"`)
	}
	if feeds {
		fmt.Fprintln(out, `Add "asanamate-hrvst" to existing feed.commands, preserving other commands. If no [feed] table exists:
[feed]
commands = ["asanamate-hrvst"]`)
	}
	fmt.Fprintln(out, `Existing command = "asanamate-hrvst --task-id ID" keeps its explicit task. To adopt the saved default, change it to command = "asanamate-hrvst" yourself.`)
	if !legacy {
		fmt.Fprintln(out, "Keep HARVEST_ACCESS_TOKEN exported wherever asanamate starts. Setup does not make it available in future shells.")
	}
	if tracking {
		fmt.Fprintln(out, "Verify: restart asanamate, press t on a ticket, inspect the form, then Esc without submitting. Creating an entry is a separate explicit action.")
	}
	if feeds {
		fmt.Fprintln(out, "Verify: restart asanamate and check today's Harvest feed.")
	}
	return nil
}

// harvestSnapshot keeps the original inode alive throughout prompting. FileInfo
// alone can miss a replacement when a filesystem rapidly recycles inode numbers.
type harvestSnapshot struct {
	file *os.File
	info os.FileInfo
	data []byte
}

func snapshotHarvestSettings(path string, original []byte) (harvestSnapshot, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && original == nil {
		return harvestSnapshot{}, nil
	}
	if err != nil {
		return harvestSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return harvestSnapshot{}, errors.New("provider settings must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return harvestSnapshot{}, err
	}
	opened, statErr := file.Stat()
	data, readErr := io.ReadAll(file)
	if statErr != nil || readErr != nil || !os.SameFile(info, opened) || !info.ModTime().Equal(opened.ModTime()) || !bytes.Equal(data, original) {
		file.Close()
		return harvestSnapshot{}, errors.New("provider settings changed while reading; rerun setup")
	}
	return harvestSnapshot{file: file, info: opened, data: data}, nil
}

// saveHarvestSettings serializes cooperating setup writers, detects edits since
// prompting, and stages a private replacement before preserving the old file.
func saveHarvestSettings(path string, before harvestSnapshot, settings harvestSettings) (string, bool, error) {
	var encoded bytes.Buffer
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", false, errors.New("provider settings locked; another setup may be running")
	}
	defer os.Remove(lock.Name())
	defer lock.Close()
	unchanged := func() bool {
		current, err := os.Lstat(path)
		if before.file == nil {
			return os.IsNotExist(err)
		}
		if _, statErr := before.file.Stat(); statErr != nil {
			return false // A closed snapshot no longer protects the inode identity.
		}
		if err != nil || !current.Mode().IsRegular() || !os.SameFile(before.info, current) || !before.info.ModTime().Equal(current.ModTime()) {
			return false
		}
		data, err := os.ReadFile(path)
		return err == nil && bytes.Equal(data, before.data)
	}
	if !unchanged() {
		return "", false, errors.New("provider settings changed during setup; rerun setup")
	}
	var existing harvestSettings
	if before.file != nil {
		if _, err := toml.Decode(string(before.data), &existing); err == nil && existing == settings {
			return "", false, nil
		}
	} else if settings == (harvestSettings{}) {
		return "", false, nil
	}
	// Omit empty keys: absence is valid but an explicitly empty ID is not.
	values := map[string]string{}
	if settings.AccountID != "" {
		values["account_id"] = settings.AccountID
	}
	if settings.TaskID != "" {
		values["task_id"] = settings.TaskID
	}
	encoded.Reset()
	if err := toml.NewEncoder(&encoded).Encode(values); err != nil {
		return "", false, err
	}
	staged, err := os.CreateTemp(filepath.Dir(path), ".harvest-*.toml")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(staged.Name())
	if _, err := staged.Write(encoded.Bytes()); err != nil {
		staged.Close()
		return "", false, err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return "", false, err
	}
	if err := staged.Close(); err != nil {
		return "", false, err
	}
	backup := ""
	if before.file != nil {
		backup = path + time.Now().Format(".2006-01-02--15-04-05.bak")
		file, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", false, err
		}
		_, writeErr := file.Write(before.data)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return backup, false, err
		}
	}
	if !unchanged() {
		return backup, false, errors.New("provider settings changed during setup; settings kept")
	}
	if err := os.Rename(staged.Name(), path); err != nil {
		return backup, false, err
	}
	return backup, true, nil
}
