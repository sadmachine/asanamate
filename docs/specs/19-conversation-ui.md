# 19: Ticket conversation picker, CLI, and opening

## Goal and dependencies

Expose native ticket conversation history in asanamate, backed by the
generic source/open contract in [spec 16](16-conversation-history.md).
Use [spec 17](17-conversation-companion.md) for real Claude/Codex sessions.
The TUI and CLI share lookup/validation; neither understands provider
transcript formats or installs provider hooks.

Depends on spec 16 and spec 17's supported adapter contract/package. Test
the integration with [spec 18's plugins](18-conversation-plugins.md), but hooks are not required
to browse manually linked conversations.

Asanamate is one consumer of the standalone `asana-sessions` package. Use
`asana-sessions asanamate list` and `asana-sessions asanamate open` for the
source/open boundary. Core neither embeds the package nor requires its
version to match asanamate's. Document and test a supported package/protocol
range; the standalone package can be released before this UI is implemented.

## TUI entry and lookup

> **Revised:** Conversations moved from the action menu to the agents modal
> (`w`, or `enter` on the reader's Agents section). It lists running agents,
> then saved conversations. A saved record whose exact `provider` and
> `session_id` match a running agent's shows once, as the agent, and picking it
> runs `agents.focus`. That agent-source identity is the authoritative
> native-ID mapping required under Opening. The lookup still starts when the
> modal opens, with the running agents shown while it loads.

Add a built-in **Conversations** entry to the selected ticket's existing
action menu when `[conversations]` is enabled. It is available even when
there are no user-defined action files. Keep existing action bindings and
repeat-action behavior intact; selecting Conversations does not overwrite
the last configured action. No new global shortcut or key namespace in v1.

Start a nonblocking lookup for the selected task GID. Show a loading state
and allow cancellation. Capture that GID with the request; if the user
changes ticket, closes the picker, or starts another lookup, cancel/ignore
the old result. Do not block TUI rendering on SDK reads or RPC startup.

Lookup happens on opening Conversations and explicit refresh, not on every
ticket render or the running-agent poll. Reuse a last-successful in-memory
result only as a visibly stale result while refreshing; do not persist an
index. Whole-source failure must not replace a previous successful result
with a misleading empty list.

## Picker

Use existing picker navigation and search. Show one row per exact identity:

- Human title (canonical suffix may be hidden in the display because the
  ticket is already known).
- Provider and last activity time, or an explicit unknown time.
- Recorded directory/branch when available.
- Archived state and known activity (`active`, `inactive`, `unknown`).

Sort newest known `updated_at` first, unknown times last, then provider and
session ID for stable ties. Keep otherwise-identical titles as separate
rows. Never truncate the actual native ID or selected snapshot based on
display width. Sanitize control characters and preserve navigation in narrow
terminals and list-only mode.

Partial results carry visible provider diagnostics. “No linked conversations”
means a complete successful lookup with zero rows. Unavailable/incomplete
lookup, stale results, unsupported versions, and metadata conflicts have
distinct messages and recovery guidance. A source that is off adds no UI
entry or startup failure.

Offer explicit Refresh and Cancel rows using existing picker behavior;
Refresh keeps the selected identity when it remains present. Do not add a
new keyboard catalog solely for this picker. A single result still opens
the picker: its title, archive state, and activity are useful before launch.

## Opening

Selection writes spec 16's private snapshot and launches the configured
`conversations.open` through foreground action execution. Reuse the actual
Bubble Tea terminal handoff used by existing foreground actions. Avoid a
parallel subprocess/terminal-control implementation.

The thin asanamate adapter reads the selection snapshot and calls the same
engine as standalone `asana-sessions open`. The engine owns provider-specific
revalidation, cwd recovery, archived restoration, and live-session checks
from spec 17. The UI labels the action
**Resume in terminal**, not **Focus agent**. Generic history cannot reliably
map a saved thread to a window, tmux pane, or Paseo agent.

The default opener refuses a known active session and explains how to return
to its owning host. Unknown activity follows spec 17: require an explicit
choice only where safe native exclusion exists; the approved conservative
Claude revision refuses unknown activity until safe resumption is established.
A custom `conversations.open` may focus a host only when that
integration has an authoritative native-ID mapping. Source records never
carry shell commands or trusted deep links to execute.

Exiting or cancelling restores the TUI, selected ticket, view, scroll,
layout, and terminal state. Report helper/provider errors without losing
the current ticket. Refresh history the next time the user requests it;
do not immediately rescan every provider when the CLI closes.

## CLI

Add:

```text
asanamate conversations <gid-or-url> [--json]
asanamate conversation open <gid-or-url> --provider <provider> --session <id>
```

`conversations` uses the same task URL/GID resolution and generic source as
the TUI. Human output includes provider, full session ID, title, last
activity, archive state, and diagnostics. `--json` emits the versioned
response shape from spec 16, including partial diagnostics. A complete
zero-row result exits zero; partial/unavailable results exit nonzero while
still preserving valid JSON results for scripts. Informational absence of
an unused provider does not make a healthy single-provider lookup partial.

`conversation open` requires both explicit identity fields; it never opens
the first/newest match implicitly. Perform a fresh lookup, require the exact
identity linked to the requested task, write a private snapshot, and run
the configured opener with terminal streams. Pass existing ticket exports
when available; resolving a linked chat requires no Asana API read. A
custom opener must handle absent optional ticket exports in this CLI path.
Do not prompt for an Asana token simply to use a validated task URL/GID.

Noninteractive open fails clearly when the chosen opener requires a terminal,
activity confirmation, or replacement cwd. CLI and TUI opening preserve the
same validation and safety rules. No `--yes` Asana-write flag applies:
renaming/resuming local conversations is not an Asana write.

Commands must remain available without a working Asana token. Refactor
startup dispatch only as needed to keep conversation discovery independent
of Asana client validation; retain normal config validation and existing
commands' behavior.

## Diagnostics and documentation

Extend `asanamate doctor` with conversation-source status only when enabled.
Use the generic lookup/validation path; do not hard-code Claude or Codex
version checks in core. The companion's own `doctor` owns provider/plugin
details. Use `asana-sessions doctor` for standalone diagnostics; it does not
inspect asanamate config/state. Missing source/opener commands are warnings
about this optional feature, with actionable guidance; they do not make unrelated TUI work
unusable. Preserve existing exit-status conventions for optional tools.

Update:

- Main command usage and help.
- README feature list, configuration table, Conversations section, CLI,
  manual-title convention, and troubleshooting.
- `docs/extensions.md` with discovery and foreground-open contracts.
- Default config template and config-update fixtures.
- Companion/plugin installation and compatibility instructions.

Keep standalone CLI, JSON schema, provider behavior, and package releases
documented in the package-owned README/changelog. Core docs explain the
adapter configuration and UI and link to that package documentation rather
than duplicating its full CLI or implying it is asanamate-only.

Explain that history is local, archived conversations are included, resume
does not restore Git, Node is an addon dependency, native IDs differ from
Paseo/ccmux IDs, and hooks can be disabled independently of lookup.

## Implementation seams

Put generic requests/records/validation in `internal/conversations`, using
`internal/jsoncmd`. Add conversation selection and snapshot context to
`internal/action` without leaking provider SDK types into core. Extend
`internal/tui` using existing picker/loading/foreground-launch patterns.
Update `cmd/asanamate/main.go` dispatch, config, and doctor through their
existing owners. Do not introduce an action-file `conversation = true` key,
new action-builder flow, polling service, or history database for this slice.

## Acceptance

- A fixture source and foreground opener drive the UI and CLI without real
  provider installations or an Asana network request.
- The integration works with an independently packed/installed
  `asana-sessions` version in the supported range. Standalone package
  acceptance does not depend on core UI or core config being present.
- Configured Conversations is available without user action files; existing
  actions, bindings, repeat behavior, running-agent display, and source-off
  behavior stay intact.
- Ticket changes, cancellation, refresh, duplicate titles, archives, unknown
  fields, narrow terminals, empty history, and partial/error states behave
  as specified. Late responses never attach to another ticket.
- Selected-record snapshots retain exact identity and GID, use private
  unique files, and cannot inject shell text or inherit stale selection
  variables from an outer action.
- Real terminal handoff restores selection and terminal state after exit,
  cancellation, interruption, provider failure, and missing cwd recovery.
- CLI opens only the requested identity linked to the requested ticket;
  lookup works without an Asana token and scripts receive structured errors.
- Complete manual proof on both supported provider versions: start outside
  asanamate, submit one task link with the plugin, close, select that ticket
  in asanamate, find the saved chat, resume the same native ID, exit, and
  return to the ticket. Repeat with hooks off and a manually named chat,
  an archived chat, a deleted worktree, and a known active session.
- Ask before writing test files. Run focused implementation checks followed
  by `go test ./...` and `go vet ./...`; all required CI platform/archive,
  adapter-compatibility, and smoke checks pass before a core release is
  tagged. Standalone package release gates and versions are owned by spec 18.
