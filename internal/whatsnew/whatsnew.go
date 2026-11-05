// Package whatsnew reads the curated release highlights shown after an upgrade.
package whatsnew

import (
	_ "embed"
	"strings"

	"github.com/sadmachine/asanamate/internal/updatecheck"
)

//go:embed whats-new.md
var content string

const unreleased = "Unreleased"

// Section is one version's highlights. Version is "Unreleased" or "vX.Y.Z";
// Body is the trimmed Markdown under its heading.
type Section struct {
	Version string
	Body    string
}

// Parse splits src on "## " headings, in file order. Text before the first
// heading is ignored, and sections with an empty body are dropped.
func Parse(src string) []Section {
	var sections []Section
	var cur *Section
	var body strings.Builder
	flush := func() {
		if cur != nil {
			if cur.Body = strings.TrimSpace(body.String()); cur.Body != "" {
				sections = append(sections, *cur)
			}
		}
		body.Reset()
	}
	for line := range strings.Lines(src) {
		if heading, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "## "); ok {
			flush()
			cur = &Section{Version: strings.TrimSpace(heading)}
			continue
		}
		body.WriteString(line)
	}
	flush()
	return sections
}

// Embedded returns the highlights built into this binary.
func Embedded() []Section { return Parse(content) }

// Newer reports whether current is a stable release newer than lastSeen. A
// lastSeen that is missing or not a stable version counts as older.
func Newer(current, lastSeen string) bool {
	cur, ok := updatecheck.ParseStable(current)
	if !ok {
		return false
	}
	last, ok := updatecheck.ParseStable(lastSeen)
	return !ok || updatecheck.Compare(cur, last) > 0
}

// Since returns the released sections newer than lastSeen and no newer than
// current. When lastSeen is not a stable version, only current's section
// qualifies. Development builds get nothing.
func Since(all []Section, current, lastSeen string) []Section {
	cur, ok := updatecheck.ParseStable(current)
	if !ok {
		return nil
	}
	last, known := updatecheck.ParseStable(lastSeen)
	return filter(all, func(v [3]uint64) bool {
		if !known {
			return v == cur
		}
		return updatecheck.Compare(v, last) > 0 && updatecheck.Compare(v, cur) <= 0
	})
}

// All returns the released sections no newer than current. Development builds
// get every section, including Unreleased, so entries can be previewed.
func All(all []Section, current string) []Section {
	cur, ok := updatecheck.ParseStable(current)
	if !ok {
		return all
	}
	return filter(all, func(v [3]uint64) bool { return updatecheck.Compare(v, cur) <= 0 })
}

// filter keeps released sections whose version satisfies keep.
func filter(all []Section, keep func([3]uint64) bool) []Section {
	var out []Section
	for _, s := range all {
		if v, ok := updatecheck.ParseStable(s.Version); ok && keep(v) {
			out = append(out, s)
		}
	}
	return out
}

// Markdown renders sections under their version headings.
func Markdown(sections []Section) string {
	parts := make([]string, len(sections))
	for i, s := range sections {
		parts[i] = "## " + s.Version + "\n\n" + s.Body
	}
	return strings.Join(parts, "\n\n")
}
