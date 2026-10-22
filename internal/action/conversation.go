package action

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/conversations"
)

// ConversationCommand writes the private selection snapshot and returns a
// command for conversation focus or the foreground handoff. The snapshot joins the run
// directory of c.Files when the caller exported the ticket (WriteFiles), else
// gets a new run directory: the CLI resolves a linked chat without reading
// Asana, so its ticket exports are absent. The caller chooses the execution
// mode and streams, as with Command. The helper runs in asanamate's cwd,
// without ticket repo/branch steps. Focus callers revalidate through discovery;
// native activity checks, cwd recovery, and resumption belong to the opener.
func ConversationCommand(stateDir, command string, c Context, selected conversations.Record) (*exec.Cmd, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("conversations.open is not configured")
	}
	if err := selected.Validate(c.Ticket.GID); err != nil {
		return nil, err
	}
	dir := filepath.Dir(c.Files.JSON)
	if c.Files.JSON == "" {
		var err error
		if dir, err = runDir(stateDir, c.Ticket.GID); err != nil {
			return nil, err
		}
	}
	snapshot, err := filepath.Abs(filepath.Join(dir, "conversation.json"))
	if err != nil {
		return nil, err
	}
	request := conversations.OpenRequest{
		Version: conversations.ProtocolVersion, Operation: "open",
		Asana: conversations.Asana{TaskGID: c.Ticket.GID}, Conversation: selected,
	}
	data, err := json.MarshalIndent(request, "", "  ")
	if err == nil {
		err = os.WriteFile(snapshot, data, 0o600)
	}
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	c.Files.Conversation, c.Conversation = snapshot, &selected
	c.Repo, c.Worktree = "", ""
	return Command(config.Action{Command: command, Mode: config.ModeForeground}, c), nil
}
