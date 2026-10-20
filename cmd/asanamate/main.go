// Command asanamate is a terminal UI for Asana tickets with scriptable actions.
package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/term"
	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/addons"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/browser"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/listing"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/setup"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/tui"
	"github.com/sadmachine/asanamate/internal/writeback"
)

var version = "dev"

const usage = `usage:
  asanamate [--hide-preview] [--exit-on-action]    open the TUI (--hide-preview: preview initially hidden;
                                                   --exit-on-action: quit before background actions)
  asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]
                                                   print tickets (default filter: list.default_view.filter;
                                                   tsv: gid, section, due, title, url)
  asanamate show [--format md|json] <gid>          print one ticket
  asanamate read [--exit-on-action] <gid>          open one ticket's reader, without the list
  asanamate setup                                  create the config file
  asanamate addons [<id>]                          list addons or show setup and removal instructions
  asanamate addons check <id>                      run documented local addon checks
  asanamate config                                 edit the config file in $VISUAL or $EDITOR
  asanamate config update [--yes]                  refresh the config's comments and new defaults,
                                                   keeping your values (old file saved as .<time>.bak)
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate title [--yes] <gid> <title>           replace a task's single-line title
  asanamate description [--yes] <gid> <markdown | ->
                                                   replace a description ("-" reads stdin; "" clears it)
  asanamate batch   [--yes] <gid> <changes-json | ->
                                                   validate and submit several task edits
  asanamate edit-comment [--yes] <comment-gid> <text | ->
                                                   replace a comment's text ("-" reads stdin)
  asanamate move    [--yes] [--project <gid> | --my-tasks] <gid> <section>
  asanamate field   [--yes] [--project <gid>] <gid> <field> <value>
                                                   set a custom field ("a, b" for multi-select
                                                   and people; "" clears it)
  asanamate assign  [--yes] <gid> <name | me | "">
                                                   assign a task ("" unassigns it)
  asanamate due     [--yes] <gid> <date | "">
                                                   set a task's due date: YYYY-MM-DD or date -d
                                                   text, such as "+1 day" ("" clears it)
  asanamate project add|remove [--yes] <gid> <project-gid>
                                                   add a task to a project or remove it
  asanamate complete [--yes] [--reopen] <gid>        mark a task complete (--reopen reopens it)
  asanamate react   [--yes] [--remove] <comment-gid> <emoji>
                                                   react to a comment (--remove takes it back;
                                                   comment gids are in show --format json)
  asanamate create  [--yes] [--project <gid> [--section <name>] | --parent <gid>]
                    [--assignee <name | me>] [--due <date>] [--notes <markdown | ->] <title>
                                                   create a ticket (default: in My Tasks, assigned
                                                   to you) or a subtask; prints its gid
  asanamate conversations [--json] <gid | url>    list saved Claude Code and Codex conversations linked
                                                   to a task (needs [conversations]; no Asana token)
  asanamate conversation open <gid | url> --provider <name> --session <id>
                                                   resume one exact linked conversation in this terminal
  asanamate doctor [<gid>]                         check setup and optional tools; for a ticket, why agents link (or not)
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
		err = runSetup(args[1:])
	case "addons":
		err = runAddons(args[1:], os.Stdout)
	case "config":
		err = runConfig(args[1:])
	case "comment", "edit-comment", "move", "field", "batch":
		err = writeCommand(name, args[1:])
	case "assign", "due", "project", "title", "description":
		err = writeCommand(name, args[1:])
	case "complete", "react":
		err = writeCommand(name, args[1:])
	case "create":
		err = runCreate(args[1:], os.Stdin, os.Stdout, newWriter)
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
	case "read":
		err = runRead(args[1:])
	case "doctor":
		err = runDoctor(args[1:])
	case "conversations":
		err = runConversations(args[1:], os.Stdout)
	case "conversation":
		err = runConversation(args[1:])
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

// newWriter returns the CLI's writeback service, which confirms on the
// terminal unless yes, ASANAMATE_CONFIRM_WRITES, or the config turns it off.
func newWriter(yes bool) (writeback.Service, error) {
	cfg, err := loadConfig()
	if err != nil {
		return writeback.Service{}, err
	}
	client, err := newClient()
	if err != nil {
		return writeback.Service{}, err
	}
	svc := writeback.Service{Client: client, Workspace: cfg.Workspace}
	if writeback.NeedsConfirm(yes, os.Getenv("ASANAMATE_CONFIRM_WRITES"), cfg.ConfirmWrites) {
		svc.Confirm = writeback.TTYConfirm
	}
	return svc, nil
}

func writeCommand(name string, args []string) error {
	return runWrite(name, args, os.Stdin, newWriter)
}

func runWrite(name string, args []string, stdin io.Reader, writer func(bool) (writeback.Service, error)) error {
	if name == "project" {
		if len(args) == 0 || (args[0] != "add" && args[0] != "remove") {
			return fmt.Errorf("project: want add or remove\n%s", usage)
		}
		name, args = "project "+args[0], args[1:]
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	yes := fs.Bool("yes", false, "write without asking for confirmation")
	project := new(string)
	if name == "move" || name == "field" {
		project = fs.String("project", os.Getenv("ASANAMATE_PROJECT_GID"), "project gid; picks the project (move) or the project's field (field) when ambiguous; defaults to $ASANAMATE_PROJECT_GID")
	}
	myTasks := new(bool)
	if name == "move" {
		myTasks = fs.Bool("my-tasks", false, "move within your My Tasks instead of a project")
	}
	reopen := new(bool)
	if name == "complete" {
		reopen = fs.Bool("reopen", false, "reopen a completed task instead")
	}
	remove := new(bool)
	if name == "react" {
		remove = fs.Bool("remove", false, "remove your reaction instead")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	want := map[string]int{"comment": 2, "edit-comment": 2, "title": 2, "description": 2, "batch": 2, "move": 2, "field": 3, "assign": 2, "due": 2, "project add": 2, "project remove": 2, "react": 2}[name]
	if name == "complete" {
		want = 1
	}
	if len(rest) != want {
		return fmt.Errorf("%s: wrong number of arguments\n%s", name, usage)
	}
	gid, noun := rest[0], "task"
	if name == "react" || name == "edit-comment" {
		noun = "comment"
	}
	if !asana.ValidGID(gid) {
		return fmt.Errorf("%s gid must be numeric, got %q", noun, gid)
	}
	if strings.HasPrefix(name, "project ") {
		project = &rest[1]
	}
	if *project != "" && !asana.ValidGID(*project) {
		return fmt.Errorf("project gid must be numeric, got %q", *project)
	}
	if *myTasks {
		both := false
		fs.Visit(func(f *flag.Flag) { both = both || f.Name == "project" })
		if both {
			return errors.New("move: pass --project or --my-tasks, not both")
		}
		*project = writeback.MyTasks
	}
	svc, err := writer(*yes)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch name {
	case "batch":
		var input io.Reader = strings.NewReader(rest[1])
		if rest[1] == "-" {
			input = stdin
		}
		decoder := json.NewDecoder(input)
		decoder.DisallowUnknownFields()
		var changes []writeback.Change
		if err := decoder.Decode(&changes); err != nil {
			return fmt.Errorf("batch changes: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return errors.New("batch changes: expected one JSON array")
		}
		for _, change := range changes {
			if change.Project != "" && change.Project != writeback.MyTasks && !asana.ValidGID(change.Project) {
				return fmt.Errorf("project gid must be numeric, got %q", change.Project)
			}
			if (change.Kind == "project_add" || change.Kind == "project_remove") && !asana.ValidGID(change.Target) {
				return fmt.Errorf("project gid must be numeric, got %q", change.Target)
			}
		}
		_, err := svc.Batch(ctx, gid, changes)
		return err
	case "title":
		return svc.SetTitle(ctx, gid, rest[1])
	case "comment", "edit-comment", "description":
		text := rest[1]
		if text == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return err
			}
			text = string(data)
		}
		if name == "edit-comment" {
			return svc.EditComment(ctx, gid, text)
		}
		if name == "description" {
			return svc.SetDescription(ctx, gid, text)
		}
		return svc.Comment(ctx, gid, text)
	case "move":
		return svc.Move(ctx, gid, rest[1], *project)
	case "assign":
		return svc.Assign(ctx, gid, rest[1])
	case "due":
		return svc.SetDue(ctx, gid, rest[1])
	case "project add":
		return svc.AddProject(ctx, gid, *project)
	case "project remove":
		return svc.RemoveProject(ctx, gid, *project)
	case "complete":
		return svc.Complete(ctx, gid, !*reopen)
	case "react":
		return svc.React(ctx, gid, rest[1], !*remove)
	default:
		return svc.SetField(ctx, gid, rest[1], rest[2], *project)
	}
}

// runCreate creates a ticket and prints only its gid, so scripts can chain it.
func runCreate(args []string, stdin io.Reader, stdout io.Writer, writer func(yes bool) (writeback.Service, error)) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "write without asking for confirmation")
	var nt writeback.NewTicket
	fs.StringVar(&nt.Project, "project", "", "project gid; without it or --parent, the ticket goes to your My Tasks")
	fs.StringVar(&nt.Section, "section", "", "section of --project, by name or gid")
	fs.StringVar(&nt.Parent, "parent", "", "parent task gid; makes a subtask")
	fs.StringVar(&nt.Assignee, "assignee", "", `full name or gid of the assignee, or "me"`)
	fs.StringVar(&nt.Due, "due", "", "due date as YYYY-MM-DD or date -d text, such as +1 day")
	fs.StringVar(&nt.Notes, "notes", "", `description in Markdown ("-" reads stdin)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("create: wrong number of arguments\n%s", usage)
	}
	nt.Name = fs.Arg(0)
	for flagName, gid := range map[string]string{"project": nt.Project, "parent": nt.Parent} {
		if gid != "" && !asana.ValidGID(gid) {
			return fmt.Errorf("%s gid must be numeric, got %q", flagName, gid)
		}
	}
	if nt.Notes == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		nt.Notes = string(data)
	}
	svc, err := writer(*yes)
	if err != nil {
		return err
	}
	t, err := svc.Create(context.Background(), nt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, t.GID)
	return err
}

func runSetup(args []string) error {
	if len(args) == 1 && args[0] == "hooks" {
		return errors.New("asanamate setup hooks moved to `asanamate-ccmux setup`")
	}
	if len(args) > 0 {
		return fmt.Errorf("usage: asanamate setup")
	}
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

// runConfig opens the config file in the user's editor, then checks that it
// still loads.
func runConfig(args []string) error {
	if len(args) > 0 && args[0] == "update" {
		return runConfigUpdate(args[1:])
	}
	if len(args) > 0 {
		return fmt.Errorf("config takes no arguments\n%s", usage)
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w (run asanamate setup first)", err)
	}
	// The value may carry arguments ("code --wait"), so the shell splits it.
	editor := editorCommand()
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", editor, err)
	}
	_, err = config.Load(path)
	return err
}

// editorCommand is the editor `asanamate config` runs, like git: $VISUAL,
// then $EDITOR, then vi.
func editorCommand() string {
	return cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
}

func runConfigUpdate(args []string) error {
	fs := flag.NewFlagSet("config update", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "rewrite without asking for confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	return setup.Update(setup.Options{In: bufio.NewReader(os.Stdin), Out: os.Stdout, ConfigPath: path}, *yes)
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("asanamate", flag.ContinueOnError)
	var hidePreview bool
	fs.BoolVar(&hidePreview, "hide-preview", false, "start with the preview hidden (z toggles it)")
	exitOnAction := fs.Bool("exit-on-action", false, "quit before running background actions, as exit mode does")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
	return startTUI(tui.Deps{NoPreview: hidePreview, ExitOnAction: *exitOnAction})
}

// runRead opens one ticket's reader full screen, without the list.
func runRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	exitOnAction := fs.Bool("exit-on-action", false, "quit before running background actions, as exit mode does")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("read needs exactly one task gid\n%s", usage)
	}
	gid := fs.Arg(0)
	if !asana.ValidGID(gid) {
		return fmt.Errorf("task gid must be numeric, got %q", gid)
	}
	return startTUI(tui.Deps{Ticket: gid, ExitOnAction: *exitOnAction})
}

// startTUI fills in d's config, state, and services, runs the TUI, then runs
// the command an exit-mode action left.
func startTUI(d tui.Deps) error {
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := loadOrMigrate(configPath, os.Stdin, os.Stdout, term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()))
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
	cacheDir, err := config.CacheDir()
	if err != nil {
		return err
	}
	if st.LegacyDisplay != (state.LegacyDisplay{}) {
		cfg, err = migrateDisplay(configPath, st)
		if err != nil {
			return err
		}
	}
	d.ConfigPath, d.Config, d.State, d.Client = configPath, cfg, st, client
	d.StateDir, d.CacheDir = stateDir, cacheDir
	d.Images = kitty.Supported(cfg.Images.Mode, os.Getenv, kitty.TmuxPassthrough)
	d.InTmux = os.Getenv("TMUX") != ""
	d.Symbols = cfg.SymbolSet(os.Getenv)
	d.ReducedMotion = reducedMotion(cfg)
	d.LightBackground = !darkBackground(cfg)
	m := tui.New(d)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	cmd := m.ExitCommand()
	if cmd == nil {
		return nil
	}
	return action.Exec(cmd)
}

// darkBackground asks the terminal for its background, only when the theme
// follows it.
func darkBackground(cfg config.Config) bool {
	if cmp.Or(cfg.Theme.Name, config.ThemeAuto) != config.ThemeAuto {
		return true
	}
	return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
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
	query := fs.String("filter", "", "filter query (default: list.default_view.filter from the config)")
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
		*query = cfg.List.DefaultView.Filter
	}
	tasks, err := ticket.List(context.Background(), client, cfg.Workspace, *project)
	if err != nil {
		return err
	}
	return listing.Tasks(os.Stdout, *format, filter.Parse(*query).Apply(tasks, nil, time.Now()), *project)
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

// errDoctorBlocking is doctor's error when a check browsing needs fails.
var errDoctorBlocking = errors.New("doctor found problems that block browsing (marked error)")

// runDoctor reports whether asanamate is ready to browse and which optional
// features can work, then, for a ticket, why agents link to it or not. Only
// core failures (config, token, workspace) make it fail. It runs no actions
// and writes nothing.
func runDoctor(args []string) error {
	if len(args) > 1 || (len(args) == 1 && !asana.ValidGID(args[0])) {
		return fmt.Errorf("doctor takes at most one numeric task gid\n%s", usage)
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
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
	w := os.Stdout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	row(w, "config", "ok: "+path)
	settingsPath := config.SettingsPath(path)
	if _, err := os.Stat(settingsPath); errors.Is(err, os.ErrNotExist) {
		row(w, "settings", "defaults (no file)")
	} else if err != nil {
		return err
	} else {
		row(w, "settings", "ok: "+settingsPath)
	}
	row(w, "actions", fmt.Sprintf("ok: %d in %s", len(cfg.Actions), config.ActionsDir(path)))
	row(w, "state", "ok: "+stateDir)
	client := checkAsana(ctx, w, cfg.Workspace)
	checkFeatures(w, cfg)
	checkAddons(ctx, w, path)
	if cfg.BranchField == "" {
		row(w, "branch_field", "unset: tickets use their ID field or title slug as the branch")
	} else {
		row(w, "branch_field", strconv.Quote(cfg.BranchField))
	}
	checkFeeds(ctx, w, cfg.Feed.Commands)
	var list []agents.Agent
	if !cfg.AgentsEnabled() {
		row(w, "agents", "off: set agents.command")
	} else {
		source := "command " + strconv.Quote(cfg.Agents.Command)
		if args, known := addons.KnownCommand("agents.command", cfg.Agents.Command); known {
			localCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			list, err = agents.FetchArgs(localCtx, args, cfg.Agents.States)
			cancel()
		} else {
			list, err = agents.Fetch(ctx, cfg.Agents.Command, cfg.Agents.States)
		}
		if err != nil {
			row(w, "agents", fmt.Sprintf("warning: %s failed: %v", source, err))
		} else {
			row(w, "agents", "ok: "+source)
		}
		if err == nil {
			fmt.Fprintf(w, "\n%d running:\n", len(list))
			for _, a := range list {
				fmt.Fprintf(w, "  %s  branch %s  %s  repo %s\n", a.Path, orNone(a.Branch), a.State, orNone(a.Repo))
			}
		}
	}
	gid := ""
	if len(args) == 1 {
		gid = args[0]
	}
	checkConversations(ctx, w, cfg, gid)
	if client == nil {
		return errDoctorBlocking
	}
	if len(args) == 0 {
		return nil
	}
	t, err := ticket.Fetch(ctx, client, args[0])
	if err != nil {
		return err
	}
	branch := action.Branch(t.Task, st.TaskBranches[t.GID], cfg.BranchField, nil)
	repos := st.LinkedRepos(t.Task)
	fmt.Fprintf(w, "\nticket  %s\nbranch  %s\n", ticket.OneLine(t.Name), branch)
	if warn := action.BranchWarning(t.Task, st.TaskBranches[t.GID], cfg.BranchField, nil); warn != "" {
		fmt.Fprintf(w, "warning %s\n", warn)
	}
	if len(repos) == 0 {
		fmt.Fprintln(w, "repos   none linked: agents in any repo can match (run a repo = true action to link one)")
	} else {
		fmt.Fprintf(w, "repos   %s\n", strings.Join(repos, ", "))
		for _, r := range repos {
			fmt.Fprintf(w, "  %s  worktree %s\n", r, orNone(repo.Worktree(r, branch)))
		}
	}
	if !cfg.AgentsEnabled() {
		return nil
	}
	linked := agents.MatchAll(list, branch, repos)
	fmt.Fprintf(w, "linked  %d agent(s)\n", len(linked))
	for _, a := range linked {
		fmt.Fprintf(w, "  %s  %s\n", a.Path, a.State)
	}
	if len(linked) == 0 {
		for _, a := range agents.Unlinked(list, branch, repos) {
			fmt.Fprintf(w, "  %s\n", agents.Hint(a, branch))
		}
	}
	return nil
}

// checkAsana reports whether the token works for the configured workspace,
// the checks browsing needs, and returns the client when they pass.
func checkAsana(ctx context.Context, w io.Writer, workspace string) *asana.Client {
	client, err := newClient()
	if err != nil {
		row(w, "asana", "error: "+err.Error())
		return nil
	}
	me, err := client.Me(ctx)
	if err != nil {
		row(w, "asana", fmt.Sprintf("error: %s is set, but Asana rejected it or is unreachable: %v", config.TokenEnv, err))
		return nil
	}
	for _, ws := range me.Workspaces {
		if ws.GID == workspace {
			row(w, "asana", fmt.Sprintf("ok: %s in workspace %s", ticket.Clean(me.Name), ticket.Clean(ws.Name)))
			return client
		}
	}
	row(w, "asana", fmt.Sprintf("error: %s cannot access workspace %s; run asanamate setup", ticket.Clean(me.Name), workspace))
	return nil
}

// checkFeatures reports the tools optional features need. Missing ones only
// warn: browsing works without them. Custom commands are not run.
func checkFeatures(w io.Writer, cfg config.Config) {
	tool := func(label, name, need string) {
		if p, err := exec.LookPath(name); err == nil {
			row(w, label, "ok: "+p)
		} else {
			row(w, label, fmt.Sprintf("warning: %s not found; %s", name, need))
		}
	}
	tool("git", "git", "repo actions and agent branch matching need it; install it from https://git-scm.com/downloads")
	switch {
	case cfg.RepoSource.Command != "":
		row(w, "repo_source", "unchecked: command "+strconv.Quote(cfg.RepoSource.Command)+" (not run)")
	case cfg.RepoSource.Root != "":
		if paths, err := repo.List(cfg.RepoSource.Root); err != nil {
			row(w, "repo_source", fmt.Sprintf("warning: %v (%d repos found)", err, len(paths)))
		} else {
			row(w, "repo_source", fmt.Sprintf("ok: %d repos in %s", len(paths), cfg.RepoSource.Root))
		}
	default:
		row(w, "repo_source", "off: type a repo path when an action asks; set repo_source.root to pick from a directory")
	}
	if slices.ContainsFunc(cfg.Actions, func(a config.Action) bool { return strings.Contains(a.Command, "PAGER") }) {
		tool("pager", setup.Pager(), "pager actions need it; install less or set $PAGER")
	}
	editor := strings.Fields(editorCommand())
	if len(editor) > 0 {
		tool("editor", editor[0], "asanamate config needs it; set $EDITOR or edit the config file directly")
	}
	tool("browser", browser.Opener(), "opening tickets in a browser needs it; on Linux, install xdg-utils and a desktop browser")
	if cfg.TimeTracking.Command == "" {
		row(w, "time_tracking", "off")
	} else if _, known := addons.KnownCommand("time_tracking.command", cfg.TimeTracking.Command); known {
		row(w, "time_tracking", "configured: see addon local check below; connection unverified")
	} else {
		row(w, "time_tracking", "unchecked: command "+strconv.Quote(cfg.TimeTracking.Command)+" (not run)")
	}
	if cfg.ProjectLinks.Command == "" {
		row(w, "project_links", "off")
	} else if _, known := addons.KnownCommand("project_links.command", cfg.ProjectLinks.Command); known {
		row(w, "project_links", "configured: see addon local check below; connection unverified")
	} else {
		row(w, "project_links", "unchecked: command "+strconv.Quote(cfg.ProjectLinks.Command)+" (not run)")
	}
}

// checkAddons reuses only cataloged local checks for recognized commands.
// Existing custom feed and agent checks below retain their behavior.
func checkAddons(ctx context.Context, w io.Writer, path string) {
	saved := addons.Load(path)
	for _, e := range addons.Catalog() {
		r := addons.Inspect(e, saved)
		if !r.Configured() {
			continue
		}
		r = addons.Check(ctx, e, saved)
		prefix := ""
		if r.Failed {
			prefix = "warning: "
		}
		row(w, "addon "+e.ID, prefix+r.Summary()+"; "+r.Check)
	}
}

// checkFeeds runs each feed command once and reports what it answered.
func checkFeeds(ctx context.Context, w io.Writer, feeds []string) {
	if len(feeds) == 0 {
		row(w, "feeds", "none: set feed.commands")
	}
	for _, command := range feeds {
		if _, known := addons.KnownCommand("feed.commands", command); known {
			row(w, "feed", "local readiness checked under addon; connection unverified (asanamate addons)")
			continue
		}
		source := "command " + strconv.Quote(command)
		reply, err := feed.Fetch(ctx, command, time.Now())
		if err != nil {
			row(w, "feed", fmt.Sprintf("warning: %s failed: %v", source, err))
			continue
		}
		items := fmt.Sprintf("%d items", len(reply.Items))
		if len(reply.Items) == 1 {
			items = "1 item"
		}
		row(w, "feed", fmt.Sprintf("ok: %s: %s, %s today%s", source, reply.Source, items, strings.TrimSuffix(", "+reply.Summary, ", ")))
	}
}

// row prints one doctor line: a label column, then its result.
func row(w io.Writer, label, result string) {
	fmt.Fprintf(w, "%-14s%s\n", label, result)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
