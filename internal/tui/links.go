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
// shows as unavailable. The selected ticket's own repo leads the list. The
// cursor starts on the viewed project.
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
	var items []pickItem
	if t, ok := m.selected(); ok {
		hint, ok := m.deps.State.TaskRepos[t.GID]
		if !ok {
			hint = "uses project repo"
		}
		items = append(items, pickItem{Label: "This ticket only: " + ticket.Clean(t.Name), Hint: hint, Value: linkTarget{ref: asana.Ref{GID: t.GID, Name: t.Name}, ticket: true}})
	}
	cursor := 0
	for _, r := range refs {
		hint, ok := m.deps.State.Repos[r.GID]
		if !ok {
			hint = "not linked"
		}
		if r.GID == gidOf(m.viewProject) {
			cursor = len(items)
		}
		items = append(items, pickItem{Label: ticket.Clean(r.Name), Hint: hint, Value: linkTarget{ref: r}})
	}
	p := newPicker(pickValue(m.pickedLink), "Repo links", items)
	p.cursor = cursor
	m.modal = p
	return nil
}

// pickedLink loads candidates for the chosen link's repo picker.
func (m *Model) pickedLink(target linkTarget) tea.Cmd {
	m.modal = nil
	return loadCandidates(m.deps.Config.RepoSource, &target)
}

// openLinkRepoPicker opens the repo picker for msg.link, offering to unlink
// it when it is linked.
func (m *Model) openLinkRepoPicker(msg candidatesMsg) {
	target := *msg.link
	links, project := m.deps.State.Repos, &target.ref
	if target.ticket {
		links, project = m.deps.State.TaskRepos, nil
	}
	var lead []pickItem
	if path, ok := links[target.ref.GID]; ok {
		lead = append(lead, pickItem{Label: "unlink", Hint: path, Value: ""})
	}
	m.modal = repoPicker(msg, project, pickPath(func(path string) tea.Cmd {
		name := ticket.Clean(target.ref.Name)
		if path == "" {
			m.modal = nil
			m.saveLink(target, "")
			m.status = "unlinked " + name
			return nil
		}
		resolved, ok := m.resolvePicked(path)
		if !ok {
			return nil
		}
		m.saveLink(target, resolved)
		m.status = "linked " + name + " to " + resolved
		return nil
	}), lead...)
}
