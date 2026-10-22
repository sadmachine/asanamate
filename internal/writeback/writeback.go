// Package writeback performs confirmed updates to Asana tasks.
package writeback

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/dateinput"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var (
	// ErrNoTTY means confirmation was required but no terminal was available.
	ErrNoTTY = errors.New("confirmation required but no terminal is available; pass --yes or set confirm_writes = false")
	// ErrDeclined means the user answered no.
	ErrDeclined = errors.New("cancelled")
)

// Confirmer asks the user to approve a write.
type Confirmer func(prompt string) (bool, error)

// NeedsConfirm resolves whether to confirm: --yes wins, then
// ASANAMATE_CONFIRM_WRITES ("1"/"0"), then the config default.
func NeedsConfirm(yes bool, envValue string, configDefault bool) bool {
	if yes {
		return false
	}
	switch envValue {
	case "0":
		return false
	case "1":
		return true
	}
	return configDefault
}

// TTYConfirm asks on /dev/tty, so it works even when stdin is a pipe.
func TTYConfirm(question string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, ErrNoTTY
	}
	defer tty.Close()
	return prompt.Confirm(bufio.NewReader(tty), tty, question)
}

// Service writes to Asana, asking Confirm first when it is set.
type Service struct {
	Client  *asana.Client
	Confirm Confirmer
	// Workspace scopes comment @mention lookups when Users is nil.
	Workspace string
	// Users are the people a comment can @mention; nil loads them on demand.
	Users []asana.Ref
	// Images supplies saved local image data for a draft. Other local images
	// are read from disk. Both sources are validated before uploading.
	Images map[string][]byte
}

// users returns s.Users, or loads the workspace's users when it is nil.
func (s Service) users(ctx context.Context) ([]asana.Ref, error) {
	if s.Users != nil || s.Workspace == "" {
		return s.Users, nil
	}
	return s.Client.WorkspaceUsers(ctx, s.Workspace)
}

// mentionable returns the users Markdown text can @mention, loading them only
// when the text has an @.
func (s Service) mentionable(ctx context.Context, text string) ([]asana.Ref, error) {
	if !strings.Contains(text, "@") {
		return s.Users, nil
	}
	users, err := s.users(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading people to mention: %w", err)
	}
	return users, nil
}

func (s Service) confirm(question string) error {
	if s.Confirm == nil {
		return nil
	}
	ok, err := s.Confirm(question)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDeclined
	}
	return nil
}

// Comment posts Markdown as a rich-text comment on the task. Each @Name matching a
// workspace user becomes a mention that notifies them. Each local image,
// ![alt](path), is attached to the task and shown inline.
func (s Service) Comment(ctx context.Context, gid, text string) error {
	return s.writeComment(ctx, gid, "", text)
}

// EditComment replaces an editable comment's Markdown, keeping its identity
// and reactions. Asana's current edit permission is checked before any write.
func (s Service) EditComment(ctx context.Context, storyGID, text string) error {
	return s.writeComment(ctx, "", storyGID, text)
}

// DeleteComment deletes a comment the authenticated user can edit. Asana's
// current permission is checked before asking for confirmation or deleting.
func (s Service) DeleteComment(ctx context.Context, storyGID string) error {
	c, err := s.Client.Story(ctx, storyGID)
	if err != nil {
		return err
	}
	if c.Type != "comment" || !c.IsEditable {
		return errors.New("comment cannot be deleted by the authenticated user")
	}
	if err := s.confirm(fmt.Sprintf("Permanently delete %s's comment %q?", ticket.Author(c), snippet(c.Text))); err != nil {
		return err
	}
	return s.Client.DeleteStory(ctx, storyGID)
}

func (s Service) writeComment(ctx context.Context, gid, storyGID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("comment text is empty")
	}
	var original asana.Story
	if storyGID != "" {
		var err error
		original, err = s.Client.Story(ctx, storyGID)
		if err != nil {
			return err
		}
		if original.Type != "comment" || !original.IsEditable {
			return errors.New("comment cannot be edited by the authenticated user")
		}
		if original.Target != nil {
			gid = original.Target.GID
		}
	}
	users, err := s.mentionable(ctx, text)
	if err != nil {
		return err
	}
	paths := LocalImages(text)
	if len(paths) > 0 && gid == "" {
		return errors.New("comment's task is unavailable; cannot attach images")
	}
	files := make([][]byte, len(paths))
	for i, p := range paths {
		data, saved := s.Images[p]
		var err error
		if saved {
			err = validateImage(data)
		} else {
			data, err = ReadImage(p)
		}
		if err != nil {
			return fmt.Errorf("comment image %s: %w", p, err)
		}
		files[i] = data
	}
	html, mentioned := CommentHTML(text, users, nil)
	if s.Confirm != nil {
		var question string
		if storyGID != "" {
			question = fmt.Sprintf("Edit %s's comment %q?\n%s\n", ticket.Author(original), snippet(original.Text), text)
		} else {
			t, err := s.Client.Task(ctx, gid)
			if err != nil {
				return err
			}
			question = fmt.Sprintf("Comment on %q?\n%s\n", t.Name, text)
		}
		if len(mentioned) > 0 {
			names := make([]string, len(mentioned))
			for i, u := range mentioned {
				names[i] = ticket.Clean(u.Name)
			}
			question += "Mentions: " + strings.Join(names, ", ") + "\n"
		}
		if len(paths) > 0 {
			question += "Uploads: " + strings.Join(paths, ", ") + "\n"
		}
		if err := s.confirm(question); err != nil {
			return err
		}
	}
	write := func(html string) error {
		if storyGID != "" {
			return s.Client.UpdateCommentHTML(ctx, storyGID, html)
		}
		if len(paths) == 0 && html == "<body>"+escapeHTML.Replace(text)+"</body>" {
			return s.Client.AddComment(ctx, gid, text)
		}
		return s.Client.AddCommentHTML(ctx, gid, html)
	}
	if len(paths) == 0 {
		return write(html)
	}
	// Margin for clock skew between this machine and Asana.
	since := time.Now().Add(-time.Minute)
	images := make(map[string]string, len(paths))
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
		a, err := s.Client.UploadAttachment(ctx, gid, names[i], files[i])
		if err != nil {
			return fmt.Errorf("uploading %s: %w", p, err)
		}
		images[p] = a.GID
	}
	html, _ = CommentHTML(text, users, images)
	if err := write(html); err != nil {
		return err
	}
	if err := s.removeAttachedStories(ctx, gid, since, names); err != nil {
		verb := "posted"
		if storyGID != "" {
			verb = "updated"
		}
		return fmt.Errorf("comment %s, but removing its separate image entries failed: %w", verb, err)
	}
	return nil
}

// removeAttachedStories deletes the "attached" story Asana adds for each
// upload, so an inline comment image is not also shown as its own entry.
// The attachments stay on the task.
func (s Service) removeAttachedStories(ctx context.Context, gid string, since time.Time, names []string) error {
	stories, err := s.Client.Stories(ctx, gid, since)
	if err != nil {
		return err
	}
	for _, name := range names {
		for i := len(stories) - 1; i >= 0; i-- {
			st := stories[i]
			if st.ResourceSubtype != "attachment_added" || !strings.HasSuffix(st.Text, " "+name) {
				continue
			}
			if err := s.Client.DeleteStory(ctx, st.GID); err != nil {
				return err
			}
			stories = slices.Delete(stories, i, i+1)
			break
		}
	}
	return nil
}

// ErrReactionIgnored means Asana accepted a reaction write but the story's
// reactions did not change, as when the undocumented reactions field changes.
var ErrReactionIgnored = errors.New("asana accepted the reaction but did not apply it")

// React adds the user's emoji reaction to a comment, by story gid, or removes
// it when reacted is false. It reads the comment back to check the change,
// since Asana ignores a reaction field it does not know.
func (s Service) React(ctx context.Context, storyGID, emoji string, reacted bool) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return errors.New("emoji is empty")
	}
	if s.Confirm != nil {
		c, err := s.Client.Story(ctx, storyGID)
		if err != nil {
			return err
		}
		question := "React %s to %s's comment %q?"
		if !reacted {
			question = "Remove your %s from %s's comment %q?"
		}
		if err := s.confirm(fmt.Sprintf(question, ticket.OneLine(emoji), ticket.Author(c), snippet(c.Text))); err != nil {
			return err
		}
	}
	if err := s.Client.SetReaction(ctx, storyGID, emoji, reacted); err != nil {
		return err
	}
	c, err := s.Client.Story(ctx, storyGID)
	if err != nil {
		return fmt.Errorf("checking the reaction: %w", err)
	}
	mine := slices.ContainsFunc(c.Reactions, func(r asana.Reaction) bool {
		return r.Reacted && (r.Emoji == emoji || r.Base == emoji)
	})
	if mine != reacted {
		return ErrReactionIgnored
	}
	return nil
}

// snippet shortens text to one line of at most 40 characters for a prompt.
func snippet(text string) string {
	runes := []rune(ticket.OneLine(strings.TrimSpace(text)))
	if len(runes) <= 40 {
		return string(runes)
	}
	return string(runes[:39]) + "…"
}

// maxImageSize is Asana's attachment size limit.
const maxImageSize = 100 << 20

// ReadImage reads a comment image path; a leading ~/ is the home directory.
// Only regular files whose content is an image qualify, so a comment cannot
// upload other local files.
func ReadImage(path string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, rest)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxImageSize {
		return nil, errors.New("not a regular file of at most 100 MB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, validateImage(data)
}

func validateImage(data []byte) error {
	if len(data) > maxImageSize {
		return errors.New("image exceeds 100 MB")
	}
	if !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return errors.New("not an image")
	}
	return nil
}

// MyTasks, passed to Move as the project, moves the task within My Tasks
// instead of a project.
const MyTasks = "my_tasks"

// Move puts the task in a section, by name or gid, of one of its projects, or
// of My Tasks when projectGID is MyTasks.
func (s Service) Move(ctx context.Context, gid, section, projectGID string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	list, err := s.sectionList(ctx, t, projectGID)
	if err != nil {
		return err
	}
	sec, err := s.findSection(ctx, list, section)
	if err != nil {
		return err
	}
	if err := s.confirm(fmt.Sprintf("Move %q to %s / %s?", t.Name, ticket.Clean(list.Name), ticket.Clean(sec.Name))); err != nil {
		return err
	}
	if projectGID == MyTasks {
		return s.Client.SetMyTasksSection(ctx, gid, sec.GID)
	}
	return s.Client.AddToSection(ctx, sec.GID, gid)
}

// findSection finds the section of list whose gid is section, or named
// section ignoring case and surrounding space.
func (s Service) findSection(ctx context.Context, list asana.Ref, section string) (asana.Ref, error) {
	sections, err := s.Client.Sections(ctx, list.GID)
	if err != nil {
		return asana.Ref{}, err
	}
	var names []string
	for _, sec := range sections {
		if sec.GID == section || strings.EqualFold(strings.TrimSpace(sec.Name), strings.TrimSpace(section)) {
			return sec, nil
		}
		names = append(names, ticket.Clean(sec.Name))
	}
	return asana.Ref{}, fmt.Errorf("no section %q in %s; sections: %s", section, ticket.Clean(list.Name), strings.Join(names, ", "))
}

// sectionList is the list whose sections Move picks from: one of the task's
// projects, or the user's My Tasks for MyTasks.
func (s Service) sectionList(ctx context.Context, t asana.Task, projectGID string) (asana.Ref, error) {
	if projectGID != MyTasks {
		return pickProject(t, projectGID)
	}
	// Asana returns a task's My Tasks section only to its assignee.
	if t.AssigneeSection == nil {
		return asana.Ref{}, errors.New("task is not in your My Tasks; only its assignee can move it there")
	}
	list, err := s.Client.MyTaskList(ctx, s.Workspace)
	if err != nil {
		return asana.Ref{}, err
	}
	list.Name = "My Tasks"
	return list, nil
}

// confirmTask asks question, a format whose first verb is the task's name. It
// loads the task only when there is someone to ask.
func (s Service) confirmTask(ctx context.Context, gid, question string, args ...any) error {
	if s.Confirm == nil {
		return nil
	}
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	return s.confirm(fmt.Sprintf(question, append([]any{t.Name}, args...)...))
}

func validateTitle(title string) error {
	if title == "" {
		return errors.New("title is empty")
	}
	if strings.ContainsAny(title, "\r\n") {
		return errors.New("title must be one line")
	}
	return nil
}

// SetTitle replaces the task's title with a nonempty, single-line title.
func (s Service) SetTitle(ctx context.Context, gid, title string) error {
	title = strings.TrimSpace(title)
	if err := validateTitle(title); err != nil {
		return err
	}
	if err := s.confirmTask(ctx, gid, "Rename %q to %q?", title); err != nil {
		return err
	}
	return s.Client.SetName(ctx, gid, title)
}

// SetDescription replaces the task's description with Markdown; empty clears it.
// Existing Asana references are preserved. Local image uploads are not supported.
func (s Service) SetDescription(ctx context.Context, gid, text string) error {
	text = strings.TrimSpace(text)
	html, err := s.descriptionHTML(ctx, text)
	if err != nil {
		return err
	}
	if err := s.confirmTask(ctx, gid, "Replace the description of %q?\n%s\n", text); err != nil {
		return err
	}
	return s.Client.SetHTMLNotes(ctx, gid, html)
}

func (s Service) descriptionHTML(ctx context.Context, text string) (string, error) {
	if len(LocalImages(text)) > 0 {
		return "", errors.New("description image uploads are not supported; keep existing asana:<gid> images or use links")
	}
	users, err := s.mentionable(ctx, text)
	if err != nil {
		return "", err
	}
	html, _ := CommentHTML(text, users, nil)
	return html, nil
}

// Assign assigns the task to a workspace user, by full name or gid, or to
// "me", the token's owner. An empty user unassigns it.
func (s Service) Assign(ctx context.Context, gid, user string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		if err := s.confirmTask(ctx, gid, "Unassign %q?"); err != nil {
			return err
		}
		return s.Client.SetAssignee(ctx, gid, "")
	}
	u, err := s.assignee(ctx, user)
	if err != nil {
		return err
	}
	if err := s.confirmTask(ctx, gid, "Assign %q to %s?", ticket.Clean(u.Name)); err != nil {
		return err
	}
	return s.Client.SetAssignee(ctx, gid, u.GID)
}

// assignee resolves user, a full name or gid, to a workspace user, or "me"
// to the token's owner, whose gid is "me" and name is "you".
func (s Service) assignee(ctx context.Context, user string) (asana.Ref, error) {
	if strings.EqualFold(user, "me") {
		return asana.Ref{GID: "me", Name: "you"}, nil
	}
	users, err := s.users(ctx)
	if err != nil {
		return asana.Ref{}, fmt.Errorf("loading people: %w", err)
	}
	return findUser(users, user)
}

// SetDue sets the task's due date, typed as dateinput.Resolve accepts; an
// empty date clears it.
func (s Service) SetDue(ctx context.Context, gid, date string) error {
	date = strings.TrimSpace(date)
	question, args := "Clear the due date of %q?", []any{}
	if date != "" {
		var err error
		if date, err = dateinput.Resolve(ctx, date); err != nil {
			return fmt.Errorf("due date: %w", err)
		}
		question, args = "Set the due date of %q to %s?", []any{date}
	}
	if err := s.confirmTask(ctx, gid, question, args...); err != nil {
		return err
	}
	return s.Client.SetDueOn(ctx, gid, date)
}

// Complete marks the task complete, or reopens it when done is false.
func (s Service) Complete(ctx context.Context, gid string, done bool) error {
	question := "Reopen %q?"
	if done {
		question = "Mark %q complete?"
	}
	if err := s.confirmTask(ctx, gid, question); err != nil {
		return err
	}
	return s.Client.SetCompleted(ctx, gid, done)
}

// AddProject adds the task to a project, keeping its other projects.
func (s Service) AddProject(ctx context.Context, gid, projectGID string) error {
	if projectGID == "" {
		return errors.New("project gid is empty")
	}
	if s.Confirm != nil {
		p, err := s.Client.Project(ctx, projectGID)
		if err != nil {
			return err
		}
		if err := s.confirmTask(ctx, gid, "Add %q to project %s?", ticket.Clean(p.Name)); err != nil {
			return err
		}
	}
	return s.Client.AddToProject(ctx, gid, projectGID)
}

// RemoveProject removes the task from one of its projects.
func (s Service) RemoveProject(ctx context.Context, gid, projectGID string) error {
	if projectGID == "" {
		return errors.New("project gid is empty")
	}
	if s.Confirm != nil {
		t, err := s.Client.Task(ctx, gid)
		if err != nil {
			return err
		}
		p, err := pickProject(t, projectGID)
		if err != nil {
			return err
		}
		if err := s.confirm(fmt.Sprintf("Remove %q from project %s?", t.Name, ticket.Clean(p.Name))); err != nil {
			return err
		}
	}
	return s.Client.RemoveFromProject(ctx, gid, projectGID)
}

// NewTicket is a ticket for Create to make. Empty fields are left unset.
type NewTicket struct {
	Name     string
	Notes    string // Markdown, as Comment reads it, without image uploads
	Parent   string // a task gid; makes the ticket its subtask
	Project  string // a project gid
	Section  string // a section of Project, by name or gid
	Assignee string // a full name, gid, or "me"
	Due      string // as dateinput.Resolve accepts
}

// Create makes a ticket and returns it: a subtask of Parent, a ticket in
// Project (and Section), or else a ticket in My Tasks, assigned to the user
// unless Assignee names someone else.
func (s Service) Create(ctx context.Context, nt NewTicket) (asana.Task, error) {
	task := asana.NewTask{Name: strings.TrimSpace(nt.Name), Project: nt.Project, DueOn: strings.TrimSpace(nt.Due)}
	if err := validateTitle(task.Name); err != nil {
		return asana.Task{}, err
	}
	switch {
	case nt.Parent != "" && nt.Project != "":
		return asana.Task{}, errors.New("a subtask goes under its parent; pass a parent or a project, not both")
	case nt.Section != "" && nt.Project == "":
		return asana.Task{}, errors.New("a section needs a project")
	case nt.Parent == "" && nt.Project == "" && s.Workspace == "":
		return asana.Task{}, errors.New("no workspace to create the ticket in")
	}
	if task.DueOn != "" {
		due, err := dateinput.Resolve(ctx, task.DueOn)
		if err != nil {
			return asana.Task{}, fmt.Errorf("due date: %w", err)
		}
		task.DueOn = due
	}
	assignee := strings.TrimSpace(nt.Assignee)
	if assignee == "" && nt.Parent == "" && nt.Project == "" {
		assignee = "me"
	}
	var details string
	if assignee != "" {
		u, err := s.assignee(ctx, assignee)
		if err != nil {
			return asana.Task{}, err
		}
		task.Assignee, details = u.GID, "Assignee: "+ticket.Clean(u.Name)+"\n"
	}
	if task.DueOn != "" {
		details += "Due: " + task.DueOn + "\n"
	}
	if notes := strings.TrimSpace(nt.Notes); notes != "" {
		users, err := s.mentionable(ctx, notes)
		if err != nil {
			return asana.Task{}, err
		}
		task.HTMLNotes, _ = CommentHTML(notes, users, nil)
		details += "Notes:\n" + notes + "\n"
	}
	where, err := s.placeNew(ctx, &task, nt)
	if err != nil {
		return asana.Task{}, err
	}
	if err := s.confirm(fmt.Sprintf("Create %q %s?\n%s", task.Name, where, details)); err != nil {
		return asana.Task{}, err
	}
	if nt.Parent != "" {
		return s.Client.CreateSubtask(ctx, nt.Parent, task)
	}
	return s.Client.CreateTask(ctx, task)
}

// placeNew sets where task goes: its section gid in a project, or the
// workspace of a My Tasks ticket. It returns where that is for the
// confirmation, loading names only when there is someone to ask.
func (s Service) placeNew(ctx context.Context, task *asana.NewTask, nt NewTicket) (string, error) {
	switch {
	case nt.Parent != "":
		if s.Confirm == nil {
			return "", nil
		}
		parent, err := s.Client.Task(ctx, nt.Parent)
		return fmt.Sprintf("as a subtask of %q", parent.Name), err
	case nt.Project == "":
		task.Workspace = s.Workspace
		if task.Assignee == "me" {
			return "in My Tasks", nil
		}
		return "in the assignee's My Tasks", nil
	}
	project := asana.Ref{GID: nt.Project, Name: "project " + nt.Project}
	if s.Confirm != nil {
		var err error
		if project, err = s.Client.Project(ctx, nt.Project); err != nil {
			return "", err
		}
	}
	where := "in " + ticket.Clean(project.Name)
	if nt.Section != "" {
		sec, err := s.findSection(ctx, project, nt.Section)
		if err != nil {
			return "", err
		}
		task.Section, where = sec.GID, where+" / "+ticket.Clean(sec.Name)
	}
	return where, nil
}

func pickProject(t asana.Task, projectGID string) (asana.Ref, error) {
	var choices []string
	for _, m := range t.Memberships {
		if m.Project.GID == projectGID {
			return m.Project, nil
		}
		choices = append(choices, fmt.Sprintf("%s (%s)", ticket.Clean(m.Project.Name), m.Project.GID))
	}
	switch {
	case projectGID != "":
		return asana.Ref{}, fmt.Errorf("task is not in project %s", projectGID)
	case len(t.Memberships) == 0:
		return asana.Ref{}, errors.New("task is not in any project")
	case len(t.Memberships) == 1:
		return t.Memberships[0].Project, nil
	}
	return asana.Ref{}, fmt.Errorf("task is in several projects; pass --project with one of: %s", strings.Join(choices, ", "))
}

// SetField sets a custom field, named or by gid, from text, as FieldValue
// reads it; a people field takes comma-separated full names or gids of
// workspace users. An empty value clears it. When the task has several fields
// with that name, projectGID picks the one attached to that project.
func (s Service) SetField(ctx context.Context, gid, fieldName, value, projectGID string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	f, err := s.resolveField(ctx, t, fieldName, projectGID)
	if err != nil {
		return err
	}
	v, err := s.fieldValue(ctx, f, value)
	if err != nil {
		return err
	}
	if err := s.confirm(fmt.Sprintf("Set %q on %q to %q?", ticket.FieldName(f), t.Name, value)); err != nil {
		return err
	}
	return s.Client.SetCustomField(ctx, gid, f.GID, v)
}

// resolveField finds the one field to write. Writes never guess between
// same-named fields: they need the project that owns the intended one.
func (s Service) resolveField(ctx context.Context, t asana.Task, name, projectGID string) (asana.CustomField, error) {
	matches := t.FieldsNamed(name)
	for _, f := range t.CustomFields {
		if f.GID == name {
			matches = []asana.CustomField{f}
		}
	}
	switch {
	case len(matches) == 0:
		return asana.CustomField{}, fmt.Errorf("task has no custom field %q", name)
	case len(matches) == 1:
		return matches[0], nil
	case projectGID == "":
		return asana.CustomField{}, fmt.Errorf("task has %d custom fields named %q; pass --project with the gid of the project whose field to set", len(matches), name)
	}
	onProject, err := s.Client.ProjectFieldGIDs(ctx, projectGID)
	if err != nil {
		return asana.CustomField{}, err
	}
	for _, f := range matches {
		if onProject[f.GID] {
			return f, nil
		}
	}
	return asana.CustomField{}, fmt.Errorf("none of the fields named %q is on project %s", name, projectGID)
}

// fieldValue is FieldValue, plus people fields, whose names it resolves to
// workspace users.
func (s Service) fieldValue(ctx context.Context, f asana.CustomField, value string) (any, error) {
	if f.ResourceSubtype != asana.FieldPeople || value == "" {
		return FieldValue(ctx, f, value)
	}
	users, err := s.users(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading people: %w", err)
	}
	names, err := splitList(f, value)
	if err != nil {
		return nil, err
	}
	gids := make([]string, 0, len(names))
	for _, name := range names {
		u, err := findUser(users, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ticket.FieldName(f), err)
		}
		gids = appendNew(gids, u.GID)
	}
	return gids, nil
}

// findUser finds the user whose gid is name, or the one user with that full
// name (ignoring case), refusing a name several users share.
func findUser(users []asana.Ref, name string) (asana.Ref, error) {
	var match []asana.Ref
	for _, u := range users {
		if u.GID == name {
			return u, nil
		}
		if strings.EqualFold(strings.TrimSpace(u.Name), name) {
			match = append(match, u)
		}
	}
	switch len(match) {
	case 0:
		return asana.Ref{}, fmt.Errorf("no user named %q", name)
	case 1:
		return match[0], nil
	}
	return asana.Ref{}, fmt.Errorf("several users are named %q", name)
}

// splitList splits comma-separated names, refusing empty ones.
func splitList(f asana.CustomField, value string) ([]string, error) {
	names := strings.Split(value, ",")
	for i, n := range names {
		if names[i] = strings.TrimSpace(n); names[i] == "" {
			return nil, fmt.Errorf("%s: empty name in %q", ticket.FieldName(f), value)
		}
	}
	return names, nil
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// optionGID finds the enum option whose gid is name, or named name ignoring
// case.
func optionGID(f asana.CustomField, name string) (string, error) {
	var options []string
	for _, o := range f.EnumOptions {
		if o.GID == name || strings.EqualFold(o.Name, name) {
			return o.GID, nil
		}
		options = append(options, ticket.Clean(o.Name))
	}
	return "", fmt.Errorf("%s has no option %q; options: %s", ticket.FieldName(f), name, strings.Join(options, ", "))
}

// FieldValue converts typed text to the API value for a text, number, enum,
// multi_enum (comma-separated option names), or date (as dateinput.Resolve
// accepts) field. Option gids work in place of names. Empty text is nil,
// which clears the field.
func FieldValue(ctx context.Context, f asana.CustomField, value string) (any, error) {
	name := ticket.FieldName(f)
	if value == "" {
		return nil, nil
	}
	switch f.ResourceSubtype {
	case asana.FieldText:
		return value, nil
	case asana.FieldNumber:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("%s needs a number, got %q", name, value)
		}
		return n, nil
	case asana.FieldEnum:
		return optionGID(f, value)
	case asana.FieldMultiEnum:
		names, err := splitList(f, value)
		if err != nil {
			return nil, err
		}
		gids := make([]string, 0, len(names))
		for _, n := range names {
			gid, err := optionGID(f, n)
			if err != nil {
				return nil, err
			}
			gids = appendNew(gids, gid)
		}
		return gids, nil
	case asana.FieldDate:
		date, err := dateinput.Resolve(ctx, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return map[string]any{"date": date}, nil
	}
	return nil, fmt.Errorf("%s: custom field type %q is not supported", name, f.ResourceSubtype)
}
