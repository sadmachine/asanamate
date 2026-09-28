// Command asanamate is a terminal UI for Asana tickets with scriptable actions.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/listing"
	"github.com/sadmachine/asanamate/internal/setup"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/tui"
	"github.com/sadmachine/asanamate/internal/writeback"
)

var version = "dev"

const usage = `usage:
  asanamate [--no-preview]                         open the TUI (--no-preview: list only)
  asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]
                                                   print tickets (tsv: gid, section, due, title, url)
  asanamate show [--format md|json] <gid>          print one ticket
  asanamate setup                                  create the config file
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate move    [--yes] [--project <gid>] <gid> <section>
  asanamate field   [--yes] [--project <gid>] <gid> <field> <value>
                                                   set a custom field ("" clears it)
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
	case "setup":
		err = runSetup()
	case "comment", "move", "field":
		err = writeCommand(name, args[1:])
	case "version":
		fmt.Println(currentVersion())
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	case "list":
		err = runList(args[1:])
	case "show":
		err = runShow(args[1:])
	default:
		if name != "" && !strings.HasPrefix(name, "-") {
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		err = runTUI(args)
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
	if name == "move" || name == "field" {
		project = fs.String("project", os.Getenv("ASANAMATE_PROJECT_GID"), "project gid; picks the project (move) or the project's field (field) when ambiguous; defaults to $ASANAMATE_PROJECT_GID")
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
	if *project != "" && !asana.ValidGID(*project) {
		return fmt.Errorf("project gid must be numeric, got %q", *project)
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
		return svc.SetField(ctx, gid, rest[1], rest[2], *project)
	}
}

func runSetup() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	return setup.Run(context.Background(), setup.Options{
		In: bufio.NewReader(os.Stdin), Out: os.Stdout, Client: client,
		ConfigPath: configPath, StatePath: filepath.Join(stateDir, state.FileName),
	})
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("asanamate", flag.ContinueOnError)
	noPreview := fs.Bool("no-preview", false, "show only the ticket list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	st, err := state.Load(filepath.Join(stateDir, state.FileName))
	if err != nil {
		return err
	}
	m := tui.New(tui.Deps{
		Config:        cfg,
		State:         st,
		Client:        client,
		StateDir:      stateDir,
		Images:        kitty.Supported(cfg.Images, os.Getenv, kitty.TmuxPassthrough),
		InTmux:        os.Getenv("TMUX") != "",
		NoPreview:     *noPreview,
		Symbols:       cfg.SymbolSet(os.Getenv),
		ReducedMotion: reducedMotion(cfg),
	})
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	cmd := m.ExitCommand()
	if cmd == nil {
		return nil
	}
	return action.Exec(cmd)
}

// reducedMotion honors the config, else the OS accessibility setting
// (macOS Reduce Motion, GNOME animations).
func reducedMotion(cfg config.Config) bool {
	if cfg.ReducedMotion != nil {
		return *cfg.ReducedMotion
	}
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("defaults", "read", "com.apple.universalaccess", "reduceMotion").Output()
		return err == nil && strings.TrimSpace(string(out)) == "1"
	case "linux":
		out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "enable-animations").Output()
		return err == nil && strings.TrimSpace(string(out)) == "false"
	}
	return false
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	project := fs.String("project", "", "project gid (default: My Tasks)")
	query := fs.String("filter", "", "filter query (default: default_filter from the config)")
	format := fs.String("format", listing.FormatTSV, "tsv or jsonl")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
	if *project != "" && !asana.ValidGID(*project) {
		return fmt.Errorf("project gid must be numeric, got %q", *project)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	filterSet := false
	fs.Visit(func(f *flag.Flag) { filterSet = filterSet || f.Name == "filter" })
	if !filterSet {
		*query = cfg.DefaultFilter
	}
	tasks, err := ticket.List(context.Background(), client, cfg.Workspace, *project)
	if err != nil {
		return err
	}
	return listing.Tasks(os.Stdout, *format, filter.Parse(*query).Apply(tasks, nil), *project)
}

func runShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	format := fs.String("format", listing.FormatMarkdown, "md or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("show needs exactly one task gid\n%s", usage)
	}
	gid := fs.Arg(0)
	if !asana.ValidGID(gid) {
		return fmt.Errorf("task gid must be numeric, got %q", gid)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	t, err := ticket.Fetch(context.Background(), client, gid)
	if err != nil {
		return err
	}
	return listing.Ticket(os.Stdout, *format, t)
}
