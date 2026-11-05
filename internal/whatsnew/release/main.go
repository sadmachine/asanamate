// Command release moves the waiting What's new highlights into the
// highlights file under a version heading and deletes their files. Run it
// from the repository root while preparing a release:
//
//	go run ./internal/whatsnew/release vX.Y.Z
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sadmachine/asanamate/internal/whatsnew"
)

const dir = "internal/whatsnew"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: go run ./%s/release vX.Y.Z", dir)
	}
	version, file := args[0], filepath.Join(dir, "whats-new.md")
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	frags, err := whatsnew.ReadFragments(os.DirFS(dir), whatsnew.UnreleasedDir)
	if err != nil {
		return err
	}
	out, err := whatsnew.Release(string(src), version, frags)
	if err != nil {
		return err
	}
	if len(frags) == 0 {
		fmt.Println("no unreleased highlights; nothing to do")
		return nil
	}
	if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
		return err
	}
	for _, f := range frags {
		if err := os.Remove(filepath.Join(dir, whatsnew.UnreleasedDir, f.Name)); err != nil {
			return err
		}
	}
	fmt.Printf("moved %d highlights files into %s under ## %s\n", len(frags), file, version)
	return nil
}
