// Command asanamate is a terminal UI for Asana tickets with scriptable actions.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/writeback"
)

var version = "dev"

const usage = `usage:
  asanamate                                        open the TUI
  asanamate setup                                  create the config file
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate move    [--yes] [--project <gid>] <gid> <section>
  asanamate field   [--yes] <gid> <field> <value>  set a custom field ("" clears it)
  asanamate version
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	var err error
	switch name {
	case "comment", "move", "field":
		err = writeCommand(name, args[1:])
	case "version":
		fmt.Println(currentVersion())
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "asanamate:", err)
		return 1
	}
	return 0
}

func currentVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return version
}

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

func newClient() (*asana.Client, error) {
	token, err := config.Token()
	if err != nil {
		return nil, err
	}
	return asana.New(token), nil
}

func writeCommand(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	yes := fs.Bool("yes", false, "write without asking for confirmation")
	project := new(string)
	if name == "move" {
		project = fs.String("project", "", "project gid; required when the task is in several projects")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	want := map[string]int{"comment": 2, "move": 2, "field": 3}[name]
	if len(rest) != want {
		return fmt.Errorf("%s: wrong number of arguments\n%s", name, usage)
	}
	gid := rest[0]
	if !asana.ValidGID(gid) {
		return fmt.Errorf("task gid must be numeric, got %q", gid)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	svc := writeback.Service{Client: client}
	if writeback.NeedsConfirm(*yes, os.Getenv("ASANAMATE_CONFIRM_WRITES"), cfg.ConfirmWrites) {
		svc.Confirm = writeback.TTYConfirm
	}
	ctx := context.Background()
	switch name {
	case "comment":
		text := rest[1]
		if text == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = string(data)
		}
		return svc.Comment(ctx, gid, text)
	case "move":
		return svc.Move(ctx, gid, rest[1], *project)
	default:
		return svc.SetField(ctx, gid, rest[1], rest[2])
	}
}
