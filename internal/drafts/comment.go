// Package drafts stores unfinished input separately from application settings.
package drafts

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sadmachine/asanamate/internal/writeback"
	"golang.org/x/sys/unix"
)

// ErrConflict prevents a second session from replacing or deleting a draft
// changed since this session opened it.
var ErrConflict = errors.New("draft changed in another session; keep this text and reopen the comment before saving")

// Comment is one ticket's unfinished comment. Images holds snapshots of local
// Markdown images, keyed by their original paths, so temporary files can vanish.
type Comment struct {
	Text      string            `json:"text"`
	Images    map[string][]byte `json:"images,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
	// Uncertain is saved before posting. A restart during a write cannot
	// distinguish a delivered comment from one that never reached Asana.
	Uncertain bool `json:"uncertain,omitempty"`
	// Changes and Branch share the comment's save, discard, and recovery lifecycle.
	Changes      []writeback.Change `json:"changes,omitempty"`
	ChangeLabels map[string]string  `json:"change_labels,omitempty"`
	Branch       *string            `json:"branch,omitempty"`
}

// CommentStore owns one workspace/ticket draft and remembers the version read.
// Each editor uses its own store. Save and Remove serialize writers and reject
// changes made by another store; readers see only complete snapshots.
type CommentStore struct {
	path     string
	snapshot []byte
}

// OpenComment reads a draft. No draft returns a zero Comment. Identity is hashed
// so workspace and ticket values cannot escape the draft directory.
func OpenComment(stateDir, workspace, task string) (*CommentStore, Comment, error) {
	if stateDir == "" {
		return nil, Comment{}, errors.New("draft state directory is not set")
	}
	s := &CommentStore{path: commentPath(stateDir, workspace, task)}
	data, err := read(s.path)
	if err != nil {
		return nil, Comment{}, err
	}
	var draft Comment
	if len(data) > 0 {
		if err := json.Unmarshal(data, &draft); err != nil {
			return nil, Comment{}, fmt.Errorf("reading comment draft: %w", err)
		}
	}
	s.snapshot = data
	return s, draft, nil
}

// HasComment reports whether a draft is saved for the workspace and identity
// OpenComment takes, without reading or locking it.
func HasComment(stateDir, workspace, task string) bool {
	if stateDir == "" {
		return false
	}
	_, err := os.Stat(commentPath(stateDir, workspace, task))
	return err == nil
}

func commentPath(stateDir, workspace, task string) string {
	identity := sha256.Sum256([]byte(workspace + "\x00" + task))
	return filepath.Join(stateDir, "drafts", "comments", fmt.Sprintf("%x.json", identity))
}

// Save atomically replaces the draft, including its images, with private permissions.
func (s *CommentStore) Save(draft Comment) error {
	draft.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return s.locked(func() error {
		file, err := os.CreateTemp(filepath.Dir(s.path), ".comment-*.json")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := os.Rename(file.Name(), s.path); err != nil {
			return err
		}
		s.snapshot = data
		return nil
	})
}

// Remove deletes only the draft version this editor last read or saved.
func (s *CommentStore) Remove() error {
	return s.locked(func() error {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		s.snapshot = nil
		return nil
	})
}

func (s *CommentStore) locked(change func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Keep the lock file: unlinking it could let two writers lock different inodes.
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	current, err := read(s.path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, s.snapshot) {
		return ErrConflict
	}
	return change()
}

func read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}
