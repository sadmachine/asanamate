package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sadmachine/asanamate/internal/addons"
	"github.com/sadmachine/asanamate/internal/config"
)

func runAddons(args []string, out io.Writer) error {
	check := len(args) > 0 && args[0] == "check"
	if check {
		args = args[1:]
	}
	if len(args) > 1 || check && len(args) != 1 {
		return errors.New("usage: asanamate addons [<id> | check <id>]")
	}
	var e addons.Entry
	if len(args) == 1 {
		var err error
		e, err = addons.Find(args[0])
		if err != nil {
			return err
		}
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	saved := addons.Load(path)
	if len(args) == 0 {
		fmt.Fprintf(out, "%s\nConfig: %s\n", saved.Status(), addons.Clean(path))
		for _, entry := range addons.Catalog() {
			r := addons.Inspect(entry, saved)
			fmt.Fprintf(out, "\n%s (%s) — %s\n  %s\n  %s\n", entry.Name, entry.ID, entry.Benefit, r.Summary(), r.Check)
		}
		fmt.Fprintln(out, "\nSetup and removal: asanamate addons <id>\nLocal checks: asanamate addons check <id>\nTUI: Settings > Addons")
		return nil
	}
	r := addons.Inspect(e, saved)
	if check {
		r = addons.Check(context.Background(), e, saved)
	}
	fmt.Fprint(out, addons.Detail(e, saved, r))
	if r.Failed {
		return errors.New("addon check failed; see checks and next step above")
	}
	return nil
}
