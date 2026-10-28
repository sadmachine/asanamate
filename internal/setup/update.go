package setup

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/prompt"
)

var (
	headerLine    = regexp.MustCompile(`^\[([a-z_.]+)\]$`)
	keyLine       = regexp.MustCompile(`^(# )?([a-z_]+) = `)
	workspaceName = regexp.MustCompile(`(?m)^# Asana workspace: (.+)$`)
)

// Update rewrites the config file from the current template, keeping every
// value the user set. The previous file is saved next to it, see backupPath.
func Update(o Options, yes bool) error {
	old, err := os.ReadFile(o.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return config.ErrNotConfigured
	}
	if err != nil {
		return err
	}
	updated, added, moved, err := Merge(string(old))
	if err != nil {
		return fmt.Errorf("fix the config before updating it: %w", err)
	}
	settingsPath := config.SettingsPath(o.ConfigPath)
	oldSettings, err := os.ReadFile(settingsPath)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	existing, err := config.ReadSettings(o.ConfigPath)
	if err != nil {
		return err
	}
	if missing {
		var defaults map[string]any
		if _, err := toml.Decode(string(config.DefaultSettings()), &defaults); err != nil {
			return err
		}
		overlay(defaults, moved)
		moved = defaults
	}
	overlay(moved, existing) // Existing settings are the user's current choices.
	settings, err := config.EncodeSettings(moved)
	if err != nil {
		return err
	}
	if _, err := config.LoadData(o.ConfigPath, []byte(updated), settings); err != nil {
		return fmt.Errorf("fix the config before updating it: %w", err)
	}
	configChanged := updated != string(old)
	settingsChanged := missing || string(settings) != string(oldSettings)
	if !configChanged && !settingsChanged {
		fmt.Fprintln(o.Out, "Config is already up to date.")
		return writeFiles(config.ThemesDir(o.ConfigPath), themeFiles)
	}
	if len(added) > 0 {
		fmt.Fprintf(o.Out, "New settings with their defaults: %s\n", strings.Join(added, ", "))
	}
	if !yes {
		ok, err := prompt.Confirm(o.In, o.Out, fmt.Sprintf("Rewrite %s and %s with the current templates, keeping your values and saving timestamped backups?", o.ConfigPath, settingsPath))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("update cancelled; config unchanged")
		}
	}
	backup := backupPath(o.ConfigPath)
	if configChanged {
		if err := backupPrivate(backup, old); err != nil {
			return err
		}
	}
	if settingsChanged && !missing {
		backup := backupPath(settingsPath)
		if err := backupPrivate(backup, oldSettings); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "Previous settings saved as %s.\n", backup)
	}
	if settingsChanged {
		if err := config.WriteSettings(o.ConfigPath, settings); err != nil {
			return err
		}
	}
	if configChanged {
		if err := replacePrivate(o.ConfigPath, []byte(updated)); err != nil {
			var rollback error
			if settingsChanged {
				if missing {
					rollback = os.Remove(settingsPath)
				} else {
					rollback = config.WriteSettings(o.ConfigPath, oldSettings)
				}
			}
			return errors.Join(err, rollback)
		}
		fmt.Fprintf(o.Out, updateSummary, o.ConfigPath, backup)
	} else {
		fmt.Fprintf(o.Out, "Updated %s.\n", settingsPath)
	}
	if err := writeFiles(config.ThemesDir(o.ConfigPath), themeFiles); err != nil {
		return err
	}
	_, err = config.Load(o.ConfigPath)
	return err
}

// overlay copies values from newer, retaining other nested keys in older.
func overlay(older, newer map[string]any) {
	for key, value := range newer {
		dst, dstTable := older[key].(map[string]any)
		src, srcTable := value.(map[string]any)
		if dstTable && srcTable {
			overlay(dst, src)
		} else {
			older[key] = value
		}
	}
}

func backupPrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Close())
}

func replacePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
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

const updateSummary = `Updated %s; your values are kept.
The previous file is saved as %s. Comments are now the current
defaults, so copy any of your own back from the backup.
`

// Merge renders the current template with the values set in old: template
// keys take the user's value when set (commented-out keys are uncommented),
// and tables the template doesn't define, such as agents, are appended. It
// returns the new config, new default keys, and settings extracted from old.
func Merge(old string) (string, []string, map[string]any, error) {
	var user map[string]any
	md, err := toml.Decode(old, &user)
	if err != nil {
		return "", nil, nil, err
	}
	moved, err := config.ApplyUpdates(user)
	if err != nil {
		return "", nil, nil, err
	}
	gid, _ := user["workspace"].(string)
	name := gid
	if m := workspaceName.FindStringSubmatch(old); m != nil {
		name = m[1]
	}
	var out, added []string
	used := map[string]bool{} // dotted keys taken from the user's file
	live := map[string]bool{} // tables the template defines
	var table []string
	for _, line := range strings.Split(Render(asana.Ref{GID: gid, Name: name}, ""), "\n") {
		if m := headerLine.FindStringSubmatch(line); m != nil {
			table = strings.Split(m[1], ".")
			live[m[1]] = true
			out = append(out, line)
			continue
		}
		if m := keyLine.FindStringSubmatch(line); m != nil {
			key := append(slices.Clone(table), m[2])
			dotted := strings.Join(key, ".")
			// Moved keys are not in the file's metadata, so presence comes
			// from the migrated map.
			if v, ok := lookupOK(user, key); ok {
				v, err := formatValue(v)
				if err != nil {
					return "", nil, nil, fmt.Errorf("%s: %w", dotted, err)
				}
				out = append(out, m[2]+" = "+v)
				used[dotted] = true
				continue
			}
			if m[1] == "" {
				added = append(added, dotted)
			}
		}
		out = append(out, line)
	}

	var b strings.Builder
	b.WriteString(strings.Join(out, "\n"))
	for _, k := range childOrder(md, nil, user) {
		v := user[k]
		if t, ok := v.(map[string]any); ok && live[k] {
			if err := checkUsed(t, []string{k}, used); err != nil {
				return "", nil, nil, err
			}
			continue
		}
		if used[k] {
			continue
		}
		if tables, ok := v.([]map[string]any); ok {
			if err := writeTableArray(&b, k, tables); err != nil {
				return "", nil, nil, err
			}
			continue
		}
		if !isTable(v) {
			return "", nil, nil, fmt.Errorf("the template has no place for %s", k)
		}
		if err := writeTable(&b, md, []string{k}, v); err != nil {
			return "", nil, nil, err
		}
	}

	result := b.String()
	var got map[string]any
	if _, err := toml.Decode(result, &got); err != nil {
		return "", nil, nil, fmt.Errorf("updated config is not valid TOML: %w", err)
	}
	if !contains(got, user) {
		return "", nil, nil, errors.New("updated config would change your values; config unchanged")
	}
	return result, added, moved, nil
}

// backupPath names a backup of path stamped with the current time, such as
// config.toml.2026-10-06--14-30-05.bak, so backups don't overwrite each other.
func backupPath(path string) string {
	return path + "." + time.Now().Format("2006-01-02--15-04-05") + ".bak"
}

func writePrivate(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// lookupOK returns the value at key and whether m sets it.
func lookupOK(m map[string]any, key []string) (any, bool) {
	var v any = m
	for _, k := range key {
		t, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = t[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

// checkUsed errors on a key in a template table that the template has no line for.
func checkUsed(t map[string]any, path []string, used map[string]bool) error {
	for k, v := range t {
		key := append(slices.Clone(path), k)
		if used[strings.Join(key, ".")] {
			continue
		}
		sub, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("the template has no place for %s", strings.Join(key, "."))
		}
		if err := checkUsed(sub, key, used); err != nil {
			return err
		}
	}
	return nil
}

func isTable(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// writeTable appends the table v under path.
func writeTable(b *strings.Builder, md toml.MetaData, path []string, v any) error {
	fmt.Fprintf(b, "\n[%s]\n", strings.Join(path, "."))
	return writeKeys(b, md, path, v.(map[string]any))
}

// tableArrayOrder is the key order writeTableArray uses, so migrated hooks
// read like the template's examples.
var tableArrayOrder = []string{"on", "when", "once", "run"}

// writeTableArray appends tables as an array of tables named name, such as
// [[hooks]], keeping their order.
func writeTableArray(b *strings.Builder, name string, tables []map[string]any) error {
	for i, t := range tables {
		fmt.Fprintf(b, "\n[[%s]]\n", name)
		keys := slices.Sorted(maps.Keys(t))
		slices.SortStableFunc(keys, func(x, y string) int {
			rank := func(k string) int {
				if i := slices.Index(tableArrayOrder, k); i >= 0 {
					return i
				}
				return len(tableArrayOrder)
			}
			return rank(x) - rank(y)
		})
		for _, k := range keys {
			v, err := formatValue(t[k])
			if err != nil {
				return fmt.Errorf("%s[%d].%s: %w", name, i+1, k, err)
			}
			fmt.Fprintf(b, "%s = %s\n", k, v)
		}
	}
	return nil
}

// writeKeys appends t's values in file order, then its sub-tables.
func writeKeys(b *strings.Builder, md toml.MetaData, path []string, t map[string]any) error {
	keys := childOrder(md, path, t)
	for _, k := range keys {
		if isTable(t[k]) {
			continue
		}
		v, err := formatValue(t[k])
		if err != nil {
			return fmt.Errorf("%s.%s: %w", strings.Join(path, "."), k, err)
		}
		fmt.Fprintf(b, "%s = %s\n", k, v)
	}
	for _, k := range keys {
		if isTable(t[k]) {
			if err := writeTable(b, md, append(slices.Clone(path), k), t[k]); err != nil {
				return err
			}
		}
	}
	return nil
}

// childOrder returns t's keys in the order the file first defines them.
func childOrder(md toml.MetaData, path []string, t map[string]any) []string {
	var keys []string
	for _, k := range md.Keys() {
		if len(k) == len(path)+1 && slices.Equal(k[:len(path)], path) && !slices.Contains(keys, k[len(path)]) {
			if _, ok := t[k[len(path)]]; ok {
				keys = append(keys, k[len(path)])
			}
		}
	}
	var rest []string
	for k := range t {
		if !slices.Contains(keys, k) {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// formatValue returns v as a TOML value, preferring the template's quoting:
// "basic" strings, then 'literal', then multi-line literal strings. Tables
// are written inline, so style values stay on their key's line.
func formatValue(v any) (string, error) {
	switch v := v.(type) {
	case string:
		if q, ok := quote(v); ok {
			return q, nil
		}
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			s, err := formatValue(v[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, k+" = "+s)
		}
		if len(parts) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			s, err := formatValue(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(map[string]any{"v": v}); err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimPrefix(b.String(), "v = "), "\n"), nil
}

func quote(s string) (string, bool) {
	if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' || r == 0x7f }) {
		return "", false
	}
	oneLine := !strings.Contains(s, "\n")
	switch {
	case oneLine && !strings.ContainsAny(s, `"\`):
		return `"` + s + `"`, true
	case oneLine && !strings.Contains(s, "'"):
		return "'" + s + "'", true
	case !strings.Contains(s, "'''") && !strings.HasSuffix(s, "'"):
		if strings.HasPrefix(s, "\n") {
			s = "\n" + s // TOML drops a newline right after the opening '''
		}
		return "'''" + s + "'''", true
	}
	return "", false
}

// contains reports whether got holds every value in want.
func contains(got, want map[string]any) bool {
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			return false
		}
		wm, wIsMap := w.(map[string]any)
		gm, gIsMap := g.(map[string]any)
		if wIsMap && gIsMap {
			if !contains(gm, wm) {
				return false
			}
		} else if !reflect.DeepEqual(g, w) {
			return false
		}
	}
	return true
}
