// Package whatsnew reads the curated release highlights shown after an upgrade.
package whatsnew

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/sadmachine/asanamate/internal/updatecheck"
)

//go:embed whats-new.md
var content string

// unreleasedFS holds unreleased/, one highlights file per change.
//
//go:embed unreleased
var unreleasedFS embed.FS

// UnreleasedDir is where each change's highlights wait for a release,
// relative to this package.
const UnreleasedDir = "unreleased"

// unreleasedReadme explains UnreleasedDir and is not a highlight.
const unreleasedReadme = "README.md"

// fragmentHeadings are the headings a highlights file may use, in the order
// a release lists them.
var fragmentHeadings = []string{"### New", "### Changed"}

// Fragment is one change's highlights file: its name and Markdown.
type Fragment struct {
	Name, Body string
}

// ReadFragments returns the highlights files in fsys's dir, by name,
// leaving out its README.
func ReadFragments(fsys fs.FS, dir string) ([]Fragment, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var frags []Fragment
	for _, e := range entries {
		if e.IsDir() || e.Name() == unreleasedReadme || path.Ext(e.Name()) != ".md" {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		frags = append(frags, Fragment{Name: e.Name(), Body: string(body)})
	}
	return frags, nil
}

// UnreleasedBody merges fragments into one version's body: each heading
// once, in fragmentHeadings order, with every fragment's entries under it
// in file name order. It names the first fragment that is empty, has text
// outside a heading, or uses another heading.
func UnreleasedBody(frags []Fragment) (string, error) {
	entries := map[string][]string{}
	for _, f := range frags {
		heading, any := "", false
		var cur strings.Builder
		flush := func() {
			if text := strings.TrimSpace(cur.String()); text != "" {
				entries[heading] = append(entries[heading], text)
				any = true
			}
			cur.Reset()
		}
		for line := range strings.Lines(f.Body) {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				if !slices.Contains(fragmentHeadings, trimmed) {
					return "", fmt.Errorf("%s: heading %q must be one of %s", f.Name, trimmed, strings.Join(fragmentHeadings, ", "))
				}
				flush()
				heading = trimmed
				continue
			}
			if heading == "" && trimmed != "" {
				return "", fmt.Errorf("%s: text must follow a %s heading", f.Name, strings.Join(fragmentHeadings, " or "))
			}
			cur.WriteString(line)
		}
		flush()
		if !any {
			return "", fmt.Errorf("%s: has no entries", f.Name)
		}
	}
	var parts []string
	for _, h := range fragmentHeadings {
		if len(entries[h]) > 0 {
			parts = append(parts, h+"\n\n"+strings.Join(entries[h], "\n"))
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// Release returns src, the highlights file, with frags merged under a new
// "## version" heading above the newest version. It returns src unchanged
// when there are no fragments, and fails when version is already there.
func Release(src, version string, frags []Fragment) (string, error) {
	// The release workflow copies the section whose heading is the tag.
	if _, ok := updatecheck.ParseStable(version); !ok || !strings.HasPrefix(version, "v") {
		return "", fmt.Errorf("version %q must look like vX.Y.Z", version)
	}
	if slices.ContainsFunc(Parse(src), func(s Section) bool { return s.Version == version }) {
		return "", fmt.Errorf("%s already has highlights", version)
	}
	body, err := UnreleasedBody(frags)
	if err != nil || body == "" {
		return src, err
	}
	section := "## " + version + "\n\n" + body + "\n\n"
	if strings.HasPrefix(src, "## ") {
		return section + src, nil
	}
	if i := strings.Index(src, "\n## "); i >= 0 {
		return src[:i+1] + section + src[i+1:], nil
	}
	return strings.TrimRight(src, "\n") + "\n\n" + section[:len(section)-1], nil
}

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

// Embedded returns the highlights built into this binary, led by an
// Unreleased section built from the waiting highlights files, if any.
// Released builds never show Unreleased; see Since and All.
func Embedded() []Section {
	sections := Parse(content)
	frags, err := ReadFragments(unreleasedFS, UnreleasedDir)
	if err != nil {
		return sections
	}
	if body, err := UnreleasedBody(frags); err == nil && body != "" {
		sections = append([]Section{{Version: unreleased, Body: body}}, sections...)
	}
	return sections
}

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
