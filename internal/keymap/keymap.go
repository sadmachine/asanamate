// Package keymap is the catalog of TUI key bindings: their scopes, names,
// default keys, and help text. config validates [keys] overrides against
// it, and the TUI runs a handler per binding name.
package keymap

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Binding is one named key binding and its default keys.
type Binding struct {
	Name string
	Keys []string
	// Desc is the key help text and the template comment.
	Desc string
	// Hint is the short statusline hint; empty uses Desc.
	Hint string
	// Group is the key help column of a main binding: Move, Ticket, or View.
	Group string
	// Pair names the binding whose help row this one shares, such as
	// half_page_up with half_page_down.
	Pair string
	// Typing bindings run while a text field has focus, where printable
	// keys are typed instead, so they need a key that types nothing.
	Typing bool
}

// Scope is a set of bindings one key handler matches against. Keys must be
// unique within a scope.
type Scope struct {
	Name, Doc string
	Bindings  []Binding
}

// Catalog returns every scope, in template order. Callers must not modify it.
func Catalog() []Scope { return catalog }

// Keymap holds the resolved keys: scope, then binding name, then keys.
type Keymap map[string]map[string][]string

// Default returns the default keys.
func Default() Keymap {
	k, err := Resolve(nil)
	if err != nil {
		panic(err)
	}
	return k
}

// Keys returns the keys bound to name in scope.
func (k Keymap) Keys(scope, name string) []string { return k[scope][name] }

// Name returns the binding key runs in scope, or "" when none.
func (k Keymap) Name(scope, key string) string {
	if a, ok := aliases[key]; ok {
		key = a
	}
	for name, keys := range k[scope] {
		if slices.Contains(keys, key) {
			return name
		}
	}
	return ""
}

// Resolve applies overrides to the defaults and validates the result. Errors
// name the dotted key, such as keys.main.quit.
func Resolve(overrides map[string]map[string][]string) (Keymap, error) {
	for _, scope := range sortedKeys(overrides) {
		i := slices.IndexFunc(catalog, func(s Scope) bool { return s.Name == scope })
		if i < 0 {
			return nil, fmt.Errorf("keys.%s: unknown scope (use %s)", scope, strings.Join(scopeNames(), ", "))
		}
		for _, name := range sortedKeys(overrides[scope]) {
			if !slices.ContainsFunc(catalog[i].Bindings, func(b Binding) bool { return b.Name == name }) {
				return nil, fmt.Errorf("keys.%s.%s: unknown binding", scope, name)
			}
		}
	}
	k := Keymap{}
	for _, s := range catalog {
		k[s.Name] = map[string][]string{}
		owner := map[string]string{}
		for _, b := range s.Bindings {
			raw, ok := overrides[s.Name][b.Name]
			if !ok {
				raw = b.Keys
			}
			keys := []string{}
			for _, r := range raw {
				key, err := Normalize(r)
				if err != nil {
					return nil, fmt.Errorf("keys.%s.%s: %w", s.Name, b.Name, err)
				}
				if prev, ok := owner[key]; ok && prev != b.Name {
					return nil, fmt.Errorf("keys.%s: %q is bound to both %s and %s", s.Name, r, prev, b.Name)
				}
				owner[key] = b.Name
				if !slices.Contains(keys, key) {
					keys = append(keys, key)
				}
			}
			if b.Typing && len(keys) > 0 && !slices.ContainsFunc(keys, func(key string) bool { return !Printable(key) }) {
				return nil, fmt.Errorf("keys.%s.%s: works while typing, so it needs a key that types nothing, such as esc or ctrl+x", s.Name, b.Name)
			}
			k[s.Name][b.Name] = keys
		}
	}
	return k, nil
}

// modifiers in the order Bubble Tea writes them.
var modifiers = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// names are the key names Bubble Tea reports besides single characters.
var names = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields("enter tab backspace esc space up down left right home end pgup pgdown insert delete begin menu") {
		m[n] = true
	}
	for i := 1; i <= 24; i++ {
		m[fmt.Sprintf("f%d", i)] = true
	}
	return m
}()

// aliases map keys some terminals report to the key they mean: legacy
// terminals send ctrl+/ as 0x1F, which decodes as ctrl+_.
var aliases = map[string]string{"ctrl+_": "ctrl+/"}

// Normalize returns key the way Bubble Tea reports it.
func Normalize(key string) (string, error) {
	switch {
	case key == "":
		return "", errors.New("key must not be empty")
	case strings.TrimSpace(key) == "":
		return "", errors.New(`use "space" for the space bar`)
	case strings.Contains(key, " "):
		return "", fmt.Errorf("key sequences such as %q are not supported yet", key)
	}
	base, mods := key, []string(nil)
	if i := strings.LastIndex(key[:len(key)-1], "+"); i >= 0 {
		base, mods = key[i+1:], strings.Split(key[:i], "+")
	}
	seen := map[string]bool{}
	for _, m := range mods {
		if !slices.Contains(modifiers, m) {
			return "", fmt.Errorf("unknown modifier %q (use %s)", m, strings.Join(modifiers, ", "))
		}
		if seen[m] {
			return "", fmt.Errorf("key %q repeats %q", key, m)
		}
		seen[m] = true
	}
	r, size := utf8.DecodeRuneInString(base)
	switch {
	case size == len(base) && unicode.IsPrint(r):
		if len(seen) == 1 && seen["shift"] && unicode.IsLower(r) {
			return string(unicode.ToUpper(r)), nil
		}
	case !names[base]:
		return "", fmt.Errorf("unknown key %q", base)
	}
	var out []string
	for _, m := range modifiers {
		if seen[m] {
			out = append(out, m)
		}
	}
	normalized := strings.Join(append(out, base), "+")
	if a, ok := aliases[normalized]; ok {
		normalized = a
	}
	return normalized, nil
}

// Printable reports whether a normalized key types text.
func Printable(key string) bool {
	if key == "space" {
		return true
	}
	r, size := utf8.DecodeRuneInString(key)
	return size == len(key) && unicode.IsPrint(r)
}

// glyphs are how hints show the arrow keys.
var glyphs = map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→"}

// Label joins keys for display, drawing arrows as glyphs.
func Label(keys ...string) string {
	shown := make([]string, len(keys))
	for i, k := range keys {
		shown[i] = k
		if g, ok := glyphs[k]; ok {
			shown[i] = g
		}
	}
	return strings.Join(shown, "/")
}

func scopeNames() []string {
	var out []string
	for _, s := range catalog {
		out = append(out, s.Name)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
