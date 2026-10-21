package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// SettingKeys lists settings shared by the user and TUI, in file order.
// Every other key belongs exclusively to config.toml.
var SettingKeys = []string{
	"theme.name", "theme.auto.dark", "theme.auto.light",
	"list.separator", "list.header.spacing", "list.selection.style",
	"list.refresh_interval", "reader.view", "layout.focus.dim",
}

const settingsHeader = `# Settings you can also change in asanamate (s for visuals; comma for tools).
# asanamate rewrites this file when you change a setting there. It keeps
# every value, but not comments below this header.

`

// SettingsPath returns settings.toml next to the config file.
func SettingsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "settings.toml")
}

func decodeSettings(path string, data []byte) (map[string]any, error) {
	values := map[string]any{}
	md, err := toml.Decode(string(data), &values)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range md.Keys() {
		dotted := key.String()
		if slices.Contains(SettingKeys, dotted) {
			continue
		}
		v, _ := getPath(values, dotted)
		if isTable(v) && slices.ContainsFunc(SettingKeys, func(k string) bool { return strings.HasPrefix(k, dotted+".") }) {
			continue
		}
		return nil, fmt.Errorf("%s: unknown setting %s", path, dotted)
	}
	return values, nil
}

// ReadSettings reads the settings table. A missing file means defaults.
func ReadSettings(configPath string) (map[string]any, error) {
	path := SettingsPath(configPath)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return decodeSettings(path, data)
}

// EncodeSettings writes only the values present, in SettingKeys order.
func EncodeSettings(values map[string]any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(settingsHeader)
	for _, key := range SettingKeys {
		if v, ok := getPath(values, key); ok {
			var line bytes.Buffer
			if err := toml.NewEncoder(&line).Encode(map[string]any{"value": v}); err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "%s = %s", key, strings.TrimPrefix(line.String(), "value = "))
		}
	}
	data := b.Bytes()
	if _, err := decodeSettings("settings.toml", data); err != nil {
		return nil, err
	}
	return data, nil
}

// DefaultSettings renders the discoverable settings file for setup.
func DefaultSettings() []byte {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(Default()); err != nil {
		panic(err)
	}
	var values map[string]any
	if _, err := toml.Decode(b.String(), &values); err != nil {
		panic(err)
	}
	data, err := EncodeSettings(values)
	if err != nil {
		panic(err)
	}
	return data
}

// SaveSetting rereads settings, preserves other values, and atomically saves
// one validated setting with private permissions.
func SaveSetting(configPath, key string, value any) error {
	if !slices.Contains(SettingKeys, key) {
		return fmt.Errorf("unknown setting %s", key)
	}
	values, err := ReadSettings(configPath)
	if err != nil {
		return err
	}
	setPath(values, key, value)
	data, err := EncodeSettings(values)
	if err != nil {
		return err
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if _, err := LoadData(configPath, configData, data); err != nil {
		return err
	}
	return WriteSettings(configPath, data)
}

// WriteSettings atomically replaces settings.toml with previously validated data.
func WriteSettings(configPath string, data []byte) error {
	path := SettingsPath(configPath)
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
