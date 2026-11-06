package activity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sadmachine/asanamate/internal/safefile"
)

// CacheFile is the cache's name inside the cache directory.
const CacheFile = "activity.json"

// Window is how far back the cache keeps entries: the longest period.
const Window = 30 * 24 * time.Hour

// Cache holds the entries of earlier scans for one workspace and user.
// Until is the end of the last complete scan; zero means no scan finished.
// SeenAt is when the user last looked at Activity.
type Cache struct {
	Workspace string    `json:"workspace"`
	User      string    `json:"user"`
	Until     time.Time `json:"until"`
	SeenAt    time.Time `json:"seen_at"`
	Entries   []Entry   `json:"entries"`

	path string
}

// LoadCache reads the cache at path. A missing, unreadable, or corrupt file,
// or one for another workspace, gives an empty cache, so a bad cache never
// blocks startup. An empty path keeps the cache in memory only.
func LoadCache(path, workspace string) *Cache {
	empty := &Cache{Workspace: workspace, path: path}
	if path == "" {
		return empty
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var c Cache
	if json.Unmarshal(data, &c) != nil || c.Workspace != workspace {
		return empty
	}
	c.path = path
	return &c
}

// Save writes the cache atomically with owner-only permissions.
func (c *Cache) Save() error {
	if c.path == "" {
		return nil
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var data bytes.Buffer
	if err := json.NewEncoder(&data).Encode(c); err != nil {
		return err
	}
	return safefile.Write(c.path, data.Bytes(), 0o600)
}

// Stale reports whether the next scan must cover the whole window: no scan
// finished, or the last one ended before the window starts.
func (c *Cache) Stale(now time.Time) bool {
	return c.Until.Before(now.Add(-Window))
}

// NewSince reports whether e is related activity a scan loaded after seen.
// Own actions and automated reminders are never new.
func (e Entry) NewSince(seen time.Time) bool {
	return !e.Mine && !e.Automatic && e.FoundAt.After(seen)
}

// Merge adds a scan report's entries, found at foundAt. A story already
// cached is replaced but keeps its first FoundAt. A complete report extends
// Until. Entries older than Window are dropped.
func (c *Cache) Merge(r Report, foundAt time.Time) { c.merge(r, foundAt, false) }

// Replace is Merge that first drops cached entries inside a complete
// report's window, so deleted stories disappear. An incomplete report merges.
func (c *Cache) Replace(r Report, foundAt time.Time) { c.merge(r, foundAt, r.Complete) }

func (c *Cache) merge(r Report, foundAt time.Time, replace bool) {
	// Entries classified for another user explain nothing to this one.
	if r.User != c.User {
		c.User, c.Until, c.SeenAt, c.Entries = r.User, time.Time{}, time.Time{}, nil
	}
	found := map[string]time.Time{}
	var kept []Entry
	for _, e := range c.Entries {
		found[e.Story.GID] = e.FoundAt
		if replace && !e.At.Before(r.Since) && !e.At.After(r.Until) {
			continue
		}
		kept = append(kept, e)
	}
	index := map[string]int{}
	for i, e := range kept {
		index[e.Story.GID] = i
	}
	for _, e := range r.Entries {
		// Only Classify reads the notes; storing them would bloat the file.
		e.Task.HTMLNotes = ""
		e.FoundAt = foundAt
		if at, ok := found[e.Story.GID]; ok {
			e.FoundAt = at
		}
		if i, ok := index[e.Story.GID]; ok {
			kept[i] = e
			continue
		}
		index[e.Story.GID] = len(kept)
		kept = append(kept, e)
	}
	if r.Complete && r.Until.After(c.Until) {
		c.Until = r.Until
	}
	cutoff := r.Until.Add(-Window)
	if c.Until.After(r.Until) {
		cutoff = c.Until.Add(-Window)
	}
	kept = slices.DeleteFunc(kept, func(e Entry) bool { return e.At.Before(cutoff) })
	sortEntries(kept)
	c.Entries = kept
}
