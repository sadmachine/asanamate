package drafts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCommentRoundTripAndIsolation(t *testing.T) {
	dir := t.TempDir()
	store, empty, err := OpenComment(dir, "workspace", "task")
	if err != nil || empty.Text != "" {
		t.Fatalf("new draft = %+v, %v", empty, err)
	}
	draft := Comment{Text: "  first\nsecond  ", Images: map[string][]byte{"/tmp/image.png": {1, 2, 3}}, Uncertain: true}
	if HasComment(dir, "workspace", "task") {
		t.Fatal("unsaved draft reported as saved")
	}
	if err := store.Save(draft); err != nil {
		t.Fatal(err)
	}
	if !HasComment(dir, "workspace", "task") || HasComment(dir, "other", "task") {
		t.Fatal("saved draft not reported for its identity only")
	}
	for _, path := range []string{filepath.Dir(store.path), store.path, store.path + ".lock"} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("draft must be private: %s: %v, %v", path, info, err)
		}
	}
	again, got, err := OpenComment(dir, "workspace", "task")
	if err != nil || got.Text != draft.Text || !got.Uncertain || got.UpdatedAt.IsZero() || !bytes.Equal(got.Images["/tmp/image.png"], draft.Images["/tmp/image.png"]) {
		t.Fatalf("restored draft = %+v, %v", got, err)
	}
	for _, identity := range [][2]string{{"other", "task"}, {"workspace", "other"}, {"../workspace", "../task"}} {
		_, got, err := OpenComment(dir, identity[0], identity[1])
		if err != nil || got.Text != "" {
			t.Fatalf("draft leaked across identity %v: %+v, %v", identity, got, err)
		}
	}
	if err := again.Remove(); err != nil {
		t.Fatal(err)
	}
	if HasComment(dir, "workspace", "task") {
		t.Fatal("removed draft still reported")
	}
	_, got, err = OpenComment(dir, "workspace", "task")
	if err != nil || got.Text != "" || len(got.Images) != 0 {
		t.Fatalf("discard left content: %+v, %v", got, err)
	}
}

func TestConcurrentCommentChangesNeverOverwrite(t *testing.T) {
	dir := t.TempDir()
	a, _, _ := OpenComment(dir, "w", "t")
	b, _, _ := OpenComment(dir, "w", "t")
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*CommentStore{a, b} {
		group.Go(func() { results <- store.Save(Comment{Text: "keep"}) })
	}
	group.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
	current, _, _ := OpenComment(dir, "w", "t")
	stale, _, _ := OpenComment(dir, "w", "t")
	if err := current.Save(Comment{Text: "newer"}); err != nil {
		t.Fatal(err)
	}
	if err := stale.Remove(); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale remove = %v", err)
	}
	_, got, _ := OpenComment(dir, "w", "t")
	if got.Text != "newer" {
		t.Fatal("stale editor deleted newer draft")
	}
}

func TestCorruptCommentIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	store, _, _ := OpenComment(dir, "w", "t")
	if err := store.Save(Comment{Text: "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenComment(dir, "w", "t"); err == nil {
		t.Fatal("corrupt draft silently ignored")
	}
	if err := store.Save(Comment{Text: "replace"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("corrupt draft overwritten: %v", err)
	}
}
