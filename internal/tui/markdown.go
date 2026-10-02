package tui

import (
	"bytes"
	"strings"

	glamouransi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
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
	var out bytes.Buffer
	err := r.markdown.Renderer().Render(&out, source, doc)
	return out.String(), err
}

func (r *markdownRenderer) codeWidth(n ast.Node) int {
	width := r.width - blockInset(r.style.Document, true) - blockInset(r.style.CodeBlock.StyleBlock, false)
	for parent := n.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case ast.KindBlockquote:
			width -= blockInset(r.style.BlockQuote, true)
		case ast.KindList:
			width -= blockInset(r.style.List.StyleBlock, true)
			for ancestor := parent.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
				if ancestor.Kind() == ast.KindList {
					width -= int(r.style.List.LevelIndent)
					break
				}
			}
		}
	}
	return max(width, 1)
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
