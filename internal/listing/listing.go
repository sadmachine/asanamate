// Package listing formats tickets for scripts and tools such as fzf.
package listing

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Output formats.
const (
	FormatTSV      = "tsv"
	FormatJSONL    = "jsonl"
	FormatMarkdown = "md"
	FormatJSON     = "json"
)

// Tasks writes one line per task. TSV columns are gid, section, due, title,
// url; section is the My Tasks section, or the section in projectGID.
func Tasks(w io.Writer, format string, tasks []asana.Task, projectGID string) error {
	switch format {
	case FormatTSV:
		for _, t := range tasks {
			due := ""
			if t.DueOn != nil {
				due = *t.DueOn
			}
			cols := []string{t.GID, t.SectionFor(projectGID), due, t.Name, t.PermalinkURL}
			for i, c := range cols {
				cols[i] = ticket.OneLine(c)
			}
			if _, err := fmt.Fprintln(w, strings.Join(cols, "\t")); err != nil {
				return err
			}
		}
		return nil
	case FormatJSONL:
		enc := json.NewEncoder(w)
		for _, t := range tasks {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown list format %q; use %s or %s", format, FormatTSV, FormatJSONL)
}

// Ticket writes one ticket as Markdown (same as ticket.md) or indented JSON.
func Ticket(w io.Writer, format string, t ticket.Ticket) error {
	switch format {
	case FormatMarkdown:
		_, err := io.WriteString(w, t.Markdown())
		return err
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(t)
	}
	return fmt.Errorf("unknown show format %q; use %s or %s", format, FormatMarkdown, FormatJSON)
}
