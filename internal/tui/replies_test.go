package tui

import (
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestLateCandidatesCannotEndAnotherWait(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.waitForCandidates(nil, "")
	stale := m.candidatesSeq
	m.waiting = "" // cancelled
	m.waitForCandidates(nil, "")
	m.Update(candidatesMsg{seq: stale, paths: []string{"/old"}})
	if m.waiting == "" || m.modal != nil {
		t.Fatal("a cancelled wait's candidates ended the current wait")
	}
	m.Update(candidatesMsg{seq: m.candidatesSeq})
	if m.waiting != "" {
		t.Fatal("the current wait's candidates must end it")
	}
}

func TestSectionsForAnotherEditAreDropped(t *testing.T) {
	m, _ := testModel(t, config.Default())
	old := &pendingEdit{ticket: ticket.Ticket{Task: openTask}, project: "p1"}
	m.edit = &pendingEdit{ticket: ticket.Ticket{Task: sideTask}, project: "p1"}
	m.Update(sectionsMsg{edit: old, project: asana.Ref{GID: "p1"}, sections: []asana.Ref{{GID: "s1", Name: "Doing"}}})
	if m.modal != nil {
		t.Fatal("sections requested by an earlier edit opened a picker for the current one")
	}
}

func TestLinkNamesDoNotReplaceAnOpenModal(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.linkNamesSeq = 1
	open := newPicker(nil, "Other", nil)
	m.modal = open
	m.Update(linkNamesMsg{names: map[string]string{"p9": "Old"}, seq: 1})
	if m.modal != open || m.linkNames["p9"] != "Old" {
		t.Fatal("late link names must be kept without replacing the open modal")
	}
}
