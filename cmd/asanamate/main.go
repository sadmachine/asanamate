// Command asanamate is a terminal UI for Asana tickets with scriptable actions.
package main

import (
	"bufio"
	"cmp"
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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/listing"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/setup"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
	"github.com/sadmachine/asanamate/internal/timetracking"
	"github.com/sadmachine/asanamate/internal/tui"
	"github.com/sadmachine/asanamate/internal/writeback"
)

var version = "dev"

const usage = `usage:
  asanamate [--no-preview] [--exit-on-action]      open the TUI (--no-preview: list only;
                                                   --exit-on-action: quit before background actions)
  asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]
                                                   print tickets (tsv: gid, section, due, title, url)
  asanamate show [--format md|json] <gid>          print one ticket
  asanamate setup                                  create the config file
  asanamate setup hooks                            add the agent status hook to Codex
  asanamate config                                 edit the config file in $VISUAL or $EDITOR
  asanamate config update [--yes]                  refresh the config's comments and new defaults,
                                                   keeping your values (old file saved as .bak)
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate move    [--yes] [--project <gid>] <gid> <section>
  asanamate field   [--yes] [--project <gid>] <gid> <field> <value>
                                                   set a custom field ("" clears it)
  asanamate doctor [<gid>]                         show agents and why they link (or not) to a ticket
  asanamate time-provider hrvst --task-id <id>     serve time tracking provider requests on stdin
  asanamate hook codex                             record a Codex hook event read on stdin
                                                   (installed by setup)
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
	case "config":
		err = runConfig(args[1:])
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
	case "doctor":
		err = runDoctor(args[1:])
	case "hook":
		runHook(args[1:])
		return 0
	case "time-provider":
		if len(args) < 2 || args[1] != "hrvst" {
			err = fmt.Errorf("usage: asanamate time-provider hrvst --task-id <id>")
		} else {
			fs := flag.NewFlagSet("time-provider hrvst", flag.ContinueOnError)
			taskID := fs.String("task-id", "", "default Harvest task ID")
			if err = fs.Parse(args[2:]); err == nil {
				if len(fs.Args()) != 0 {
					err = fmt.Errorf("unexpected time-provider arguments: %v", fs.Args())
				} else {
					err = timetracking.RunHarvest(context.Background(), os.Stdin, os.Stdout, *taskID)
				}
			}
		}
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

// runHook records an agent hook event. It never fails: an agent runs it on
// every event, and a hook error must not get in the agent's way.
func runHook(args []string) {
	if len(args) != 1 || args[0] != "codex" {
		return
	}
	if stateDir, err := config.StateDir(); err == nil {
		_ = agents.WriteHook(os.Stdin, agents.HookDir(stateDir))
	}
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
	svc := writeback.Service{Client: client, Workspace: cfg.Workspace}
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

func runSetup(args []string) error {
	codexHome, err := setup.CodexHome()
	if err != nil {
		return err
	}
	if len(args) > 0 {
		if len(args) != 1 || args[0] != "hooks" {
			return fmt.Errorf("usage: asanamate setup [hooks]")
		}
		return setup.OfferCodexHook(setup.Options{
			In: bufio.NewReader(os.Stdin), Out: os.Stdout, CodexHome: codexHome, Executable: hookExecutable(),
		})
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
		CodexHome: codexHome, Executable: hookExecutable(),
	})
}

// hookExecutable is the asanamate path agent hooks run: the one on PATH,
// which survives upgrades, else this binary.
func hookExecutable() string {
	if path, err := exec.LookPath("asanamate"); err == nil {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
	}
	path, _ := os.Executable()
	return path
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
	// Like git: $VISUAL, then $EDITOR, then vi. The value may carry
	// arguments ("code --wait"), so the shell splits it.
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", editor, err)
	}
	_, err = config.Load(path)
	return err
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
	noPreview := fs.Bool("no-preview", false, "show only the ticket list")
	exitOnAction := fs.Bool("exit-on-action", false, "quit before running background actions, as exit mode does")
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
		Images:        kitty.Supported(cfg.Images.Mode, os.Getenv, kitty.TmuxPassthrough),
		InTmux:        os.Getenv("TMUX") != "",
		NoPreview:     *noPreview,
		ExitOnAction:  *exitOnAction,
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
	fmt.Fprintf(w, "config        %s\n", path)
	if cfg.BranchField == "" {
		fmt.Fprintln(w, "branch_field  unset: tickets use their ID field or title slug as the branch")
	} else {
		fmt.Fprintf(w, "branch_field  %q\n", cfg.BranchField)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var list []agents.Agent
	if !cfg.AgentsEnabled() {
		fmt.Fprintln(w, "agents        off: set agents.preset or agents.command")
	} else {
		if cfg.Agents.Preset != "" {
			fmt.Fprintf(w, "agents        preset %q\n", cfg.Agents.Preset)
		} else {
			fmt.Fprintf(w, "agents        command %q\n", cfg.Agents.Command)
		}
		if list, err = agents.Fetch(ctx, cfg.Agents.Preset, cfg.Agents.Command, cfg.Agents.States, stateDir); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n%d running:\n", len(list))
		for _, a := range list {
			fmt.Fprintf(w, "  %s  branch %s  %s  repo %s\n", a.Path, orNone(a.Branch), a.State, orNone(a.Repo))
		}
	}
	if len(args) == 0 {
		return nil
	}
	client, err := newClient()
	if err != nil {
		return err
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

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
