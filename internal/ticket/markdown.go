package ticket

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/JohannesKaufmann/dom"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"

	"github.com/sadmachine/asanamate/internal/asana"
)

// Field is a cleaned label and value shown for a ticket. Key names the
// ticket property behind an editable field: KeyAssignee, KeyDue, KeyMyTasks,
// ProjectKey(gid), or FieldKey(custom field gid); it is "" otherwise.
type Field struct{ Label, Value, Key string }

// Field keys. Project and custom field keys are "<kind>:<gid>".
const (
	KeyAssignee = "assignee"
	KeyDue      = "due"
	KeyMyTasks  = "my_tasks"
	KeyProject  = "project"
	KeyField    = "field"
)

// ProjectKey is the field key of the ticket's section in a project.
func ProjectKey(gid string) string { return KeyProject + ":" + gid }

// FieldKey is the field key of a custom field.
func FieldKey(gid string) string { return KeyField + ":" + gid }

// FieldName returns a custom field's cleaned, trimmed name.
func FieldName(f asana.CustomField) string { return Clean(strings.TrimSpace(f.Name)) }

// Meta returns the ticket's built-in fields, omitting empty ones.
func (t Ticket) Meta() []Field {
	var fields []Field
	add := func(label, value, key string) {
		if value != "" {
			fields = append(fields, Field{label, Clean(value), key})
		}
	}
	add("Status", t.Status(), "")
	add("URL", t.PermalinkURL, "")
	if t.Assignee != nil {
		add("Assignee", t.Assignee.Name, KeyAssignee)
	}
	if t.DueOn != nil {
		add("Due", *t.DueOn, KeyDue)
	}
	for _, m := range t.Memberships {
		value := m.Project.Name
		if m.Section != nil {
			value += " / " + m.Section.Name
		}
		add("Project", value, ProjectKey(m.Project.GID))
	}
	if t.AssigneeSection != nil {
		add("My Tasks section", t.AssigneeSection.Name, KeyMyTasks)
	}
	add("Tags", strings.Join(asana.Names(t.Tags), ", "), "")
	if t.Parent != nil {
		add("Parent", t.Parent.Name, "")
	}
	return fields
}

// FieldValues returns the ticket's custom fields that have a value.
func (t Ticket) FieldValues() []Field {
	var fields []Field
	for _, f := range t.CustomFields {
		if f.DisplayValue != nil && *f.DisplayValue != "" {
			fields = append(fields, Field{FieldName(f), Clean(*f.DisplayValue), FieldKey(f.GID)})
		}
	}
	return fields
}

// EmptyFields returns the ticket's custom fields that have no value.
func (t Ticket) EmptyFields() []Field {
	var fields []Field
	for _, f := range t.CustomFields {
		if f.DisplayValue == nil || *f.DisplayValue == "" {
			fields = append(fields, Field{FieldName(f), "", FieldKey(f.GID)})
		}
	}
	return fields
}

// Description returns the ticket's notes as Markdown.
func (t Ticket) Description() string { return HTMLToMarkdown(t.HTMLNotes) }

// Author returns the cleaned name of a comment's author.
func Author(c asana.Story) string {
	if c.CreatedBy == nil {
		return "Unknown"
	}
	return Clean(c.CreatedBy.Name)
}

// Day returns the date part of an Asana timestamp.
func Day(timestamp string) string {
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}
	return timestamp
}

// Markdown renders the ticket for the reading pane and for $ASANAMATE_TICKET_MD.
func (t Ticket) Markdown() string { return t.MarkdownWith("") }

// MarkdownWith renders the ticket with extra Markdown inserted after the
// fields, before the description.
func (t Ticket) MarkdownWith(extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", Clean(t.Name))
	b.WriteString(bulletFields(t.Meta()))
	b.WriteString("\n")
	section(&b, "Fields", bulletFields(t.FieldValues()))
	b.WriteString(extra)
	section(&b, "Description", t.Description())

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
		comments = append(comments, fmt.Sprintf("### %s · %s\n\n%s", Author(c), Day(c.CreatedAt), HTMLToMarkdown(c.HTMLText)))
	}
	section(&b, "Comments", strings.Join(comments, "\n\n"))
	return b.String()
}

func section(b *strings.Builder, title, body string) {
	if body != "" {
		fmt.Fprintf(b, "\n## %s\n\n%s\n", title, body)
	}
}

func bulletFields(fields []Field) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- **%s:** %s", f.Label, f.Value)
	}
	return b.String()
}

func bulletNames(refs []asana.Ref) string {
	var lines []string
	for _, r := range refs {
		lines = append(lines, "- "+Clean(r.Name))
	}
	return strings.Join(lines, "\n")
}

// HTMLToMarkdown converts Asana rich text to cleaned Markdown.
func HTMLToMarkdown(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	md, err := markdownConverter.ConvertString(html)
	if err != nil {
		return Clean(html)
	}
	return Clean(strings.TrimSpace(md))
}

var markdownConverter = newMarkdownConverter()

// newMarkdownConverter returns the CommonMark converter, rendering a link
// whose text is its URL as an autolink so it shows the URL once.
func newMarkdownConverter() *converter.Converter {
	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
	))
	conv.Register.RendererFor("a", converter.TagTypeInline, renderAutolink, converter.PriorityEarly)
	return conv
}

func renderAutolink(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	href := strings.TrimSpace(dom.GetAttributeOr(n, "href", ""))
	u, err := url.Parse(href)
	if err != nil || u.Scheme == "" || strings.ContainsAny(href, " <>") ||
		strings.TrimSpace(dom.CollectText(n)) != href {
		return converter.RenderTryNext
	}
	w.WriteString("<" + href + ">")
	return converter.RenderSuccess
}

// RichPart is a run of Asana rich text converted to Markdown. ImageGID is set
// on an inline image, the attachment it shows; its Markdown is the image link.
type RichPart struct{ Markdown, ImageGID string }

var (
	imgTag      = regexp.MustCompile(`<img\b[^>]*>`)
	imgGID      = regexp.MustCompile(`\bdata-asana-gid="(\d+)"`)
	imgToken    = regexp.MustCompile(`asanamateimage(\d+)x`)
	blankMarkup = regexp.MustCompile(`^[\s\-*+>#.\d]*$`)
)

// ImageGIDs returns the attachment gids of the inline images in rich text.
func ImageGIDs(html string) []string {
	var gids []string
	for _, tag := range imgTag.FindAllString(html, -1) {
		if m := imgGID.FindStringSubmatch(tag); m != nil {
			gids = append(gids, m[1])
		}
	}
	return gids
}

// SplitImages converts rich text to Markdown split around its inline images.
// Parts left holding only list or quote markers are dropped.
func SplitImages(html string) []RichPart {
	var images []RichPart
	marked := imgTag.ReplaceAllStringFunc(html, func(tag string) string {
		m := imgGID.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		images = append(images, RichPart{Markdown: HTMLToMarkdown(tag), ImageGID: m[1]})
		return fmt.Sprintf("asanamateimage%dx", len(images)-1)
	})
	md := HTMLToMarkdown(marked)
	if len(images) == 0 {
		return []RichPart{{Markdown: md}}
	}
	var parts []RichPart
	text := func(s string) {
		if !blankMarkup.MatchString(s) {
			parts = append(parts, RichPart{Markdown: strings.TrimSpace(s)})
		}
	}
	last := 0
	for _, loc := range imgToken.FindAllStringSubmatchIndex(md, -1) {
		text(md[last:loc[0]])
		i, _ := strconv.Atoi(md[loc[2]:loc[3]])
		parts = append(parts, images[i])
		last = loc[1]
	}
	text(md[last:])
	return parts
}
