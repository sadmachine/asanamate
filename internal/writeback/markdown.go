package writeback

import (
	"html"
	"net/url"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var commentMarkdown = goldmark.New(goldmark.WithExtensions(extension.Strikethrough))

// CommentHTML renders Markdown using only tags supported by Asana stories.
// Code and links never expand mentions. Raw HTML stays literal. mentioned
// lists the users tagged, in order.
func CommentHTML(markdown string, users []asana.Ref) (string, []asana.Ref) {
	source := []byte(markdown)
	doc := commentMarkdown.Parser().Parse(text.NewReader(source))
	var b strings.Builder
	var mentioned []asana.Ref
	var render func(ast.Node, bool)
	children := func(n ast.Node, mentions bool) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			render(c, mentions)
		}
	}
	wrap := func(n ast.Node, tag string, mentions bool) {
		b.WriteString("<" + tag + ">")
		children(n, mentions)
		b.WriteString("</" + tag + ">")
	}
	// Asana forbids pre/blockquote inside lists and lists inside blockquotes.
	inside := func(n ast.Node, kind ast.NodeKind) bool {
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Kind() == kind {
				return true
			}
		}
		return false
	}
	render = func(n ast.Node, mentions bool) {
		switch n := n.(type) {
		case *ast.Text:
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = html.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
			}
			if mentions {
				mentioned = append(mentioned, writeMentions(&b, value, users)...)
			} else {
				b.WriteString(escapeHTML.Replace(value))
			}
			if n.SoftLineBreak() || n.HardLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.CodeSpan:
			b.WriteString("<code>")
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				value := strings.ReplaceAll(string(c.(*ast.Text).Value(source)), "\n", " ")
				b.WriteString(escapeHTML.Replace(value))
			}
			b.WriteString("</code>")
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			tag := "pre"
			if inside(n, ast.KindList) {
				tag = "code"
			}
			b.WriteString("<" + tag + ">")
			b.WriteString(escapeHTML.Replace(string(n.Lines().Value(source))))
			b.WriteString("</" + tag + ">")
		case *ast.Emphasis:
			tag := "em"
			if n.Level == 2 {
				tag = "strong"
			}
			wrap(n, tag, mentions)
		case *extensionast.Strikethrough:
			wrap(n, "s", mentions)
		case *ast.Heading:
			wrap(n, "strong", mentions)
		case *ast.Blockquote:
			if inside(n, ast.KindList) {
				children(n, mentions)
			} else {
				wrap(n, "blockquote", mentions)
			}
		case *ast.List:
			if inside(n, ast.KindBlockquote) {
				children(n, mentions)
			} else if n.IsOrdered() {
				wrap(n, "ol", mentions)
			} else {
				wrap(n, "ul", mentions)
			}
		case *ast.ListItem:
			if inside(n, ast.KindBlockquote) {
				b.WriteString("- ")
				children(n, mentions)
				if n.NextSibling() != nil {
					b.WriteByte('\n')
				}
			} else {
				wrap(n, "li", mentions)
			}
		case *ast.Link:
			destination := html.UnescapeString(string(util.UnescapePunctuations(n.Destination)))
			if commentLink(&b, destination) {
				children(n, false)
				b.WriteString("</a>")
			} else {
				children(n, false)
			}
		case *ast.AutoLink:
			destination := string(n.URL(source))
			if n.AutoLinkType == ast.AutoLinkEmail {
				destination = "mailto:" + destination
			}
			linked := commentLink(&b, destination)
			b.WriteString(escapeHTML.Replace(string(n.Label(source))))
			if linked {
				b.WriteString("</a>")
			}
		case *ast.Image:
			children(n, false) // Stories do not support inline images; retain alt text.
		case *ast.RawHTML:
			for i := 0; i < n.Segments.Len(); i++ {
				segment := n.Segments.At(i)
				b.WriteString(escapeHTML.Replace(string(segment.Value(source))))
			}
		case *ast.HTMLBlock:
			b.WriteString(escapeHTML.Replace(string(n.Lines().Value(source))))
			if n.HasClosure() {
				b.WriteString(escapeHTML.Replace(string(n.ClosureLine.Value(source))))
			}
		case *ast.ThematicBreak:
			b.WriteString("---")
		default:
			children(n, mentions)
		}
		if n.Type() == ast.TypeBlock && n.Kind() != ast.KindListItem && n.NextSibling() != nil {
			b.WriteString("\n\n")
		}
	}
	b.WriteString("<body>")
	children(doc, true)
	b.WriteString("</body>")
	return b.String(), mentioned
}

// commentLink opens only links with supported, safe absolute destinations.
func commentLink(b *strings.Builder, destination string) bool {
	u, err := url.Parse(destination)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return false
		}
	case "mailto":
		if u.Opaque == "" {
			return false
		}
	default:
		return false
	}
	b.WriteString(`<a href="` + html.EscapeString(destination) + `">`)
	return true
}
