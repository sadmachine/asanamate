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
// value the user set. The previous file is saved next to it with a .bak suffix.
func Update(o Options, yes bool) error {
	old, err := os.ReadFile(o.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return config.ErrNotConfigured
	}
	if err != nil {
		return err
	}
	updated, added, err := Merge(string(old))
	if err != nil {
		return fmt.Errorf("fix the config before updating it: %w", err)
	}
	if err := checkLoads(o.ConfigPath, updated); err != nil {
		return fmt.Errorf("fix the config before updating it: %w", err)
	}
	if updated == string(old) {
		fmt.Fprintln(o.Out, "Config is already up to date.")
		return writeFiles(config.ThemesDir(o.ConfigPath), themeFiles)
	}
	if len(added) > 0 {
		fmt.Fprintf(o.Out, "New settings with their defaults: %s\n", strings.Join(added, ", "))
	}
	backup := o.ConfigPath + ".bak"
	if !yes {
		ok, err := prompt.Confirm(o.In, o.Out, fmt.Sprintf("Rewrite %s with the current template, keeping your values (previous file saved to %s)?", o.ConfigPath, backup))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("update cancelled; config unchanged")
		}
	}
	if err := writePrivate(backup, old); err != nil {
		return err
	}
	if err := writePrivate(o.ConfigPath, []byte(updated)); err != nil {
		return err
	}
	if err := writeFiles(config.ThemesDir(o.ConfigPath), themeFiles); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, updateSummary, o.ConfigPath, backup)
	_, err = config.Load(o.ConfigPath)
	return err
}

const updateSummary = `Updated %s; your values are kept.
The previous file is saved as %s. Comments are now the current
defaults, so copy any of your own back from the backup.
`

// Merge renders the current template with the values set in old: template
// keys take the user's value when set (commented-out keys are uncommented),
// and tables the template doesn't define, such as agents, are appended. It returns the new file and the template keys old didn't set.
func Merge(old string) (string, []string, error) {
	var user map[string]any
	md, err := toml.Decode(old, &user)
	if err != nil {
		return "", nil, err
	}
	if err := config.ApplyMoves(user); err != nil {
		return "", nil, err
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
					return "", nil, fmt.Errorf("%s: %w", dotted, err)
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
				return "", nil, err
			}
			continue
		}
		if used[k] {
			continue
		}
		if !isTable(v) {
			return "", nil, fmt.Errorf("the template has no place for %s", k)
		}
		if err := writeTable(&b, md, []string{k}, v); err != nil {
			return "", nil, err
		}
	}

	result := b.String()
	var got map[string]any
	if _, err := toml.Decode(result, &got); err != nil {
		return "", nil, fmt.Errorf("updated config is not valid TOML: %w", err)
	}
	if !contains(got, user) {
		return "", nil, errors.New("updated config would change your values; config unchanged")
	}
	return result, added, nil
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

// checkLoads loads body as if it were the config at path: from a temporary
// file in the same directory, so actions and themes resolve the same way.
func checkLoads(path, body string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := config.Load(f.Name()); err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), f.Name(), path))
	}
	return nil
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
