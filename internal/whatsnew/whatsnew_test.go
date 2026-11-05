package whatsnew

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/updatecheck"
)

var fixture = []Section{{"Unreleased", "u"}, {"v0.5.0", "c"}, {"v0.4.1", "b"}, {"v0.4.0", "a"}}

func versions(sections []Section) []string {
	var out []string
	for _, s := range sections {
		out = append(out, s.Version)
	}
	return out
}

func TestParse(t *testing.T) {
	got := Parse("# What's new\n\nIntro.\n\n## Unreleased\n\n### New\n\n- **A.** a\n\n## v0.5.0\n\n### New\n\n- **B.** b\n\n## v0.4.0\n\n")
	want := []Section{{"Unreleased", "### New\n\n- **A.** a"}, {"v0.5.0", "### New\n\n- **B.** b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestNewerAndSince(t *testing.T) {
	for _, tc := range []struct {
		current, lastSeen string
		newer             bool
		since             []string
	}{
		{"v0.5.0", "v0.4.0", true, []string{"v0.5.0", "v0.4.1"}},
		{"0.5.0", "v0.4.0", true, []string{"v0.5.0", "v0.4.1"}}, // GoReleaser drops the v
		{"v0.5.0", "", true, []string{"v0.5.0"}},
		{"v0.5.0", "latest", true, []string{"v0.5.0"}},
		{"v0.5.0", "v0.5.0", false, nil},
		{"v0.4.0", "v0.5.0", false, nil},
		{"dev", "v0.4.0", false, nil},
		{"", "v0.4.0", false, nil},
		{"v0.5.1-0.20261105120000-abcdef123456", "v0.4.0", false, nil},
		{"v0.4.2", "v0.4.1", true, nil},
	} {
		t.Run(tc.current+"/"+tc.lastSeen, func(t *testing.T) {
			if got := Newer(tc.current, tc.lastSeen); got != tc.newer {
				t.Errorf("Newer = %v, want %v", got, tc.newer)
			}
			if got := versions(Since(fixture, tc.current, tc.lastSeen)); !reflect.DeepEqual(got, tc.since) {
				t.Errorf("Since = %v, want %v", got, tc.since)
			}
		})
	}
}

func TestAll(t *testing.T) {
	if got := versions(All(fixture, "v0.4.1")); !reflect.DeepEqual(got, []string{"v0.4.1", "v0.4.0"}) {
		t.Errorf("stable = %v", got)
	}
	if got := versions(All(fixture, "dev")); !reflect.DeepEqual(got, []string{"Unreleased", "v0.5.0", "v0.4.1", "v0.4.0"}) {
		t.Errorf("dev = %v", got)
	}
}

func TestMarkdown(t *testing.T) {
	if got := Markdown([]Section{{"v0.5.0", "c"}, {"v0.4.1", "b"}}); got != "## v0.5.0\n\nc\n\n## v0.4.1\n\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestEmbeddedContentIsWellFormed(t *testing.T) {
	var prev [3]uint64
	seen := 0
	for line := range strings.Lines(content) {
		heading, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "## ")
		if !ok {
			continue
		}
		seen++
		if heading == unreleased {
			if seen != 1 {
				t.Errorf("%q must be the first section", heading)
			}
			continue
		}
		v, ok := updatecheck.ParseStable(heading)
		if !ok || !strings.HasPrefix(heading, "v") {
			t.Errorf("heading %q is not Unreleased or vX.Y.Z", heading)
			continue
		}
		if prev != [3]uint64{} && updatecheck.Compare(v, prev) >= 0 {
			t.Errorf("%q is not older than the section above it", heading)
		}
		prev = v
	}
	Embedded() // must not panic
}
