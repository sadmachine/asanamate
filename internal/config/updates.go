package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// UpdateNeeded reports a config that uses settings an older release moved.
type UpdateNeeded struct {
	Path string
	// Changes contains one line per pending change, in migration order.
	Changes []string
}

func (u *UpdateNeeded) Error() string {
	return fmt.Sprintf("%s: %s; run `asanamate config update`", u.Path, strings.Join(u.Changes, "; "))
}

// An update both reports and applies one kind of released change. Detection
// runs the same ordered transformations on a copy, tolerating conflicts so it
// can report all pending changes. Migration rejects those conflicts.
type update func(map[string]any, map[string]any, bool) ([]string, error)

var updates = []update{updateKeys, updateCommands, updatePreset, updateThemeValues, updateSettings, updatePromptAfter}

// PendingUpdates lists all pending changes without modifying raw.
func PendingUpdates(raw map[string]any) []string {
	m := cloneTable(raw)
	var changes []string
	for _, apply := range updates {
		lines, _ := apply(m, map[string]any{}, false)
		changes = append(changes, lines...)
	}
	return changes
}

// ApplyUpdates migrates raw in order and returns settings removed from it.
func ApplyUpdates(raw map[string]any) (map[string]any, error) {
	settings := map[string]any{}
	for _, apply := range updates {
		if _, err := apply(raw, settings, true); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

func cloneTable(raw map[string]any) map[string]any {
	m := make(map[string]any, len(raw))
	for k, v := range raw {
		if t, ok := v.(map[string]any); ok {
			v = cloneTable(t)
		}
		m[k] = v
	}
	return m
}

func updateKeys(m, _ map[string]any, strict bool) ([]string, error) {
	var changes []string
	accent := any("4")
	if v, ok := m["accent_color"].(string); ok && v != "" {
		accent = v
	}
moves:
	for _, mv := range Moves {
		v, ok := getPath(m, mv.From)
		if !ok || isTable(v) {
			continue
		}
		changes = append(changes, mv.From+" moved to "+mv.To)
		if _, exists := getPath(m, mv.To); exists {
			if strict {
				return nil, fmt.Errorf("%s and %s are both set; keep only %s", mv.From, mv.To, mv.To)
			}
			deletePath(m, mv.From)
			continue
		}
		if (mv.To == "colors.pinned" || mv.To == "colors.viewing") && v == "" {
			v = accent
		}
		deletePath(m, mv.From)
		parts := strings.Split(mv.To, ".")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], ".")
			if value, exists := getPath(m, parent); exists && !isTable(value) {
				if strict {
					return nil, fmt.Errorf("%s cannot move to %s: %s is not a table", mv.From, mv.To, parent)
				}
				continue moves
			}
		}
		setPath(m, mv.To, v)
	}
	return changes, nil
}

func updateCommands(m, _ map[string]any, _ bool) ([]string, error) {
	var changes []string
	for {
		mv, value, ok := commandMovedFrom(m)
		if !ok {
			return changes, nil
		}
		changes = append(changes, fmt.Sprintf("%s: %s moved to %s", mv.Key, mv.From, mv.To))
		setPath(m, mv.Key, value)
	}
}

func updatePreset(m, _ map[string]any, strict bool) ([]string, error) {
	if _, ok := getPath(m, agentsPreset); !ok {
		return nil, nil
	}
	err := MoveAgentsPreset(m)
	if strict && err != nil {
		return nil, err
	}
	return []string{agentsPreset + " was replaced by agents.command"}, nil
}

func updateThemeValues(m, _ map[string]any, _ bool) ([]string, error) {
	var changes []string
	for _, key := range []string{"theme.name", "theme.auto.dark", "theme.auto.light"} {
		v, _ := getPath(m, key)
		if v == "dark" || v == "light" {
			name := v.(string)
			changes = append(changes, fmt.Sprintf("%s: %q is now %q", key, name, "ansi-"+name))
			setPath(m, key, "ansi-"+name)
		}
	}
	return changes, nil
}

func updateSettings(m, settings map[string]any, _ bool) ([]string, error) {
	var changes []string
	for _, key := range SettingKeys {
		if v, ok := getPath(m, key); ok {
			changes = append(changes, key+" moved to settings.toml")
			setPath(settings, key, v)
			deletePath(m, key)
		}
	}
	return changes, nil
}

// promptAfterHooks are the hooks each time_tracking.prompt_after edit became.
var promptAfterHooks = map[string]map[string]any{
	HookAssign:   {"on": HookAssign, "when": "-assignee:@me -assignee:none", "run": HookLogTime},
	HookComplete: {"on": HookComplete, "run": HookLogTime},
}

// updatePromptAfter replaces time_tracking.prompt_after with the hooks that
// open the log time form after the same edits, after any existing hooks.
func updatePromptAfter(m, _ map[string]any, strict bool) ([]string, error) {
	const key = "time_tracking.prompt_after"
	v, ok := getPath(m, key)
	if !ok {
		return nil, nil
	}
	edits, _ := v.([]any)
	var hooks []map[string]any
	switch existing := m["hooks"].(type) {
	case nil:
	case []map[string]any:
		hooks = slices.Clone(existing)
	default:
		if strict {
			return nil, errors.New("hooks must be an array of tables to replace " + key)
		}
	}
	for _, edit := range []string{HookAssign, HookComplete} {
		if slices.Contains(edits, any(edit)) {
			hooks = append(hooks, maps.Clone(promptAfterHooks[edit]))
		}
	}
	deletePath(m, key)
	if len(hooks) > 0 {
		m["hooks"] = hooks
	}
	return []string{key + " was replaced by [[hooks]] with run = \"log_time\""}, nil
}
