package tui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// openLinks opens the repo links picker: linked projects first, including
// links outside the member projects, then the rest, each by name. It first
// loads the names of those outside links; one whose project no longer loads
// shows as unavailable. The cursor starts on the viewed project.
func (m *Model) openLinks() tea.Cmd {
	names := map[string]string{}
	for _, p := range m.projects {
		names[p.GID] = p.Name
	}
	var missing []string
	for gid := range m.deps.State.Repos {
		if _, ok := names[gid]; ok {
			continue
		}
		name, ok := m.linkNames[gid]
		if !ok && m.deps.Client != nil {
			missing = append(missing, gid)
		}
		names[gid] = cmp.Or(name, "unavailable project")
	}
	if len(missing) > 0 {
		m.status = "loading projects…"
		return loadLinkNames(m.deps.Client, missing)
	}
	refs := make([]asana.Ref, 0, len(names))
	for gid, name := range names {
		refs = append(refs, asana.Ref{GID: gid, Name: name})
	}
	slices.SortFunc(refs, func(a, b asana.Ref) int {
		_, al := m.deps.State.Repos[a.GID]
		_, bl := m.deps.State.Repos[b.GID]
		if al != bl {
			if al {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	items := make([]pickItem, len(refs))
	cursor := 0
	for i, r := range refs {
		hint, ok := m.deps.State.Repos[r.GID]
		if !ok {
			hint = "not linked"
		}
		items[i] = pickItem{Label: ticket.Clean(r.Name), Hint: hint, Value: r}
		if r.GID == gidOf(m.viewProject) {
			cursor = i
		}
	}
	p := newPicker(pickValue(m.pickedLinkProject), "Repo links", items)
	p.cursor = cursor
	m.modal = p
	return nil
}

// pickedLinkProject loads candidates for the chosen project's repo picker.
func (m *Model) pickedLinkProject(ref asana.Ref) tea.Cmd {
	m.modal = nil
	return loadCandidates(m.deps.Config.RepoSource.Command, &ref)
}

// openLinkRepoPicker opens the repo picker for msg.link's project, offering
// to unlink it when it is linked.
func (m *Model) openLinkRepoPicker(msg candidatesMsg) {
	ref := *msg.link
	var lead []pickItem
	if path, ok := m.deps.State.Repos[ref.GID]; ok {
		lead = append(lead, pickItem{Label: "unlink", Hint: path, Value: ""})
	}
	m.modal = repoPicker(msg, &ref, pickPath(func(path string) tea.Cmd {
		name := ticket.Clean(ref.Name)
		if path == "" {
			m.modal = nil
			m.saveLink(ref.GID, "")
			m.status = "unlinked " + name
			return nil
		}
		resolved, ok := m.resolvePicked(path)
		if !ok {
			return nil
		}
		m.saveLink(ref.GID, resolved)
		m.status = "linked " + name + " to " + resolved
		return nil
	}), lead...)
}
