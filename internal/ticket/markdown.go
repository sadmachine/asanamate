package ticket

import (
	"fmt"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/sadmachine/asanamate/internal/asana"
)

// Markdown renders the ticket for the reading pane and for $ASANAMATE_TICKET_MD.
func (t Ticket) Markdown() string { return t.MarkdownWith("") }

// MarkdownWith renders the ticket with extra Markdown inserted after the
// fields, before the description.
func (t Ticket) MarkdownWith(extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", Clean(t.Name))
	item := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, Clean(value))
		}
	}
	status := "open"
	if t.Completed {
		status = "done"
	}
	item("Status", status)
	item("URL", t.PermalinkURL)
	if t.Assignee != nil {
		item("Assignee", t.Assignee.Name)
	}
	if t.DueOn != nil {
		item("Due", *t.DueOn)
	}
	for _, m := range t.Memberships {
		value := m.Project.Name
		if m.Section != nil {
			value += " / " + m.Section.Name
		}
		item("Project", value)
	}
	if t.AssigneeSection != nil {
		item("My Tasks section", t.AssigneeSection.Name)
	}
	item("Tags", joinNames(t.Tags, ", "))
	if t.Parent != nil {
		item("Parent", t.Parent.Name)
	}

	var fields []string
	for _, f := range t.CustomFields {
		if f.DisplayValue != nil && *f.DisplayValue != "" {
			fields = append(fields, fmt.Sprintf("- **%s:** %s", Clean(strings.TrimSpace(f.Name)), Clean(*f.DisplayValue)))
		}
	}
	section(&b, "Fields", strings.Join(fields, "\n"))
	b.WriteString(extra)
	section(&b, "Description", htmlToMarkdown(t.HTMLNotes))

	var subtasks []string
	for _, s := range t.Subtasks {
		box := " "
		if s.Completed {
			box = "x"
		}
		subtasks = append(subtasks, fmt.Sprintf("- [%s] %s", box, Clean(s.Name)))
	}
	section(&b, "Subtasks", strings.Join(subtasks, "\n"))
	section(&b, "Blocked by", bulletNames(t.Dependencies))
	section(&b, "Blocking", bulletNames(t.Dependents))

	var attachments []string
	for i, a := range t.Attachments {
		attachments = append(attachments, fmt.Sprintf("%d. [%s](%s)", i+1, Clean(a.Name), Clean(AttachmentURL(a))))
	}
	section(&b, "Attachments", strings.Join(attachments, "\n"))

	var comments []string
	for _, c := range t.Comments {
		author := "Unknown"
		if c.CreatedBy != nil {
			author = Clean(c.CreatedBy.Name)
		}
		comments = append(comments, fmt.Sprintf("### %s · %s\n\n%s", author, day(c.CreatedAt), htmlToMarkdown(c.HTMLText)))
	}
	section(&b, "Comments", strings.Join(comments, "\n\n"))
	return b.String()
}

func section(b *strings.Builder, title, body string) {
	if body != "" {
		fmt.Fprintf(b, "\n## %s\n\n%s\n", title, body)
	}
}

func joinNames(refs []asana.Ref, sep string) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return strings.Join(names, sep)
}

func bulletNames(refs []asana.Ref) string {
	var lines []string
	for _, r := range refs {
		lines = append(lines, "- "+Clean(r.Name))
	}
	return strings.Join(lines, "\n")
}

func day(timestamp string) string {
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}
	return timestamp
}

func htmlToMarkdown(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return Clean(html)
	}
	return Clean(strings.TrimSpace(md))
}
