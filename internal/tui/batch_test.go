package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/writeback"
)

func chooseBatch(t *testing.T, m *Model, label string) {
	t.Helper()
	if m.modal == nil || m.composer == nil || m.modal != m.composer.review {
		t.Fatal("batch review is not open")
	}
	for i, item := range m.modal.items {
		if strings.TrimPrefix(item.Label, "* ") == label {
			m.modal.cursor = i
			press(m, "enter")
			return
		}
	}
	t.Fatalf("batch row %q not found", label)
}

func TestBatchStagesTitleAndDescription(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	chooseBatch(t, m, "Title")
	m.input.area.SetValue("New title")
	send(m, ctrlS)
	chooseBatch(t, m, "Title")
	if m.input.area.Value() != "New title" {
		t.Fatal("staged title was lost")
	}
	m.input.area.SetValue("Fix")
	send(m, ctrlS)
	if len(m.composer.changes) != 0 {
		t.Fatal("returning to original title left a change")
	}
	chooseBatch(t, m, "Title")
	m.input.area.SetValue("New title")
	send(m, ctrlS)
	chooseBatch(t, m, "Description")
	m.input.area.SetValue("**Details**")
	send(m, ctrlS)
	if len(*writes) != 0 || len(m.composer.changes) != 2 {
		t.Fatalf("wrote before submission: %v", *writes)
	}
	send(m, ctrlS)
	if m.composer != nil || len(*writes) != 2 || !strings.Contains((*writes)[0], `"name":"New title"`) || !strings.Contains((*writes)[1], `"html_notes":"\u003cbody\u003e\u003cstrong\u003eDetails\u003c/strong\u003e\u003c/body\u003e"`) {
		t.Fatalf("writes = %v", *writes)
	}
}

func TestCommentCombinesAssignmentSectionAndText(t *testing.T) {
	m, writes := editModel(t)
	m.viewProject = &asana.Ref{GID: "p1"}
	press(m, "c")
	m.input.area.SetValue("Ready for review")
	press(m, "tab", "enter", "down", "down", "enter")
	if len(*writes) != 0 || m.input != m.composer.box || m.input.area.Value() != "Ready for review" {
		t.Fatal("assignment sent early or lost the comment")
	}
	press(m, "tab", "enter", "enter", "down", "enter")
	if len(*writes) != 0 || len(m.composer.changes) != 2 {
		t.Fatalf("pending = %+v, writes = %v", m.composer.changes, *writes)
	}
	send(m, ctrlS)
	if m.composer != nil || len(*writes) != 3 || !strings.HasPrefix((*writes)[0], "POST /sections/s2/addTask ") ||
		!strings.Contains((*writes)[1], `"assignee":"u2"`) || !strings.Contains((*writes)[2], `"text":"Ready for review"`) {
		t.Fatalf("composer = %+v, writes = %v", m.composer, *writes)
	}
}

func TestBatchStagesAllDetailsAndSubmitsWithoutComment(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	for _, label := range []string{"Assign to", "Due date", "Completed", "Web section", "My Tasks section", "Branch", "Due", "Scope", "Reviewers", "Branch (local)", "Add to project", "Remove from project", "Comment (optional)"} {
		found := false
		for _, item := range m.modal.items {
			found = found || item.Label == label
		}
		if !found {
			t.Errorf("missing editable detail %q", label)
		}
	}
	chooseBatch(t, m, "Due date")
	m.input.area.SetValue("2026-10-20")
	send(m, ctrlS)
	chooseBatch(t, m, "Completed")
	if len(*writes) != 0 || len(m.composer.changes) != 2 {
		t.Fatal("details were sent before final submit")
	}
	send(m, ctrlS)
	if m.composer != nil || len(*writes) != 2 || !strings.Contains((*writes)[0], `"due_on":"2026-10-20"`) || !strings.Contains((*writes)[1], `"completed":true`) {
		t.Fatalf("writes = %v, composer = %+v", *writes, m.composer)
	}
}

func TestBatchSubmitHotkeyAppearsInFooterWithoutSubmitRow(t *testing.T) {
	for _, submit := range []string{"ctrl+s", "ctrl+g"} {
		t.Run(submit, func(t *testing.T) {
			m, writes := editModel(t)
			var err error
			keys, err = keymap.Resolve(map[string]map[string][]string{"input": {"submit": {submit}}})
			if err != nil {
				t.Fatal(err)
			}
			press(m, "e", "m")
			for _, item := range m.modal.items {
				if item.Label == "Submit all changes" {
					t.Fatal("submit still appears as an editable row")
				}
			}
			view := ansi.Strip(m.modal.view(160, 20, m.modalLook()))
			footer := view[strings.LastIndex(view, "\n")+1:]
			if !strings.Contains(footer, submit+" submit all changes") {
				t.Fatalf("footer lacks submit hotkey: %q", footer)
			}
			if submit != "ctrl+s" && strings.Contains(footer, "ctrl+s") {
				t.Fatalf("footer shows the old submit hotkey: %q", footer)
			}
			chooseBatch(t, m, "Completed")
			// Keep another detail selected: submission must not require a submit row.
			press(m, submit)
			if m.composer != nil || len(*writes) != 1 || !strings.Contains((*writes)[0], `"completed":true`) {
				t.Fatalf("submit hotkey did not save the draft: composer = %v, writes = %v", m.composer, *writes)
			}
		})
	}
}

func TestBatchCancelAndDiscardWritesNothing(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	chooseBatch(t, m, "Due date")
	press(m, "esc")
	chooseBatch(t, m, "Completed")
	press(m, "esc", "down", "enter")
	if m.composer != nil || m.input != nil || m.modal != nil || len(*writes) != 0 {
		t.Fatalf("discard left a modal or wrote: composer = %+v, writes = %v", m.composer, *writes)
	}
}

func TestBatchDraftRestoresDetailsAndBranch(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	chooseBatch(t, m, "Completed")
	chooseBatch(t, m, "Branch (local)")
	m.input.area.SetValue("feature/batch")
	send(m, ctrlS)
	send(m, ctrlD)
	if len(*writes) != 0 || m.composer != nil || m.deps.State.TaskBranches["1"] != "" {
		t.Fatal("saving draft wrote staged changes")
	}
	press(m, "e", "m")
	if len(m.composer.changes) != 1 || m.composer.branch == nil || *m.composer.branch != "feature/batch" {
		t.Fatalf("restored = %+v", m.composer)
	}
	send(m, ctrlS)
	if len(*writes) != 1 || m.deps.State.TaskBranches["1"] != "feature/batch" {
		t.Fatalf("writes = %v, branch = %q", *writes, m.deps.State.TaskBranches["1"])
	}
}

func TestBatchRepeatingAFieldUsesStagedValueAndCanUndo(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	chooseBatch(t, m, "Branch")
	m.input.area.SetValue("feature/new")
	send(m, ctrlS)
	chooseBatch(t, m, "Branch")
	if m.input.area.Value() != "feature/new" {
		t.Fatal("field did not preload staged value")
	}
	m.input.area.SetValue("old")
	send(m, ctrlS)
	if len(m.composer.changes) != 0 || len(*writes) != 0 {
		t.Fatal("returning to original value did not undo the pending edit")
	}
}

func TestBatchFailureKeepsOnlyRemainingEditsAndComment(t *testing.T) {
	m, _ := editModel(t)
	var writes []string
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"data":{"gid":"1","name":"Fix"}}`)
			return
		}
		data, _ := io.ReadAll(r.Body)
		writes = append(writes, string(data))
		if fail && strings.Contains(string(data), "due_on") {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"errors":[{"message":"Denied"}]}`)
			return
		}
		io.WriteString(w, `{"data":{}}`)
	}))
	t.Cleanup(srv.Close)
	m.deps.Client.BaseURL = srv.URL
	press(m, "e", "m")
	chooseBatch(t, m, "Completed")
	chooseBatch(t, m, "Due date")
	m.input.area.SetValue("2026-10-20")
	send(m, ctrlS)
	chooseBatch(t, m, "Comment (optional)")
	m.input.area.SetValue("Keep this comment")
	send(m, ctrlS)
	if m.composer == nil || len(m.composer.changes) != 1 || !m.composer.task.Completed || len(writes) != 2 || m.composer.uncertain {
		t.Fatalf("composer = %+v, writes = %v", m.composer, writes)
	}
	draft := savedComment(t, m)
	if len(draft.Changes) != 1 || draft.Changes[0].Kind != "due" || draft.Text != "Keep this comment" || draft.Uncertain {
		t.Fatalf("recovery draft = %+v", draft)
	}
	fail = false
	send(m, ctrlS)
	if m.composer != nil || len(writes) != 4 || strings.Contains(writes[2], "completed") || !strings.Contains(writes[3], "Keep this comment") {
		t.Fatalf("retry writes = %v, composer = %+v", writes, m.composer)
	}
}

func TestBatchValidationFailureLeavesAllEditsUnsent(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "m")
	m.composer.changes = []writeback.Change{{Kind: "complete", Value: "true"}, {Kind: "field", Target: "missing", Value: "bad"}}
	send(m, ctrlS)
	if len(*writes) != 0 || len(m.composer.changes) != 2 || m.modal.err == "" {
		t.Fatalf("writes = %v, changes = %v, error = %q", *writes, m.composer.changes, m.modal.err)
	}
}

func TestCommentFooterRendersAndPreservesTextFocus(t *testing.T) {
	m, _ := editModel(t)
	press(m, "c")
	view := ansi.Strip(m.composer.view(75, 30, m.modalLook()))
	for _, label := range []string{"Task updates", "Assignee", "Amy", "Section", "More", "tab fields"} {
		if !strings.Contains(view, label) {
			t.Errorf("missing %q in %q", label, view)
		}
	}
	press(m, "tab")
	if view := ansi.Strip(m.composer.view(75, 30, m.modalLook())); !strings.Contains(view, keyLabel("input", "save_draft")+" draft") {
		t.Errorf("field hints missing draft key in %q", view)
	}
	press(m, "shift+tab")
	// Text input returns a recurring cursor blink command after rendering.
	// Inspect the input change without draining that animation forever.
	m.Update(key("a"))
	if m.composer.footer != -1 || m.input.area.Value() != "a" {
		t.Fatal("returning from footer did not restore text focus")
	}
}

func TestCommentFooterDoesNotCaptureConfirmationKeys(t *testing.T) {
	m, writes := editModel(t)
	press(m, "c")
	m.input.area.SetValue("discard me")
	press(m, "tab", "esc", "down", "enter")
	if m.composer != nil || m.modal != nil || m.input != nil || len(*writes) != 0 {
		t.Fatal("footer captured Discard instead of closing the draft")
	}
}

func TestBatchPeopleProgressSurvivesWithoutCachedUsers(t *testing.T) {
	m, _ := editModel(t)
	press(m, "e", "m")
	change := writeback.Change{Kind: "field", Target: "f4", Value: "u2"}
	m.composer.labels = map[string]string{changeKey(change): "zed"}
	// A restored draft can submit before the people picker has loaded users.
	m.users = nil
	m.applyDraftChanges([]writeback.Change{change})
	m.openBatchReview()
	chooseBatch(t, m, "Reviewers")
	if len(m.modal.checked) != 2 || !m.modal.checked[1] {
		t.Fatal("successfully saved person lost its selection after loading users")
	}
	press(m, "down", "space", "enter")
	if len(m.composer.changes) != 1 || m.composer.changes[0].Value != "" {
		t.Fatal("successfully saved people field could not be cleared")
	}
}
