package setup

import (
	"errors"
	"fmt"
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
	"github.com/sadmachine/asanamate/internal/repo"
)

var (
	headerLine    = regexp.MustCompile(`^(\[\[?)([a-z_.]+)\]\]?$`)
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
	if _, err := config.Load(o.ConfigPath); err != nil {
		return fmt.Errorf("fix the config before updating it: %w", err)
	}
	updated, added, err := Merge(string(old))
	if err != nil {
		return err
	}
	if updated == string(old) {
		fmt.Fprintln(o.Out, "Config is already up to date.")
		return nil
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
// and tables the template doesn't define, such as actions and agents, are
// appended. It returns the new file and the template keys old didn't set.
func Merge(old string) (string, []string, error) {
	var user map[string]any
	md, err := toml.Decode(old, &user)
	if err != nil {
		return "", nil, err
	}
	gid, _ := user["workspace"].(string)
	name := gid
	if m := workspaceName.FindStringSubmatch(old); m != nil {
		name = m[1]
	}
	root, err := filepath.Abs(repo.ExpandHome(defaultRepoRoot))
	if err != nil {
		return "", nil, err
	}

	var out, added []string
	used := map[string]bool{} // dotted keys taken from the user's file
	live := map[string]bool{} // tables the template defines
	var table []string
	inArray, skip := false, false
	for _, line := range strings.Split(Render(asana.Ref{GID: gid, Name: name}, root), "\n") {
		if m := headerLine.FindStringSubmatch(line); m != nil {
			table, inArray = strings.Split(m[2], "."), m[1] == "[["
			if !inArray {
				live[m[2]] = true
			} else if skip = md.IsDefined(table...); skip {
				continue // the user's own array replaces the template's entries
			} else {
				added = append(added, m[2])
			}
			out = append(out, line)
			continue
		}
		if skip {
			skip = strings.TrimSpace(line) != ""
			continue // drop the entry and the blank line after it
		}
		if m := keyLine.FindStringSubmatch(line); m != nil && !inArray {
			key := append(slices.Clone(table), m[2])
			dotted := strings.Join(key, ".")
			if md.IsDefined(key...) {
				v, err := formatValue(lookup(user, key))
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

func lookup(m map[string]any, key []string) any {
	var v any = m
	for _, k := range key {
		t, _ := v.(map[string]any)
		v = t[k]
	}
	return v
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
	switch v.(type) {
	case map[string]any, []map[string]any:
		return true
	}
	return false
}

// writeTable appends v, a table or an array of tables, under path.
func writeTable(b *strings.Builder, md toml.MetaData, path []string, v any) error {
	header := strings.Join(path, ".")
	if t, ok := v.(map[string]any); ok {
		fmt.Fprintf(b, "\n[%s]\n", header)
		return writeKeys(b, md, path, t)
	}
	for _, t := range v.([]map[string]any) {
		fmt.Fprintf(b, "\n[[%s]]\n", header)
		if err := writeKeys(b, md, path, t); err != nil {
			return err
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
// "basic" strings, then 'literal', then '''multi-line literal'''.
func formatValue(v any) (string, error) {
	if s, ok := v.(string); ok {
		if q, ok := quote(s); ok {
			return q, nil
		}
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
