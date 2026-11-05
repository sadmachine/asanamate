package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/sadmachine/asanamate/internal/whatsnew"
)

// runWhatsNew prints the release highlights up to version, newest first.
func runWhatsNew(args []string, out io.Writer, sections []whatsnew.Section, version string) error {
	if len(args) > 0 {
		return errors.New("usage: asanamate whats-new")
	}
	md := whatsnew.Markdown(whatsnew.All(sections, version))
	if md == "" {
		md = "No highlights for " + version + "."
	}
	_, err := fmt.Fprintln(out, md)
	return err
}
