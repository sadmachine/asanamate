// Package updatecheck finds newer stable asanamate releases without downloading them.
package updatecheck

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const releasesURL = "https://api.github.com/repos/sadmachine/asanamate/releases"

// Check returns the newest stable core tag if it is newer than current.
// Development and prerelease builds skip the check. The entire check is bounded
// to five seconds; callers may ignore errors because updates are advisory.
func Check(current string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return check(ctx, http.DefaultClient, releasesURL, current)
}

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func check(ctx context.Context, client *http.Client, endpoint, current string) (string, error) {
	installed, ok := stableVersion(current)
	if !ok {
		return "", nil
	}
	latest, tag := installed, ""
	// The companion publishes releases in the same repository. Scan all pages
	// so neither those releases nor publication order can hide a newer core tag.
	for page := 1; ; page++ {
		releases, err := fetch(ctx, client, fmt.Sprintf("%s?per_page=100&page=%d", endpoint, page))
		if err != nil {
			return "", err
		}
		for _, r := range releases {
			if !strings.HasPrefix(r.Tag, "v") || r.Draft || r.Prerelease {
				continue
			}
			if v, ok := stableVersion(r.Tag); ok && compare(v, latest) > 0 {
				latest, tag = v, r.Tag
			}
		}
		if len(releases) < 100 {
			return tag, nil
		}
	}
}

func fetch(ctx context.Context, client *http.Client, endpoint string) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "asanamate-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check releases: HTTP %d", resp.StatusCode)
	}
	const maxBody = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("release response exceeds %d bytes", maxBody)
	}
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// stableVersion accepts only release versions: vMAJOR.MINOR.PATCH, with an
// optional v for GoReleaser's injected version. Unknown builds never notify.
func stableVersion(raw string) ([3]uint64, bool) {
	var version [3]uint64
	parts := strings.Split(strings.TrimPrefix(raw, "v"), ".")
	if len(parts) != len(version) {
		return version, false
	}
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return version, false
		}
		version[i] = n
	}
	return version, true
}

func compare(a, b [3]uint64) int {
	for i := range a {
		if order := cmp.Compare(a[i], b[i]); order != 0 {
			return order
		}
	}
	return 0
}
