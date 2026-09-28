// Package action runs configured commands against a ticket.
//
// Ticket data reaches commands only through ASANAMATE_* environment variables
// and files, never through the command string, so ticket text cannot inject
// shell syntax.
package action

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const envPrefix = "ASANAMATE_"

// Files are the ticket exports passed to an action.
type Files struct {
	JSON     string
	Markdown string
}

// Context is everything an action run knows about.
type Context struct {
	Ticket        ticket.Ticket
	Project       *asana.Ref
	Repo          string
	ConfirmWrites bool
	Files         Files
	// Preferred holds the gids of the active project's custom fields; it
	// decides between fields that share a name.
	Preferred map[string]bool
	// BranchField names the custom field holding the ticket's git branch.
	BranchField string
	// Agent is the running agent the action is about, if any.
	Agent *agents.Agent
	// Agents are all running agents linked to the ticket, most urgent first.
	Agents []agents.Agent
}

// Branch returns the ticket's git branch: the value of branchField when set,
// otherwise the title slug.
func Branch(t asana.Task, branchField string, preferred map[string]bool) string {
	if v := fieldBranch(t, branchField, preferred); v != "" {
		return v
	}
	return Slug(t.Name)
}

// BranchWarning explains a Branch that fell back to the title slug even though
// branchField is set, or returns "" when there is nothing to warn about.
func BranchWarning(t asana.Task, branchField string, preferred map[string]bool) string {
	if branchField == "" || fieldBranch(t, branchField, preferred) != "" {
		return ""
	}
	return fmt.Sprintf("%q is empty, so the branch is the title slug %q", branchField, Slug(t.Name))
}

func fieldBranch(t asana.Task, branchField string, preferred map[string]bool) string {
	if branchField == "" {
		return ""
	}
	f, _ := t.Field(branchField, preferred)
	return f.Value()
}

// WriteFiles exports the ticket to <stateDir>/tickets/<gid>/ticket.{json,md}.
// The paths are stable so detached actions can read them later.
func WriteFiles(stateDir string, t ticket.Ticket) (Files, error) {
	if !asana.ValidGID(t.GID) {
		return Files{}, fmt.Errorf("invalid task gid %q", t.GID)
	}
	dir := filepath.Join(stateDir, "tickets", t.GID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Files{}, err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return Files{}, err
	}
	f := Files{JSON: filepath.Join(dir, "ticket.json"), Markdown: filepath.Join(dir, "ticket.md")}
	if err := os.WriteFile(f.JSON, data, 0o600); err != nil {
		return Files{}, err
	}
	if err := os.WriteFile(f.Markdown, []byte(t.Markdown()), 0o600); err != nil {
		return Files{}, err
	}
	return f, nil
}

// Env returns the ASANAMATE_* variables for an action run, sorted.
func Env(c Context) []string {
	t := c.Ticket
	vars := map[string]string{
		"GID":            t.GID,
		"TITLE":          t.Name,
		"URL":            t.PermalinkURL,
		"SLUG":           Slug(t.Name),
		"COMPLETED":      strconv.FormatBool(t.Completed),
		"REPO":           c.Repo,
		"TICKET_JSON":    c.Files.JSON,
		"TICKET_MD":      c.Files.Markdown,
		"CONFIRM_WRITES": "0",
		"ASSIGNEE":       "",
		"DUE":            "",
		"MY_SECTION":     "",
		"PROJECT":        "",
		"PROJECT_GID":    "",
		"SECTION":        "",
		"BRANCH":         Branch(t.Task, c.BranchField, c.Preferred),
		"AGENT_STATUS":   "",
		"AGENT_STATE":    "",
		"AGENT_TITLE":    "",
		"AGENT_TARGETS":  agentTargets(c.Agents),
		"AGENT_PATH":     "",
		"AGENT_TARGET":   "",
	}
	if c.Agent != nil {
		vars["AGENT_STATUS"], vars["AGENT_PATH"], vars["AGENT_TARGET"] = c.Agent.Status, c.Agent.Path, c.Agent.Target
		vars["AGENT_STATE"], vars["AGENT_TITLE"] = string(c.Agent.State), c.Agent.Title
	}
	if c.ConfirmWrites {
		vars["CONFIRM_WRITES"] = "1"
	}
	if t.Assignee != nil {
		vars["ASSIGNEE"] = t.Assignee.Name
	}
	if t.DueOn != nil {
		vars["DUE"] = *t.DueOn
	}
	if t.AssigneeSection != nil {
		vars["MY_SECTION"] = t.AssigneeSection.Name
	}
	if c.Project != nil {
		vars["PROJECT"] = c.Project.Name
		vars["PROJECT_GID"] = c.Project.GID
		vars["SECTION"] = t.SectionIn(c.Project.GID)
	}
	tags := make([]string, len(t.Tags))
	for i, tag := range t.Tags {
		tags[i] = tag.Name
	}
	vars["TAGS"] = strings.Join(tags, ",")
	byName := map[string][]asana.CustomField{}
	for _, f := range t.CustomFields {
		if name := envName(f.Name); name != "" {
			byName[name] = append(byName[name], f)
		}
	}
	for name, fields := range byName {
		vars["FIELD_"+name] = asana.PickField(fields, c.Preferred).Value()
	}
	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, envPrefix+k+"="+ticket.Clean(v))
	}
	sort.Strings(env)
	return env
}

// agentTargets lists agents one per line as "target\tstate\tpath\ttitle".
func agentTargets(list []agents.Agent) string {
	lines := make([]string, len(list))
	for i, a := range list {
		lines[i] = a.Target + "\t" + string(a.State) + "\t" + a.Path + "\t" + a.Title
	}
	return strings.Join(lines, "\n")
}

// Slug turns a title into a lowercase, dash-separated string of at most 50 characters.
func Slug(s string) string {
	return separated(strings.ToLower(s), '-', 50)
}

func envName(s string) string {
	return strings.ToUpper(separated(strings.ToLower(s), '_', 0))
}

// separated keeps [a-z0-9], collapses every other run into sep, and trims sep from both ends.
func separated(s string, sep byte, limit int) string {
	var b strings.Builder
	pending := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pending && b.Len() > 0 {
				b.WriteByte(sep)
			}
			b.WriteRune(r)
			pending = false
		} else {
			pending = true
		}
	}
	out := b.String()
	if limit > 0 && len(out) > limit {
		out = strings.TrimRight(out[:limit], string(sep))
	}
	return out
}

// DefaultProject is the active project for actions that do not resolve a repo:
// the viewed project if the ticket is in it, else the ticket's only project, else nil.
func DefaultProject(t asana.Task, viewProjectGID string) *asana.Ref {
	for _, m := range t.Memberships {
		if m.Project.GID == viewProjectGID {
			p := m.Project
			return &p
		}
	}
	if len(t.Memberships) == 1 {
		p := t.Memberships[0].Project
		return &p
	}
	return nil
}

// Command builds the /bin/sh invocation for an action. Inherited ASANAMATE_*
// variables are dropped so values from an outer run cannot leak in.
func Command(a config.Action, c Context) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", a.Command)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, envPrefix) {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, Env(c)...)
	if c.Repo != "" {
		cmd.Dir = c.Repo
	}
	return cmd
}

// RunBackground runs cmd in a new session with output appended to logPath,
// and waits for it to exit. The new session has no controlling terminal, so a
// confirmation prompt fails fast instead of blocking, and the process survives
// asanamate quitting.
func RunBackground(cmd *exec.Cmd, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	fmt.Fprintf(log, "--- %s %s\n", time.Now().Format(time.RFC3339), cmd.Args[len(cmd.Args)-1])
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Run()
}

// Exec replaces the current process with cmd, so the command owns the
// terminal, receives signals directly, and its exit status becomes ours.
func Exec(cmd *exec.Cmd) error {
	if cmd.Dir != "" {
		if err := os.Chdir(cmd.Dir); err != nil {
			return err
		}
	}
	return syscall.Exec(cmd.Path, cmd.Args, cmd.Env)
}
