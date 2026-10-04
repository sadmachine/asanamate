package tui

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	glamouransi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownRenderer uses Glamour's layout with literal, prewrapped code blocks.
// Wrapping before layout keeps the block gutter on every continuation line.
type markdownRenderer struct {
	markdown goldmark.Markdown
	style    glamouransi.StyleConfig
	width    int
	wrapMark string
}

func newMarkdownRenderer(theme string, width int, bare bool, wrapMark string) *markdownRenderer {
	style := *styles.DefaultStyles[theme]
	if bare {
		style.Document.Margin = new(uint)
		style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	}
	style.CodeBlock.Chroma = nil
	style.CodeBlock.Theme = ""
	// Render the two-column gutter ourselves to mark soft wraps only.
	style.CodeBlock.Margin = new(uint)
	style.CodeBlock.Color = style.Document.Color
	style.CodeBlock.BackgroundColor = style.Code.BackgroundColor
	// Glamour's plain-code fallback unescapes Markdown punctuation. Escape
	// backslashes first so code, including shell escapes, remains literal.
	style.CodeBlock.Format = `{{ Replace .text "\\" "\\\\" -1 }}`
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	md.SetRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(
		util.Prioritized(glamouransi.NewRenderer(glamouransi.Options{
			Styles: style, WordWrap: width,
		}), 1000),
	)))
	return &markdownRenderer{markdown: md, style: style, width: width, wrapMark: wrapMark}
}

func (r *markdownRenderer) Render(md string) (string, error) {
	source := []byte(md)
	doc := r.markdown.Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || (n.Kind() != ast.KindCodeBlock && n.Kind() != ast.KindFencedCodeBlock) {
			return ast.WalkContinue, nil
		}
		width := max(r.codeWidth(n)-2, 1)
		var code strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			segment := n.Lines().At(i)
			line := strings.TrimSuffix(string(segment.Value(source)), "\n")
			line = strings.ReplaceAll(line, "\t", "    ")
			indent := min(len(line)-len(strings.TrimLeft(line, " ")), width-1)
			padding := strings.Repeat(" ", indent)
			wrapped := ansi.Wrap(line[indent:], width-indent, "")
			for j, row := range strings.Split(wrapped, "\n") {
				gutter := "  "
				if j > 0 {
					// Reset only intensity; keep the code foreground and background.
					gutter = "\x1b[2m" + r.wrapMark + "\x1b[22m "
				}
				code.WriteString(gutter)
				row = padding + row
				code.WriteString(row)
				code.WriteString(strings.Repeat(" ", max(width-ansi.StringWidth(row), 0)))
				code.WriteByte('\n')
			}
		}
		start := len(source)
		source = append(source, code.String()...)
		lines := text.NewSegments()
		lines.Append(text.NewSegment(start, len(source)))
		n.SetLines(lines)
		return ast.WalkSkipChildren, nil
	})
	source, err := r.renderLists(source, doc)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = r.markdown.Renderer().Render(&out, source, doc)
	return out.String(), err
}

func (r *markdownRenderer) renderLists(source []byte, doc ast.Node) ([]byte, error) {
	// Lay out each item separately so its marker is a hanging indent rather
	// than part of the text Glamour wraps at the list's left edge.
	var lists []*ast.List
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if list, ok := n.(*ast.List); ok && entering {
			lists = append(lists, list)
		}
		return ast.WalkContinue, nil
	})
	for i := len(lists) - 1; i >= 0; i-- {
		list := lists[i]
		var body strings.Builder
		body.WriteByte('\n')
		for item := list.FirstChild(); item != nil; item = item.NextSibling() {
			prefix := r.listPrefix(item)
			indent := strings.Repeat(" ", ansi.StringWidth(prefix))
			itemDoc := ast.NewDocument()
			var children []ast.Node
			for child := item.FirstChild(); child != nil; child = item.FirstChild() {
				children = append(children, child)
				itemDoc.AppendChild(itemDoc, child)
			}
			style := r.style
			style.Document.Margin = new(uint)
			style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
			var rendered bytes.Buffer
			itemRenderer := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(
				glamouransi.NewRenderer(glamouransi.Options{Styles: style, WordWrap: max(r.contentWidth(list)-len(indent), 1)}), 1000,
			)))
			err := itemRenderer.Render(&rendered, source, itemDoc)
			for _, child := range children {
				item.AppendChild(item, child)
			}
			if err != nil {
				return nil, err
			}
			for row, line := range strings.Split(strings.Trim(rendered.String(), "\n"), "\n") {
				if row == 0 {
					body.WriteString(prefix)
				} else {
					body.WriteString(indent)
				}
				body.WriteString(line)
				body.WriteByte('\n')
			}
		}
		// Text nodes pass through Glamour's entity and Markdown unescaping.
		literal := html.EscapeString(strings.ReplaceAll(body.String(), "\\", "\\\\"))
		start := len(source)
		source = append(source, literal...)
		list.Parent().ReplaceChild(list.Parent(), list, ast.NewTextSegment(text.NewSegment(start, len(source))))
	}
	return source, nil
}

func (r *markdownRenderer) listPrefix(item ast.Node) string {
	if child := item.FirstChild(); child != nil {
		if checkbox, ok := child.FirstChild().(*extast.TaskCheckBox); ok {
			if checkbox.IsChecked {
				return r.style.Task.Ticked
			}
			return r.style.Task.Unticked
		}
	}
	if item.Parent().(*ast.List).IsOrdered() {
		number := item.Parent().(*ast.List).Start
		for sibling := item.PreviousSibling(); sibling != nil; sibling = sibling.PreviousSibling() {
			number++
		}
		return fmt.Sprint(number) + r.style.Enumeration.BlockPrefix
	}
	return r.style.Item.BlockPrefix
}

func (r *markdownRenderer) contentWidth(n ast.Node) int {
	width := r.width - blockInset(r.style.Document, true)
	for parent := n.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case ast.KindBlockquote:
			width -= blockInset(r.style.BlockQuote, true)
		case ast.KindList:
			width -= blockInset(r.style.List.StyleBlock, true)
		case ast.KindListItem:
			width -= ansi.StringWidth(r.listPrefix(parent))
		}
	}
	return max(width, 1)
}

func (r *markdownRenderer) codeWidth(n ast.Node) int {
	return max(r.contentWidth(n)-blockInset(r.style.CodeBlock.StyleBlock, false), 1)
}

func blockInset(style glamouransi.StyleBlock, bothMargins bool) int {
	var inset uint
	if style.Indent != nil {
		tokenWidth := 1
		if style.IndentToken != nil {
			tokenWidth = ansi.StringWidth(*style.IndentToken)
		}
		inset += *style.Indent * uint(tokenWidth)
	}
	if style.Margin != nil {
		inset += *style.Margin
		if bothMargins {
			inset += *style.Margin
		}
	}
	return int(inset)
}
