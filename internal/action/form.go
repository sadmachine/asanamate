package action

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// LoadForm returns fresh action controls. Static actions use their configured
// fields; dynamic commands receive ticket data on stdin, never in shell text.
func LoadForm(ctx context.Context, source config.ActionForm, t ticket.Ticket) (form.Spec, error) {
	if source.Command == "" {
		return source.Spec, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request := struct {
		Version   int           `json:"version"`
		Operation string        `json:"operation"`
		Ticket    ticket.Ticket `json:"ticket"`
	}{Version: 1, Operation: "form", Ticket: t}
	out, err := jsoncmd.RunBounded(ctx, "action form", source.Command, request,
		jsoncmd.Limits{Stdout: 1024 * 1024, Stderr: 64 * 1024})
	if err != nil {
		return form.Spec{}, err
	}
	var spec form.Spec
	if err := json.Unmarshal(out, &spec); err != nil {
		return form.Spec{}, fmt.Errorf("action form: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return form.Spec{}, fmt.Errorf("action form: %w", err)
	}
	return spec, nil
}
