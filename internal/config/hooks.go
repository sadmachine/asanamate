package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Hook runs a built-in prompt or an action when an event happens to a ticket
// that matches a filter.
type Hook struct {
	// On is the event: HookTicketOpen, HookAssign, HookComplete, or
	// HookNewActivity.
	On string `toml:"on"`
	// When is a list filter query checked against the event's ticket; empty
	// matches every ticket.
	When string `toml:"when"`
	// Once runs the hook at most once per ticket, remembered in state.
	Once bool `toml:"once"`
	// Run is HookLogTime, "edit:<item>" with an item from HookEdits, or
	// "action:<file>" naming an action file without .toml.
	Run string `toml:"run"`
}

// Hook events.
const (
	HookTicketOpen  = "ticket_open"
	HookAssign      = "assign"
	HookComplete    = "complete"
	HookNewActivity = "new_activity"
)

// HookEvents lists the events hooks.on accepts.
var HookEvents = []string{HookTicketOpen, HookAssign, HookComplete, HookNewActivity}

// HookLogTime is the target that opens the log time form.
const HookLogTime = "log_time"

// HookEdits lists the edit menu items a hook can open: the ones that ask for
// a value.
var HookEdits = []string{"title", "description", "section", "field", "assign", "due", "add_project", "remove_project", "branch"}

// Target splits Run into its kind ("log_time", "edit", or "action") and the
// edit item or action file it names.
func (h Hook) Target() (kind, name string) {
	if k, n, ok := strings.Cut(h.Run, ":"); ok {
		return k, n
	}
	return h.Run, ""
}

// Key identifies the hook for its once records. Editing the hook changes it,
// so the edited hook starts fresh.
func (h Hook) Key() string {
	return h.On + "|" + h.When + "|" + h.Run
}

func (h Hook) validate(timeTracking bool) error {
	if !slices.Contains(HookEvents, h.On) {
		return fmt.Errorf("on: %q must be one of %s", h.On, strings.Join(HookEvents, ", "))
	}
	switch kind, name := h.Target(); {
	case h.Run == HookLogTime:
		if !timeTracking {
			return errors.New("run: log_time needs [time_tracking]")
		}
	case kind == "edit" && slices.Contains(HookEdits, name):
	case kind == "action" && name != "":
	default:
		return fmt.Errorf("run: %q must be log_time, edit:<item> (%s), or action:<file>", h.Run, strings.Join(HookEdits, ", "))
	}
	if h.On == HookNewActivity && !strings.HasPrefix(h.Run, "action:") {
		return errors.New("run: new_activity hooks run background actions only")
	}
	return nil
}

// HookAction returns the action a hook's action:<file> target names.
func (c Config) HookAction(h Hook) (Action, bool) {
	kind, file := h.Target()
	if kind != "action" {
		return Action{}, false
	}
	i := slices.IndexFunc(c.Actions, func(a Action) bool { return a.File == file })
	if i < 0 {
		return Action{}, false
	}
	return c.Actions[i], true
}

// validateHookActions checks that action targets name loaded actions, and
// that new_activity ones never ask for anything.
func (c Config) validateHookActions() error {
	for i, h := range c.Hooks {
		kind, file := h.Target()
		if kind != "action" {
			continue
		}
		a, ok := c.HookAction(h)
		if !ok {
			return fmt.Errorf("hooks[%d].run: no action file actions/%s.toml", i+1, file)
		}
		if h.On == HookNewActivity && a.Interactive() {
			return fmt.Errorf("hooks[%d].run: actions/%s.toml must be a background action without form, input, repo, or agent to run on new_activity", i+1, file)
		}
	}
	return nil
}
