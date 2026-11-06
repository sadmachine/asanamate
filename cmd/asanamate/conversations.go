package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/conversations"
	"github.com/sadmachine/asanamate/internal/display"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// errIncompleteLookup fails a lookup whose usable rows are already printed.
var errIncompleteLookup = errors.New("conversation lookup is incomplete; see the diagnostics")

// taskArg resolves a task GID or an app.asana.com task URL.
func taskArg(arg string) (string, error) {
	if asana.ValidGID(arg) {
		return arg, nil
	}
	if gid := asana.TaskGIDFromURL(arg); gid != "" {
		return gid, nil
	}
	return "", fmt.Errorf("want a task gid or an app.asana.com task URL, got %q", arg)
}

// parseInterspersed parses flags given before or after positional arguments
// and returns the positional ones.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// conversationConfig loads the config for conversation commands, which never
// need an Asana token: a validated task GID is enough to find linked chats.
func conversationConfig() (config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cfg, err
	}
	if !cfg.ConversationsEnabled() {
		return cfg, errors.New("conversations are off: set conversations.command and conversations.open (see the README's Conversations section)")
	}
	return cfg, nil
}

func lookupConversations(ctx context.Context, gid string) (conversations.Result, error) {
	cfg, err := conversationConfig()
	if err != nil {
		return conversations.Result{}, err
	}
	result, err := conversations.Source{Command: cfg.Conversations.Command}.List(ctx, gid)
	conversations.Sort(result.Conversations)
	return result, err
}

// runConversations prints a task's linked conversations. Only a complete
// lookup exits zero; partial and failed lookups still print what they found,
// and --json always prints the versioned response, with a source diagnostic
// on failure.
func runConversations(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("conversations", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the versioned JSON response")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("conversations needs exactly one task gid or URL\n%s", usage)
	}
	gid, err := taskArg(rest[0])
	if err != nil {
		return err
	}
	result, err := lookupConversations(context.Background(), gid)
	if err != nil {
		result = conversations.Result{
			Version: conversations.ProtocolVersion, Conversations: []conversations.Record{},
			Diagnostics: []conversations.Diagnostic{conversations.Failure(err)},
		}
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return err
		}
	} else {
		printConversations(stdout, result, err == nil)
	}
	switch {
	case err != nil:
		return err
	case !result.Complete():
		return errIncompleteLookup
	}
	return nil
}

// printConversations writes one row per exact identity, then diagnostics.
// Session IDs stay exact for `conversation open`; ones with spaces or
// characters unsafe to display are quoted.
func printConversations(w io.Writer, r conversations.Result, ok bool) {
	if ok && len(r.Conversations) == 0 && r.Complete() {
		fmt.Fprintln(w, "No linked conversations.")
	}
	if len(r.Conversations) > 0 {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PROVIDER\tSESSION\tUPDATED\tSTATE\tTITLE")
		for _, c := range r.Conversations {
			id := c.SessionID
			if display.Line(id) != id || strings.ContainsFunc(id, unicode.IsSpace) {
				id = strconv.Quote(id)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", display.Line(c.Provider), id,
				c.UpdatedLabel(), c.StateLabel(), display.Line(c.Title))
		}
		tw.Flush()
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintln(w, diagnosticLine(d))
	}
}

// diagnosticLine is one diagnostic with its recovery hint when it degrades
// the lookup.
func diagnosticLine(d conversations.Diagnostic) string {
	line := fmt.Sprintf("%s: %s: %s: %s", d.Severity, display.Line(d.Provider), d.Code, d.Message)
	if hint := conversations.Guidance(d.Code); hint != "" && d.Severity != "info" {
		line += " (" + hint + ")"
	}
	return line
}

// runConversation opens one exact linked conversation in this terminal. It
// never chooses a match: both identity fields are required, and a fresh lookup
// must still link that identity to the task.
func runConversation(args []string) error {
	if len(args) == 0 || args[0] != "open" {
		return fmt.Errorf("conversation: want open\n%s", usage)
	}
	fs := flag.NewFlagSet("conversation open", flag.ContinueOnError)
	provider := fs.String("provider", "", "the saved conversation's provider, such as claude or codex")
	session := fs.String("session", "", "the exact native session ID, as conversations prints it")
	rest, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return err
	}
	if len(rest) != 1 || *provider == "" || *session == "" {
		return fmt.Errorf("conversation open needs a task gid or URL, --provider, and --session\n%s", usage)
	}
	gid, err := taskArg(rest[0])
	if err != nil {
		return err
	}
	cfg, err := conversationConfig()
	if err != nil {
		return err
	}
	result, err := conversations.Source{Command: cfg.Conversations.Command}.List(context.Background(), gid)
	if err != nil {
		return err
	}
	selected, ok := result.Find(*provider, *session)
	if !ok {
		msg := fmt.Sprintf("no %s conversation %q is linked to task %s", *provider, *session, gid)
		if !result.Complete() {
			msg += "; the lookup was incomplete (run asanamate conversations " + gid + ")"
		}
		return errors.New(msg)
	}
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	cmd, err := action.ConversationCommand(stateDir, cfg.Conversations.Open,
		action.Context{Ticket: ticket.Ticket{Task: asana.Task{GID: gid}}, BranchField: cfg.BranchField}, selected)
	if err != nil {
		return err
	}
	return action.Exec(cmd)
}

// checkConversations reports the optional conversation source, only when it
// is on. Missing commands warn; with a ticket, the generic lookup runs.
func checkConversations(ctx context.Context, w io.Writer, cfg config.Config, gid string) {
	if !cfg.ConversationsEnabled() {
		return
	}
	found := true
	for _, c := range [][2]string{{"conversations.command", cfg.Conversations.Command}, {"conversations.open", cfg.Conversations.Open}} {
		name := strings.Fields(c[1])[0]
		if _, err := exec.LookPath(name); err != nil {
			found = false
			row(w, "conversations", fmt.Sprintf("warning: %s: %s not found; install the conversation source, such as asana-sessions, or fix %s", c[0], name, c[0]))
		}
	}
	if gid == "" {
		if found {
			row(w, "conversations", "ok: commands found; asanamate doctor <gid> looks up a ticket's conversations")
		}
		return
	}
	result, err := conversations.Source{Command: cfg.Conversations.Command}.List(ctx, gid)
	if err != nil {
		row(w, "conversations", "warning: "+diagnosticLine(conversations.Failure(err)))
		return
	}
	status := "ok"
	if !result.Complete() {
		status = "warning: incomplete lookup"
	}
	row(w, "conversations", fmt.Sprintf("%s: %d linked to %s", status, len(result.Conversations), gid))
	for _, d := range result.Diagnostics {
		fmt.Fprintln(w, "  "+diagnosticLine(d))
	}
}
