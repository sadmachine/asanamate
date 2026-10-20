package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/extensions"
)

func runExtensions(args []string, out io.Writer) error {
	check := len(args) > 0 && args[0] == "check"
	if check {
		args = args[1:]
	}
	if len(args) > 1 || check && len(args) != 1 {
		return errors.New("usage: asanamate extensions [<id> | check <id>]")
	}
	var e extensions.Entry
	if len(args) == 1 {
		var err error
		e, err = extensions.Find(args[0])
		if err != nil {
			return err
		}
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	saved := extensions.Load(path)
	saved.Version = currentVersion()
	if len(args) == 0 {
		fmt.Fprintf(out, "%s\nConfig: %s\n", saved.Status(), extensions.Clean(path))
		for _, entry := range extensions.Catalog() {
			r := extensions.Inspect(entry, saved)
			fmt.Fprintf(out, "\n%s (%s) — %s\n  %s\n  %s\n", entry.Name, entry.ID, entry.Benefit, r.Summary(), r.Check)
		}
		fmt.Fprintln(out, "\nSetup and removal: asanamate extensions <id>\nLocal checks: asanamate extensions check <id>\nTUI: Settings > Extensions")
		return nil
	}
	r := extensions.Inspect(e, saved)
	if check {
		r = extensions.Check(context.Background(), e, saved)
	}
	fmt.Fprint(out, extensions.Detail(e, saved, r))
	if r.Failed {
		return errors.New("extension check failed; see checks and next step above")
	}
	return nil
}
