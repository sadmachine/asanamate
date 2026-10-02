package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestMarkdownCodeBlocks(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		for _, bare := range []bool{true, false} {
			for _, width := range []int{20, 40, 80} {
				t.Run(fmt.Sprintf("%s/bare=%v/width=%d", theme, bare, width), func(t *testing.T) {
					m, _ := testModel(t, config.Config{Theme: theme})
					code := "    " + strings.Repeat("word ", 22) + strings.Repeat("界", 8)
					for _, wrapper := range []string{"```php\n%s\n```", "```\n%s\n```", "> ```php\n> %s\n> ```", "- item\n\n  ```php\n  %s\n  ```", "    %s"} {
						out := m.glamour(fmt.Sprintf(wrapper, code), width, bare)
						rows := 0
						for _, row := range strings.Split(out, "\n") {
							plain := ansi.Strip(row)
							if ansi.StringWidth(row) > width {
								t.Fatalf("row exceeds width %d: %q", width, plain)
							}
							if strings.Contains(plain, "word") {
								rows++
								plain = strings.TrimPrefix(strings.TrimLeft(plain, " "), "│ ")
								// Four source spaces survive on every wrapped row.
								if strings.Contains(wrapper, ">") && !strings.Contains(plain, "    word") {
									t.Fatalf("lost code indent: %q", ansi.Strip(row))
								}
								if !strings.Contains(ansi.Strip(row), "    word") {
									t.Fatalf("lost code indent: %q", ansi.Strip(row))
								}
								if !strings.Contains(row, ";48;5;") {
									t.Fatalf("missing code background: %q", row)
								}
							}
						}
						if rows < 2 {
							t.Fatalf("code did not wrap: %q", out)
						}
					}
				})
			}
		}
	}
}

func TestCodeWrapMarkerOnlyOnContinuations(t *testing.T) {
	for _, symbols := range []string{config.SymbolsUnicode, config.SymbolsNerd, config.SymbolsASCII} {
		for _, theme := range []string{"dark", "light"} {
			m, _ := testModel(t, config.Config{Theme: theme, Symbols: symbols})
			m.sym = newSymbols(symbols, nil, false)
			out := m.renderBody("```\n    "+strings.Repeat("word ", 12)+"\n    next original line\n\n    last line\n```", 30)
			marker := symbolSets[symbols].codeWrap
			wrapped := 0
			for _, row := range strings.Split(out, "\n") {
				plain := ansi.Strip(row)
				if strings.Contains(plain, "word") {
					if wrapped == 0 {
						if !strings.HasPrefix(plain, "      word") {
							t.Fatalf("original line has a marker: %q", plain)
						}
					} else if !strings.HasPrefix(plain, marker+"     word") || !strings.Contains(row, "\x1b[2m"+marker+"\x1b[22m") {
						t.Fatalf("continuation needs a dim %q marker and preserved indent: plain=%q, styled=%q", marker, plain, row)
					}
					wrapped++
				} else if strings.Contains(plain, marker) {
					t.Fatalf("marker on an original or blank line: %q", plain)
				}
			}
			if wrapped < 2 {
				t.Fatalf("expected wrapped code: %q", out)
			}
		}
	}
}

func TestCommentCodeIsLiteralAndMonochrome(t *testing.T) {
	const code = "php8.4 --version\n\\* \\` \\_ \\! \\| \\\\ C:\\path\\file\n&lt;tag&gt; 42 'text'"
	md := ticket.HTMLToMarkdown("<body><pre>" + code + "</pre></body>")
	colors := regexp.MustCompile(`\x1b\[([^m]*38;5;\d+[^m]*)m`)
	for _, theme := range []string{"dark", "light"} {
		m, _ := testModel(t, config.Config{Theme: theme})
		out := m.renderBody(md, 80)
		for _, line := range strings.Split(strings.ReplaceAll(code, "&lt;tag&gt;", "<tag>"), "\n") {
			if !strings.Contains(ansi.Strip(out), line) {
				t.Fatalf("lost literal code %q: %q", line, ansi.Strip(out))
			}
		}
		for _, match := range colors.FindAllStringSubmatch(out, -1) {
			want := "38;5;" + *styles.DefaultStyles[theme].Document.Color
			if !strings.HasPrefix(match[1], want) {
				t.Fatalf("unexpected code color: %q", match[0])
			}
		}
	}
}

func TestMarkdownProseUnchanged(t *testing.T) {
	const md = "# Heading\n\nNormal **bold**, _italic_, `inline`, and [link](https://example.com).\n\n> quote\n\n| A | B |\n|---|---|\n| C | D |"
	for _, theme := range []string{"dark", "light"} {
		for _, bare := range []bool{true, false} {
			style := *styles.DefaultStyles[theme]
			if bare {
				style.Document.Margin = new(uint)
				style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
			}
			original, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(40))
			if err != nil {
				t.Fatal(err)
			}
			want, err := original.Render(md)
			if err != nil {
				t.Fatal(err)
			}
			m, _ := testModel(t, config.Config{Theme: theme})
			if got := m.glamour(md, 40, bare); got != want {
				t.Fatalf("%s bare=%v: prose rendering changed", theme, bare)
			}
		}
	}
}

func TestMarkdownListHangingIndent(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		for _, bare := range []bool{true, false} {
			for _, tt := range []struct {
				name, markdown, prefix string
			}{
				{"bullet", "- hello " + strings.Repeat("continuation ", 8), "• hello"},
				{"number", "9. hello " + strings.Repeat("continuation ", 8), "9. hello"},
				{"wide number", "10. hello " + strings.Repeat("continuation ", 8), "10. hello"},
				{"number transition", "9. parent\n10. hello " + strings.Repeat("continuation ", 8), "10. hello"},
				{"task", "- [x] hello " + strings.Repeat("continuation ", 8), "[✓] hello"},
				{"styled", "- **hello** " + strings.Repeat("_continuation_ ", 8), "• hello"},
				{"explicit break", "- hello  \n  continuation", "• hello"},
				{"nested", "- parent\n  - hello " + strings.Repeat("continuation ", 8), "• hello"},
				{"quote", "> - hello " + strings.Repeat("continuation ", 8), "• hello"},
			} {
				t.Run(fmt.Sprintf("%s/%v/%s", theme, bare, tt.name), func(t *testing.T) {
					m, _ := testModel(t, config.Config{Theme: theme})
					out := ansi.Strip(m.glamour(tt.markdown, 32, bare))
					column, continuations := -1, 0
					for _, line := range strings.Split(out, "\n") {
						if ansi.StringWidth(line) > 32 {
							t.Fatalf("row exceeds width: %q", line)
						}
						if index := strings.Index(line, tt.prefix); index >= 0 {
							column = ansi.StringWidth(line[:index]) + ansi.StringWidth(strings.TrimSuffix(tt.prefix, "hello"))
						} else if index := strings.Index(line, "continuation"); index >= 0 {
							continuations++
							if got := ansi.StringWidth(line[:index]); got != column {
								t.Fatalf("continuation column = %d, want %d:\n%s", got, column, out)
							}
						}
					}
					if column < 0 || continuations == 0 {
						t.Fatalf("missing marker or continuation:\n%s", out)
					}
				})
			}
		}
	}
}
