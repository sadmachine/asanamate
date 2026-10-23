package action

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestLoadFormKeepsTicketDataOutOfShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	t.Setenv("ACTION_FORM_REQUEST", path)
	source := config.ActionForm{Command: `cat > "$ACTION_FORM_REQUEST"; printf '%s' '{"fields":[{"id":"conversation","label":"Conversation","type":"select","options":[{"id":"codex:123","name":"Codex history"}]}]}'`}
	tk := ticket.Ticket{Task: asana.Task{GID: "42", Name: "$(touch should-not-run); ' \"\n"}, Comments: []asana.Story{{GID: "7", Text: "comment"}}}
	spec, err := LoadForm(context.Background(), source, tk)
	if err != nil || len(spec.Fields) != 1 || !spec.Fields[0].HasOption("codex:123") {
		t.Fatalf("form: %+v, %v", spec, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Version   int           `json:"version"`
		Operation string        `json:"operation"`
		Ticket    ticket.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(data, &req); err != nil || req.Version != 1 || req.Operation != "form" ||
		req.Ticket.GID != tk.GID || req.Ticket.Name != tk.Name || len(req.Ticket.Comments) != 1 {
		t.Fatalf("request did not preserve ticket: %s, %v", data, err)
	}
}

func TestLoadFormRejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{
		`not-json`,
		`{"fields":[]}`,
		`{"fields":[{"id":"conversation","label":"Conversation","type":"select","options":[]}]}`,
		`{"fields":[{"id":"conversation","label":"Conversation","type":"select","default":"gone","options":[{"id":"current","name":"Current"}]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Setenv("ACTION_FORM_REPLY", body)
			_, err := LoadForm(context.Background(), config.ActionForm{Command: `cat >/dev/null; printf '%s' "$ACTION_FORM_REPLY"`}, ticket.Ticket{})
			if err == nil {
				t.Fatal("invalid form accepted")
			}
		})
	}
	_, err := LoadForm(context.Background(), config.ActionForm{Command: `printf 'history unavailable' >&2; exit 1`}, ticket.Ticket{})
	if err == nil || !strings.Contains(err.Error(), "history unavailable") {
		t.Fatalf("command failure lost: %v", err)
	}
}
