package ticket

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JohannesKaufmann/dom"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

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

// Author returns the cleaned name of a story's author.
func Author(c asana.Story) string {
	if c.CreatedBy == nil {
		return "Unknown"
	}
	return Clean(c.CreatedBy.Name)
}

// Stamp returns an Asana timestamp as the local date and time, such as
// "2026-09-20 14:05", or the timestamp unchanged when it does not parse.
func Stamp(timestamp string) string {
	at, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return timestamp
	}
	return at.Local().Format("2006-01-02 15:04")
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

	var activity []string
	for _, s := range t.Timeline() {
		if s.Comment {
			activity = append(activity, fmt.Sprintf("### %s · %s\n\n%s", Author(s.Story), Stamp(s.CreatedAt), HTMLToMarkdown(s.HTMLText)))
		} else {
			activity = append(activity, fmt.Sprintf("- %s · %s · %s", Author(s.Story), Stamp(s.CreatedAt), OneLine(s.Text)))
		}
	}
	section(&b, "Activity", strings.Join(activity, "\n\n"))
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

// CommentMarkdown prepares a comment for editing. Asana resource links retain
// their identity as asana:<gid>, including mentions and inline attachments.
func CommentMarkdown(c asana.Story) string {
	return editMarkdown(c.HTMLText, c.Text)
}

// DescriptionMarkdown prepares the description for editing, keeping Asana
// resource links and inline attachments as asana:<gid> references.
func (t Ticket) DescriptionMarkdown() string {
	return editMarkdown(t.HTMLNotes, t.Description())
}

func editMarkdown(html, fallback string) string {
	if strings.TrimSpace(html) == "" {
		return Clean(fallback)
	}
	md, err := commentConverter.ConvertString(html)
	if err != nil {
		return Clean(fallback)
	}
	return Clean(strings.TrimSpace(md))
}

var commentConverter = func() *converter.Converter {
	conv := newMarkdownConverter()
	conv.Register.PreRenderer(editCommentReferences, converter.PriorityEarly)
	return conv
}()

func editCommentReferences(_ converter.Context, n *html.Node) {
	if n.Type == html.ElementNode && (n.Data == "a" || n.Data == "img") {
		gid := dom.GetAttributeOr(n, "data-asana-gid", "")
		if asana.ValidGID(gid) {
			attr := "href"
			if n.Data == "img" {
				attr = "src"
			}
			// Remove the attributes used by the display renderer to flatten
			// user mentions, and any expiring attachment download URL.
			n.Attr = []html.Attribute{{Key: attr, Val: "asana:" + gid}}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		editCommentReferences(nil, child)
	}
}

// newMarkdownConverter returns the CommonMark converter, rendering user
// mentions as text and a link whose text is its URL as an autolink.
func newMarkdownConverter() *converter.Converter {
	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(table.WithHeaderPromotion(true)),
	))
	conv.Register.RendererFor("a", converter.TagTypeInline, renderLink, converter.PriorityEarly)
	conv.Register.PreRenderer(normalizeCodeBlocks, converter.PriorityEarly)
	conv.Register.PreRenderer(newlinesToBreaks, converter.PriorityStandard)
	return conv
}

// Asana rich text marks line breaks with literal newlines, which HTML treats
// as spaces. Turn them into br tags outside code blocks.
func newlinesToBreaks(_ converter.Context, n *html.Node) {
	if n.Type == html.ElementNode && n.Data == "pre" {
		return
	}
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.TextNode && strings.Contains(child.Data, "\n") {
			for i, line := range strings.Split(child.Data, "\n") {
				if i > 0 {
					n.InsertBefore(&html.Node{Type: html.ElementNode, Data: "br", DataAtom: atom.Br}, child)
				}
				if line != "" {
					n.InsertBefore(&html.Node{Type: html.TextNode, Data: line}, child)
				}
			}
			n.RemoveChild(child)
		} else {
			newlinesToBreaks(nil, child)
		}
		child = next
	}
}

// Asana also uses bare code tags for blocks. Promote them before whitespace
// cleanup so CommonMark preserves their content and renders a fenced block.
func normalizeCodeBlocks(ctx converter.Context, n *html.Node) {
	if n.Type == html.ElementNode {
		if n.Data == "pre" {
			return
		}
		if n.Data == "code" {
			parentType, _ := ctx.GetTagType(n.Parent.Data)
			blockParent := n.Parent.Data == "body" || parentType == converter.TagTypeBlock
			if strings.ContainsAny(dom.CollectText(n), "\r\n") ||
				(blockParent && codeBoundary(ctx, n.PrevSibling, true) && codeBoundary(ctx, n.NextSibling, false)) {
				n.Data, n.DataAtom = "pre", atom.Pre
			}
			return
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		normalizeCodeBlocks(ctx, child)
	}
}

// codeBoundary reports whether code starts or ends its own line or block.
func codeBoundary(ctx converter.Context, n *html.Node, before bool) bool {
	for n != nil {
		switch n.Type {
		case html.TextNode:
			if before {
				if i := strings.LastIndexAny(n.Data, "\r\n"); i >= 0 {
					return strings.TrimSpace(n.Data[i+1:]) == ""
				}
			} else if i := strings.IndexAny(n.Data, "\r\n"); i >= 0 {
				return strings.TrimSpace(n.Data[:i]) == ""
			}
			if strings.TrimSpace(n.Data) != "" {
				return false
			}
		case html.ElementNode:
			tagType, _ := ctx.GetTagType(n.Data)
			return n.Data == "br" || tagType == converter.TagTypeBlock
		}
		if before {
			n = n.PrevSibling
		} else {
			n = n.NextSibling
		}
	}
	return true
}

func renderLink(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	if dom.GetAttributeOr(n, "data-asana-type", "") == "user" {
		ctx.RenderChildNodes(ctx, w, n)
		return converter.RenderSuccess
	}
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

// Link is a link in a ticket's rich text. Label is its cleaned text, or ""
// when the text is the URL itself.
type Link struct{ Label, URL string }

// Links returns the http(s) links in the ticket's description, then in its
// comments, in the order they appear. A URL that repeats is listed once, and
// user mentions are left out.
func (t Ticket) Links() []Link {
	sources := []string{t.HTMLNotes}
	for _, c := range t.Comments {
		sources = append(sources, c.HTMLText)
	}
	var links []Link
	seen := map[string]bool{}
	for _, src := range sources {
		doc, err := html.Parse(strings.NewReader(src))
		if err != nil {
			continue
		}
		for n := range doc.Descendants() {
			if n.Type != html.ElementNode || n.DataAtom != atom.A || dom.GetAttributeOr(n, "data-asana-type", "") == "user" {
				continue
			}
			href := OneLine(strings.TrimSpace(dom.GetAttributeOr(n, "href", "")))
			u, err := url.Parse(href)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[href] {
				continue
			}
			seen[href] = true
			label := strings.Join(strings.Fields(OneLine(dom.CollectText(n))), " ")
			if label == href {
				label = ""
			}
			links = append(links, Link{Label: label, URL: href})
		}
	}
	return links
}
