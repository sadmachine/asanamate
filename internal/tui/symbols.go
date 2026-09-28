package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/config"
)

// symbolSet holds the ticket markers and agent state symbols for one style.
type symbolSet struct {
	open, done string
	states     map[agents.State]string
	spinner    []string        // frames shown for working agents; nil means static
	border     lipgloss.Border // reading pane cards and section rules
}

var braille = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var symbolSets = map[string]symbolSet{
	config.SymbolsUnicode: {open: "□", done: "✓", spinner: braille, border: lipgloss.RoundedBorder(), states: map[agents.State]string{
		agents.Waiting: "⚠", agents.Working: "◐", agents.Completed: "●", agents.Idle: "○", agents.Unknown: "?"}},
	// Nerd Font (Font Awesome) glyphs; needs a Nerd Font.
	config.SymbolsNerd: {open: "", done: "", spinner: braille, states: map[agents.State]string{
		agents.Waiting: "", agents.Working: "", agents.Completed: "", agents.Idle: "", agents.Unknown: ""}},
	config.SymbolsASCII: {open: "[ ]", done: "[x]", border: lipgloss.ASCIIBorder(), states: map[agents.State]string{
		agents.Waiting: "(!)", agents.Working: "(~)", agents.Completed: "(+)", agents.Idle: "(-)", agents.Unknown: "(?)"}},
}

var stateStyles = map[agents.State]lipgloss.Style{
	agents.Waiting:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	agents.Working:   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	agents.Completed: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	agents.Idle:      dimStyle,
	agents.Unknown:   dimStyle,
}

// newSymbols returns the named set with per-state overrides applied. An
// overridden working symbol, or reduced motion, turns the spinner off.
func newSymbols(set string, overrides map[string]string, reducedMotion bool) symbolSet {
	base, ok := symbolSets[set]
	if !ok {
		base = symbolSets[config.SymbolsUnicode]
	}
	s := base
	s.states = make(map[agents.State]string, len(base.states))
	for k, v := range base.states {
		s.states[k] = v
	}
	for k, v := range overrides {
		s.states[agents.State(k)] = v
	}
	if reducedMotion || overrides[string(agents.Working)] != "" {
		s.spinner = nil
	}
	return s
}

// agent returns the symbol for a state; working agents animate by frame.
func (s symbolSet) agent(state agents.State, frame int) string {
	if state == agents.Working && s.spinner != nil {
		return s.spinner[frame%len(s.spinner)]
	}
	return s.states[state]
}

// badge renders agents (already in urgency order) as one symbol each, or as
// grouped counts when there are more than four.
func (s symbolSet) badge(list []agents.Agent, frame int) string {
	if len(list) == 0 {
		return ""
	}
	var parts []string
	if len(list) <= 4 {
		for _, a := range list {
			parts = append(parts, stateStyles[a.State].Render(s.agent(a.State, frame)))
		}
	} else {
		for _, st := range agents.States {
			if n := countState(list, st); n > 0 {
				parts = append(parts, stateStyles[st].Render(fmt.Sprintf("%s%d", s.agent(st, frame), n)))
			}
		}
	}
	return strings.Join(parts, " ")
}

// summary renders grouped counts, for the header.
func (s symbolSet) summary(list []agents.Agent, frame int) string {
	var parts []string
	for _, st := range agents.States {
		if n := countState(list, st); n > 0 {
			parts = append(parts, stateStyles[st].Render(fmt.Sprintf("%s%d", s.agent(st, frame), n)))
		}
	}
	return strings.Join(parts, " ")
}

func countState(list []agents.Agent, st agents.State) int {
	n := 0
	for _, a := range list {
		if a.State == st {
			n++
		}
	}
	return n
}
