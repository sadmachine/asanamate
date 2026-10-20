package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sadmachine/asanamate/internal/config"
)

// harvestSettings contains nonsecret provider defaults. Missing files and keys
// leave defaults empty; a task is needed only for form/log operations.
type harvestSettings struct {
	AccountID string `toml:"account_id"`
	TaskID    string `toml:"task_id"`
}

func positiveID(id string) bool {
	return digits(id) && strings.TrimLeft(id, "0") != ""
}

func validToken(token string) bool {
	return strings.TrimSpace(token) != "" && !strings.ContainsAny(token, "\r\n")
}

func readHarvestSettings() (harvestSettings, string, []byte, error) {
	dir, err := config.Dir()
	if err != nil {
		return harvestSettings{}, "", nil, err
	}
	path := filepath.Join(dir, "harvest.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return harvestSettings{}, path, nil, nil
	}
	if err != nil {
		return harvestSettings{}, path, nil, fmt.Errorf("%s: cannot read provider settings", path)
	}
	var settings harvestSettings
	meta, err := toml.Decode(string(data), &settings)
	if err != nil {
		return settings, path, data, fmt.Errorf("%s: invalid TOML or ID type (IDs must be quoted strings)", path)
	}
	if keys := meta.Undecoded(); len(keys) != 0 {
		return settings, path, data, fmt.Errorf("%s: unknown key %s", path, keys[0])
	}
	for _, key := range []string{"account_id", "task_id"} {
		value := settings.AccountID
		if key == "task_id" {
			value = settings.TaskID
		}
		if meta.IsDefined(key) && !positiveID(value) {
			return settings, path, data, fmt.Errorf("%s: %s must be a positive numeric ID", path, key)
		}
	}
	return settings, path, data, nil
}

func nativeCredentials(settings harvestSettings) (harvestCredentials, bool, error) {
	token, hasToken := os.LookupEnv("HARVEST_ACCESS_TOKEN")
	account, hasAccount := os.LookupEnv("HARVEST_ACCOUNT_ID")
	if !hasAccount {
		account = settings.AccountID
	}
	native := hasToken || hasAccount || settings.AccountID != ""
	if !native {
		return harvestCredentials{}, false, nil
	}
	if !validToken(token) || !positiveID(account) {
		return harvestCredentials{}, true, errors.New("incomplete or invalid native Harvest credentials; export HARVEST_ACCESS_TOKEN and a positive HARVEST_ACCOUNT_ID (or save account_id with asanamate-hrvst setup); legacy login will not be used")
	}
	return harvestCredentials{AccessToken: token, AccountID: account}, true, nil
}
