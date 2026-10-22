package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/drafts"
	"github.com/sadmachine/asanamate/internal/keymap"
)

var ctrlD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}

func editableComment() asana.Story {
	return asana.Story{GID: "9", Type: "comment", IsEditable: true, CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: "<body><strong>Original</strong></body>"}
}

func TestCommentEditMenuAndSave(t *testing.T) {
	m, writes := editModel(t)
	tk := m.details["1"]
	tk.Comments = []asana.Story{editableComment()}
	m.details["1"] = tk
	m.fieldKey, m.focusReader = "comment:9", true
	press(m, "enter", "e")
	if m.input == nil || m.input.area.Value() != "**Original**" || m.composer.storyGID != "9" {
		t.Fatalf("edit did not preload Markdown: %+v", m.composer)
	}
	m.input.area.SetValue("**Updated**")
	send(m, ctrlS)
	if m.input != nil || len(*writes) != 1 || !strings.HasPrefix((*writes)[0], "PUT /stories/9 ") {
		t.Fatalf("input = %v, writes = %v", m.input, *writes)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix((*writes)[0], "PUT /stories/9 ")), &body); err != nil || body.Data["html_text"] != "<body><strong>Updated</strong></body>" {
		t.Fatalf("body = %+v, err = %v", body, err)
	}
}

func TestCommentEditDraftsAreIndependent(t *testing.T) {
	m, writes := editModel(t)
	press(m, "c")
	m.input.area.SetValue("new comment draft")
	send(m, ctrlD)
	m.openCommentEditor("1", editableComment())
	m.input.area.SetValue("edit draft")
	send(m, ctrlD)
	if savedComment(t, m).Text != "new comment draft" || len(*writes) != 0 {
		t.Fatal("edit draft changed the new-comment draft or wrote to Asana")
	}
	m.openCommentEditor("1", asana.Story{GID: "10", HTMLText: "<body>Other comment</body>"})
	if m.input.area.Value() != "Other comment" {
		t.Fatal("edit draft leaked into another comment")
	}
	press(m, "esc")
	m.openCommentEditor("1", editableComment())
	if m.input.area.Value() != "edit draft" {
		t.Fatal("edit draft not restored")
	}
}

func TestCommentEditCancelUnchangedAndRejectEmpty(t *testing.T) {
	m, writes := editModel(t)
	m.openCommentEditor("1", editableComment())
	press(m, "esc")
	if m.input != nil || len(*writes) != 0 {
		t.Fatal("unchanged edit did not close quietly")
	}
	m.openCommentEditor("1", editableComment())
	m.input.area.SetValue(" \n ")
	send(m, ctrlS)
	if m.input == nil || !strings.Contains(m.input.err, "empty") || len(*writes) != 0 {
		t.Fatal("empty edit closed or wrote")
	}
}

func TestCommentEditFailureKeepsInputAndDraft(t *testing.T) {
	for _, failure := range []string{"permission", "api"} {
		t.Run(failure, func(t *testing.T) {
			m, _ := editModel(t)
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					json.NewEncoder(w).Encode(map[string]any{"data": asana.Story{Type: "comment", IsEditable: failure != "permission"}})
					return
				}
				writes++
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"errors":[{"message":"Edit denied"}]}`)
			}))
			t.Cleanup(srv.Close)
			m.deps.Client.BaseURL = srv.URL
			m.openCommentEditor("1", editableComment())
			m.input.area.SetValue("keep edited text")
			send(m, ctrlS)
			if m.input == nil || m.input.area.Value() != "keep edited text" || m.input.err == "" || m.composer.busy {
				t.Fatal("failed edit lost input or stayed busy")
			}
			_, draft, err := drafts.OpenComment(m.deps.StateDir, "w", "story:9")
			if err != nil || draft.Text != "keep edited text" || draft.Uncertain {
				t.Fatalf("draft = %+v, err = %v", draft, err)
			}
			if (writes == 0) != (failure == "permission") {
				t.Fatalf("writes = %d after %s", writes, failure)
			}
		})
	}
}

func TestCommentEditKeepsBusyUntilWriteCompletes(t *testing.T) {
	m, writes := editModel(t)
	m.openCommentEditor("1", editableComment())
	m.input.area.SetValue("edited")
	_, cmd := m.Update(ctrlS)
	if cmd == nil || !m.composer.busy {
		t.Fatal("edit did not start")
	}
	press(m, "x", "esc")
	send(m, ctrlS)
	if m.input.area.Value() != "edited" || len(*writes) != 0 {
		t.Fatal("busy editor changed input or duplicated the write")
	}
	drain(m, cmd)
	if m.input != nil || len(*writes) != 1 {
		t.Fatalf("input = %v, writes = %v", m.input, *writes)
	}
}

func TestCommentMenuHidesUnavailableEdit(t *testing.T) {
	m, _ := editModel(t)
	c := editableComment()
	c.IsEditable = false
	m.openCommentMenu("1", c)
	if len(m.modal.items) != 1 || m.modal.items[0].Value != commentReact {
		t.Fatalf("menu = %+v", m.modal.items)
	}
}

func savedComment(t *testing.T, m *Model) drafts.Comment {
	t.Helper()
	_, draft, err := drafts.OpenComment(m.deps.StateDir, m.deps.Config.Workspace, "1")
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestCommentSaveAndRestoreAcrossSessions(t *testing.T) {
	m, writes := editModel(t)
	m.width, m.height = 140, 40
	press(m, "c")
	m.input.area.SetValue("  first\nsecond  ")
	if view := ansi.Strip(m.body()); !strings.Contains(view, keyLabel("input", "save_draft")+" draft") {
		t.Fatal("draft key missing from editor")
	}
	send(m, ctrlD)
	if m.input != nil || len(*writes) != 0 || savedComment(t, m).Text != "  first\nsecond  " {
		t.Fatal("saving draft lost content or posted to Asana")
	}
	next, _ := editModel(t)
	next.deps.StateDir = m.deps.StateDir
	next.viewProject = &asana.Ref{GID: "another-project"}
	press(next, "c")
	if next.input.area.Value() != "  first\nsecond  " || !strings.Contains(next.input.title, "draft") {
		t.Fatal("new session or project did not restore ticket draft")
	}
}

func TestCommentExitChoices(t *testing.T) {
	for _, choice := range []string{"save", "discard", "keep", "esc"} {
		t.Run(choice, func(t *testing.T) {
			m, writes := editModel(t)
			m.width, m.height = 140, 40
			press(m, "c")
			m.input.area.SetValue("draft")
			press(m, "esc")
			if m.composer.confirm == nil || !strings.Contains(ansi.Strip(m.body()), "Save comment as draft?") {
				t.Fatal("content exited without confirmation")
			}
			switch choice {
			case "save":
				press(m, "enter")
			case "discard":
				press(m, "down", "enter")
			case "keep":
				press(m, "down", "down", "enter")
			case "esc":
				press(m, "esc")
			}
			if len(*writes) != 0 {
				t.Fatal("exit posted comment")
			}
			if choice == "keep" || choice == "esc" {
				if m.input == nil || m.input.area.Value() != "draft" || m.composer.confirm != nil {
					t.Fatal("keep editing lost editor")
				}
			} else if m.input != nil {
				t.Fatal("save/discard did not close editor")
			}
			if got := savedComment(t, m).Text; (got == "draft") != (choice == "save") {
				t.Fatalf("saved text = %q after %s", got, choice)
			}
		})
	}
}

func TestEmptyCommentExitsWithoutConfirmation(t *testing.T) {
	m, writes := editModel(t)
	press(m, "c")
	m.input.area.SetValue(" \n ")
	press(m, "esc")
	if m.input != nil || m.composer != nil || len(*writes) != 0 {
		t.Fatal("empty comment did not close quietly")
	}
}

func TestCommentCtrlCConfirmsBeforeQuitting(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	m.input.area.SetValue("keep")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || m.composer.confirm == nil {
		t.Fatal("ctrl+c quit with unsaved content")
	}
	press(m, "esc")
	if m.composer.quitting || m.input.area.Value() != "keep" {
		t.Fatal("cancelling quit lost input")
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	_, cmd = m.Update(key("enter"))
	msg := cmd()
	_, cmd = m.Update(msg)
	if !slices.Contains(commandMessages(cmd), tea.Msg(tea.QuitMsg{})) || savedComment(t, m).Text != "keep" {
		t.Fatal("save-and-quit failed")
	}
}

func TestCommentFailuresKeepInputAndDraft(t *testing.T) {
	for _, failure := range []string{"api", "network", "response", "server", "storage"} {
		t.Run(failure, func(t *testing.T) {
			m, writes := editModel(t)
			press(m, "c")
			m.input.area.SetValue("keep this comment")
			if failure == "storage" {
				if err := os.WriteFile(filepath.Join(m.deps.StateDir, "drafts"), []byte("block directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if failure == "response" {
						// Asana received the write, but the success response broke mid-body.
						w.Header().Set("Content-Length", "1000")
						io.WriteString(w, `{"data":`)
						return
					}
					if failure == "server" {
						http.Error(w, "unavailable", http.StatusInternalServerError)
						return
					}
					http.Error(w, `{"errors":[{"message":"denied"}]}`, http.StatusForbidden)
				}))
				t.Cleanup(srv.Close)
				m.deps.Client.BaseURL = srv.URL
				if failure == "network" {
					srv.Close()
				}
			}
			send(m, ctrlS)
			if m.input == nil || m.input.area.Value() != "keep this comment" || m.input.err == "" || m.composer.busy {
				t.Fatal("failed post lost input or remained busy")
			}
			if len(*writes) != 0 {
				t.Fatal("storage failure still posted")
			}
			if failure != "storage" {
				if got := savedComment(t, m); got.Text != "keep this comment" || got.Uncertain != (failure != "api") {
					t.Fatalf("recovery draft = %+v", got)
				}
			}
			if failure == "network" {
				send(m, ctrlS)
				if m.composer.confirm == nil || m.composer.confirm.items[0].Label != "Keep editing" {
					t.Fatal("uncertain retry did not require confirmation")
				}
			}
		})
	}
}

func TestCommentBusyIgnoresEditingAndDuplicatePosts(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	m.input.area.SetValue("one post")
	_, cmd := m.Update(ctrlS)
	if !m.composer.busy || cmd == nil || savedComment(t, m).Text != "" {
		t.Fatal("post did not start off UI loop")
	}
	for _, msg := range []tea.Msg{ctrlS, ctrlD, key("esc"), key("x"), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}} {
		_, duplicate := m.Update(msg)
		if duplicate != nil || m.input.area.Value() != "one post" {
			t.Fatal("busy editor changed or submitted twice")
		}
	}
	drain(m, cmd)
	if m.input != nil || savedComment(t, m).Text != "" {
		t.Fatal("successful post did not remove draft")
	}
}

func TestCommentImagesSurviveMissingOriginalAndPost(t *testing.T) {
	m, _ := editModel(t)
	path := filepath.Join(t.TempDir(), "pasted image.png")
	png := []byte("\x89PNG\r\n\x1a\nimage")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	press(m, "c")
	send(m, imagePasteMsg{path: path})
	send(m, ctrlD)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var uploaded, posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/attachments":
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			uploaded = string(data) == string(png)
			io.WriteString(w, `{"data":{"gid":"9"}}`)
		case "/tasks/1/stories":
			if r.Method != http.MethodPost {
				io.WriteString(w, `{"data":[]}`)
				return
			}
			var body struct{ Data map[string]string }
			json.NewDecoder(r.Body).Decode(&body)
			posted = strings.Contains(body.Data["html_text"], `data-asana-gid="9"`)
			io.WriteString(w, `{"data":{"gid":"10"}}`)
		}
	}))
	defer srv.Close()
	next, _ := editModel(t)
	next.deps.StateDir = m.deps.StateDir
	next.deps.Client.BaseURL = srv.URL
	press(next, "c")
	send(next, ctrlS)
	if !uploaded || !posted || next.input != nil || savedComment(t, next).Text != "" {
		t.Fatalf("upload = %v, post = %v, status = %s", uploaded, posted, next.status)
	}
}

func TestCommentKeyRemapAndOtherInputs(t *testing.T) {
	m, _ := editModel(t)
	keys = keymap.Default()
	keys["input"]["save_draft"] = []string{"alt+s"}
	press(m, "c")
	m.input.area.SetValue("draft")
	send(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt})
	if m.input != nil || savedComment(t, m).Text != "draft" {
		t.Fatal("remapped draft key did not save")
	}
	b := newInputBox("Action input", "")
	b.area.SetValue("abc")
	b.area.CursorStart()
	b.update(ctrlD)
	if b.area.Value() != "bc" || strings.Contains(b.view(80, 20, m.accentStyle), "save draft") {
		t.Fatal("draft behavior leaked into action input")
	}
}

func TestInterruptedCommentRestoresUncertainty(t *testing.T) {
	m, _ := editModel(t)
	store, _, err := drafts.OpenComment(m.deps.StateDir, "w", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(drafts.Comment{Text: "might be posted", Uncertain: true}); err != nil {
		t.Fatal(err)
	}
	press(m, "c")
	if !strings.Contains(m.input.help, "Delivery uncertain") {
		t.Fatal("interrupted write restored without warning")
	}
	send(m, ctrlS)
	if m.composer.confirm == nil {
		t.Fatal("interrupted write retried without confirmation")
	}
}

func TestCommentTimeoutRequiresReview(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	m.input.area.SetValue("may have arrived")
	composer := m.composer
	m.Update(commentDoneMsg{composer: composer, op: commentPost, saved: true,
		draft: drafts.Comment{Text: "may have arrived", Uncertain: commentDeliveryUncertain(context.DeadlineExceeded)},
		err:   context.DeadlineExceeded})
	if m.input.area.Value() != "may have arrived" || !strings.Contains(m.input.help, "Delivery uncertain") {
		t.Fatal("timeout lost input or delivery warning")
	}
	send(m, ctrlS)
	if composer.confirm == nil {
		t.Fatal("timeout retry did not ask for review")
	}
}

func TestCommentConcurrentSessionPreservesNewerDraft(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	m.input.area.SetValue("this session")
	other, _, err := drafts.OpenComment(m.deps.StateDir, "w", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Save(drafts.Comment{Text: "other session"}); err != nil {
		t.Fatal(err)
	}
	send(m, ctrlD)
	if m.input == nil || m.input.area.Value() != "this session" || !strings.Contains(m.input.err, "another session") || savedComment(t, m).Text != "other session" {
		t.Fatal("conflict lost either session's text")
	}
	m.input.area.SetValue(" \n ")
	press(m, "esc")
	if m.input != nil || savedComment(t, m).Text != "other session" {
		t.Fatal("empty editor could not exit or deleted another session's draft")
	}
}

func TestCommentPostCleanupDoesNotDeleteNewerDraft(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	m.input.area.SetValue("post this")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other, _, err := drafts.OpenComment(m.deps.StateDir, "w", "1")
		if err == nil {
			err = other.Save(drafts.Comment{Text: "next comment"})
		}
		if err != nil {
			t.Error(err)
		}
		io.WriteString(w, `{"data":{"gid":"9"}}`)
	}))
	defer srv.Close()
	m.deps.Client.BaseURL = srv.URL
	send(m, ctrlS)
	if m.input != nil || savedComment(t, m).Text != "next comment" || !strings.Contains(m.status, "comment posted") {
		t.Fatal("post cleanup deleted newer draft or reported post as failed")
	}
}
