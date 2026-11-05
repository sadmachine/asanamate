# 16: Ticket conversation history and extension contract

## Goal

Select an Asana ticket, find its previous Claude Code and Codex conversations,
and resume one even when neither asanamate nor ccmux launched it. Optional
provider plugins automate linking when a task URL appears in a user prompt.
The provider's saved conversation metadata is the source of truth.

The generic core contract is implemented in `internal/conversations` and
`internal/action`, with configuration and fixture tests. The standalone tool,
provider integrations, and ticket UI are specs 17–19.
Details requiring a provider spike are explicitly gated in spec 17;
documentation alone is not proof that an operation works on an actively
running conversation.

## Delivery order

| Task | Owns | Depends on |
|---|---|---|
| 16, this file | Generic conversation source, configuration, and open contract | none |
| [17-conversation-companion](17-conversation-companion.md) | Standalone `asana-sessions` CLI, native discovery, linking, and resumption | 16's adapter contract only; run its feasibility gate first |
| [18-conversation-plugins](18-conversation-plugins.md) | Optional hooks, explicit-link skill, installation, and packaging | 17 |
| [19-conversation-ui](19-conversation-ui.md) | Ticket picker, foreground handoff, CLI, and end-to-end proof | 16, 17 |

The standalone package and plugins (17–18) can ship before the core
integration (16 and 19). Each product must pass its own acceptance and release
checks; the complete integrated feature must pass all four lists. Hook
automation can be disabled independently of historical discovery and reopening.
Run spec 17's feasibility gate before implementing this contract; revise
the proposed contract if the released provider APIs require it.

The [2026-10-16 live observations](17-provider-feasibility.md) found no native
writer exclusion for Claude Code `2.1.301`. The user approved proceeding with
the generic core contract under a conservative provider-opener revision:
Claude discovery/linking remain supported, but the default Claude opener
refuses unknown activity until safe resumption is established. This is not
a claim that the companion's full feasibility or release gate has passed.

## Terms and boundaries

- A **conversation** is a provider's persisted chat, identified by its native
  session/thread ID. A conversation can exist without a running process.
- An **agent** is a running session exposed by the existing agent source.
  Agent state and conversation history remain different extension points.
- A **ticket link** is one exact task GID stored in conversation metadata.
  Multiple conversations may link to one ticket. The first release supports
  one ticket per conversation.
- A **provider plugin** is a Claude Code or Codex plugin containing hooks
  and a skill. It is not a dynamically loaded Go plugin.
- **asana-sessions** is the standalone optional package and command. It
  understands provider APIs, metadata, and resume behavior. It is also
  referred to as the companion when describing its role beside asanamate.
- **Core** understands generic conversation records, exact GID matching,
  selection, errors, and terminal handoff. Provider code stays outside the
  main Go binary, following `docs/extensions.md` and spec 07.

Discovery is local to the machine, account, and provider homes visible to the
configured source. No transcript synchronization or remote-host registry is
implied. Provider session IDs are opaque; they are not Asana GIDs.

## Standalone product boundary

Keep the package in this repository at `packages/asana-sessions/` while it
is built out. Give it its own manifest, lockfile, README, tests, version,
and release process. It must install and work without asanamate, its
configuration, or its state directory. A future repository move should
require distribution changes, not an engine rewrite.

The public CLI serves terminal users and scripts: list sessions by an Asana
task URL/GID, link or unlink an exact session, and resume it. Human output
and JSON output are first-class interfaces. No Asana API credentials are
required to extract a GID or search local session metadata.

Only a thin `asana-sessions asanamate` adapter implements this document's
source/open protocol. Keep `ASANAMATE_*` variables, ticket snapshots, and
protocol translation inside that adapter. The engine and public commands
must not depend on them. Other applications use the public CLI/JSON output
without adopting asanamate's protocol. Do not generalize beyond Asana,
Claude Code, and Codex in this slice.

## User outcomes

1. An installed plugin sees one task URL in a submitted user prompt and
   appends a stable ticket marker to the native conversation title.
2. Later, the user selects that ticket in asanamate and opens Conversations.
   Both providers' matching histories appear, including archived records.
3. Selecting a conversation opens the explicit terminal-resume flow. Exiting
   the provider restores the TUI and its ticket selection.
4. A conversation manually named with the same marker is discoverable without
   hooks or any previous asanamate state.
5. Missing optional tools, inaccessible history, and deleted worktrees have
   visible recovery paths. They never become a false “no conversations.”

## Canonical ticket metadata

The portable representation is a trailing title marker:

```text
Fix task sorting [asana:1234567890]
```

The marker is case-sensitive and contains an ASCII decimal GID accepted by
`asana.ValidGID`. Match the complete suffix, not a substring or a title that
merely mentions a number. Session IDs, project GIDs, and branch names never
substitute for a task GID.

The companion may also recognize a Claude native tag exactly equal to
`asana:<GID>`. Automatic linking writes the title only: Claude has one tag
slot, which another integration may already use. If a recognized tag and
title marker disagree, report a metadata conflict and exclude the record
from automatic ticket matching until explicitly repaired.

One authoritative definition of this parsing and normalization belongs in
the companion. Core validates returned GIDs and matches them to the selected
ticket; it does not parse Claude or Codex titles. Branch and repository data
are display context, not implicit ticket links in the first release.

## Existing seams

- `internal/jsoncmd`: JSON requests to configured commands.
- `internal/action`: private per-run snapshots, environment sanitization,
  foreground commands, and terminal/process handoff.
- `internal/tui/picker.go`, `flow.go`: asynchronous selection and actions.
- `internal/config/config.go`, `internal/setup/setup.go`: configuration,
  defaults, validation, and generated template.
- `cmd/asanamate-ccmux` and `cmd/asanamate-hrvst`: optional external commands.

Extend these seams rather than adding a plugin manager, daemon, or provider
SDK dependency to core. Do not force persisted conversations into
`agents.command`; its running-state and branch-linking semantics differ.
(Since then, agent lines gained optional `task_gid`, `provider`, and
`session_id` columns, and `asana-sessions asanamate agents --` decorates a
running-agent source with them. That links running agents, not persisted
conversations; see `docs/extensions.md#agent-sources`.)

## Configuration

Add an optional table with two settings:

```toml
[conversations]
command = "asana-sessions asanamate list"
open = "asana-sessions asanamate open"
```

Both default to empty, so the feature is off. Require both to be nonempty
when either is set; errors name `conversations.command` or
`conversations.open`. The growing concept owns a sub-table. There is no
provider selector, poll interval, terminal selector, or new global key in v1.

`command` performs noninteractive discovery. `open` is trusted user-authored
shell text that performs a foreground open using a selected-record snapshot.
A user can replace `open` with an external host launcher without changing
core. Neither command is assembled from ticket or provider text.

Follow every config-key requirement in `AGENTS.md`, including config tests,
the template, README table, and `config update` behavior. Older config files
keep working without adding this table. Existing agent configuration stays
unchanged.

## Discovery protocol, version 1

Run `conversations.command` through the existing JSON-command mechanism.
Pass one request on stdin, then EOF:

```json
{
  "version": 1,
  "operation": "list",
  "asana": {"task_gid": "1234567890"}
}
```

Return exactly one JSON object on stdout:

```json
{
  "version": 1,
  "conversations": [
    {
      "provider": "codex",
      "session_id": "native-session-id",
      "task_gid": "1234567890",
      "title": "Fix task sorting [asana:1234567890]",
      "cwd": "/home/me/code/project",
      "branch": "fix-task-sorting",
      "updated_at": "2026-10-15T16:00:00Z",
      "archived": false,
      "activity": "unknown"
    }
  ],
  "diagnostics": [
    {"provider": "claude", "code": "unavailable", "severity": "info", "message": "History is not installed"}
  ]
}
```

Required record fields: `provider`, `session_id`, `task_gid`, `title`,
`activity`. `cwd`, `branch`, and `updated_at` may be absent when unknown;
`archived` defaults to false. `activity` is `active`, `inactive`, or
`unknown`; the last is the default when a provider cannot prove activity.
Do not infer inactivity from an old timestamp, idle agent, absent ccmux row,
or a new app-server process that has not loaded the thread.

Identity is `(provider, session_id)` within the configured local source.
The first release reads one active home per provider, honoring environment
overrides; multiple machines or simultaneous account/home namespaces need a
future protocol extension. Titles and paths are display data, not identity.

Protocol rules:

- Validate version, operation, and task GID before launching providers.
  Ignore unknown optional fields; reject unsupported protocol versions.
- Core checks record types, GID validity and equality with the request,
  activity values, timestamps when present, and duplicate identities.
  Invalid records fail validation rather than silently opening another chat.
- A supplied `cwd` is absolute. Its existence is checked at open time; a
  missing directory does not make a historical record invalid.
- Collapse exact duplicate records. Conflicting duplicates are errors.
- No matches is `conversations: []` with a successful complete lookup.
  Diagnostics remain visible, so partial lookup is distinguishable from
  complete lookup. Whole-command failures use a nonzero exit status.
- Missing provider installations produce per-provider diagnostics; failure
  of one installed provider does not discard the other's records. Both
  unavailable is an unavailable lookup, not an empty successful history.
- A provider with no history installation is informational when the other
  provider is available. A healthy single-provider installation is complete,
  not permanently degraded. An installed provider's unreadable or failed
  lookup makes the result partial. An existing empty history is successful.
  When no provider is available, the adapter exits nonzero with recovery
  information on stderr. Core cannot infer availability from an empty array
  and informational absence of one provider.
- Diagnostics have bounded, display-safe messages and stable codes:
  `unavailable`, `unsupported_version`, `permission_denied`,
  `metadata_conflict`, `incomplete`, and `provider_error`. Their required
  `severity` is `info`, `warning`, or `error`; warning/error diagnostics mean
  incomplete lookup, while info alone does not.
- Reuse `jsoncmd` for transport, but extend its responsible runner with
  bounded capture: it currently buffers stdout and stderr without limits.
  This protocol permits 4 MiB stdout and 64 KiB stderr, with a 10-second
  lookup deadline. Termination must clean up the helper and its discovery
  subprocesses. Overflow and timeout report incomplete lookup, never a
  successful truncated result. Preserve existing callers' contracts when
  extending the runner. No on-disk core cache in v1.
- Do not log prompts or transcript contents. Only metadata needed for
  listing leaves the provider adapter. Sanitize terminal controls before
  displaying titles, paths, branches, or diagnostics.

Document the final request, response, limits, and failure rules in
`docs/extensions.md`, with a fixture-command example and protocol tests.

## Foreground open contract

Discovery stdin is a JSON pipe. An interactive resumed CLI needs terminal
stdin. Keep those transports separate instead of feeding JSON and terminal
input through one stream.

Before running `conversations.open`, write `conversation.json` beside the
existing ticket exports in a unique action run directory. Reuse action
snapshot permissions: directory `0700`, file `0600`. Export:

- `ASANAMATE_CONVERSATION_JSON`: absolute selected-record snapshot path.
- `ASANAMATE_CONVERSATION_PROVIDER`: selected provider.
- `ASANAMATE_CONVERSATION_ID`: exact native session ID.

The snapshot contains `version: 1`, `operation: "open"`, `asana.task_gid`,
and one `conversation` object using the discovery record shape. Existing
ticket exports remain available in the TUI; spec 19's CLI open reads no
ticket from Asana, so its ticket exports are empty. Clear these variables for unrelated action
runs and remove inherited `ASANAMATE_*` values as `action.Command` does now.

Execute the configured open command through the existing foreground action
mechanism, with terminal stdin/stdout/stderr. It reads the snapshot path
from the environment, not stdin. Use the current asanamate directory for
the helper itself; the provider adapter chooses the resumed provider's cwd.
Do not apply the ticket's repo/branch action steps to a saved conversation.

The opener must revalidate the selected identity and its ticket link against
native history before resuming. Never execute commands supplied in source
records. Session IDs, paths, and GIDs become argument-array values or file
data, never interpolated shell text. Exit zero means the open operation
completed normally, including a resumed CLI that exited normally; nonzero
means failure. Core restores terminal state and reports failure either way.

For the default Claude opener, unknown activity is refused until safe native
exclusion is established, per the approved revision above. Confirmation alone
does not establish safe resumption of an independently launched conversation.

For the supplied opener, the asanamate adapter reads this snapshot and calls
the same engine operation as the standalone CLI. The engine receives a
validated selection object; it never reads an asanamate environment variable
or imports core Go code. Custom openers may implement this contract directly.

## Scope

Included: both providers, exact ticket links, archived history, manual
linking, optional automatic hooks, a generic source/open boundary, native
ticket selection, terminal resumption, installation/removal, and diagnostics.

Excluded: transcript search or preview, automatic historical backfill from
message text, branch-based guesses, multi-ticket links, cloud/remote sync,
automatic checkout/worktree creation, a general plugin registry, disk
indexing, and built-in Paseo/ccmux window control. A custom opener can use a
host API, but that host's agent ID must be mapped explicitly to the native
session ID; branch/title equality is insufficient.

## Acceptance

Core fixture/configuration checks belong to this task. The separately installed
package condition below remains an integration gate for spec 17; the native
ticket picker and CLI are owned by spec 19. The approved Claude restriction
applies to that companion integration.

- A fixture source can drive the complete generic protocol without Claude,
  Codex, Node, ccmux, or an Asana network request.
- Validation covers malformed records, mismatched GIDs, conflicts, partial
  diagnostics, cancellation, and bounded output.
- A fixture opener receives the private snapshot and terminal input; no
  selected metadata is substituted into shell text.
- Default installation behaves exactly as before with the feature off.
- A separately installed `asana-sessions` package provides its public CLI
  without asanamate config/state. Its adapter produces the generic contract
  from the same lookup and open operations.
- Config and extension documentation describe the same contract.
- Run focused package checks, then required Go checks for implementation.
  Obtain permission before writing test files, as workspace rules require.

## Evidence and release gate

Documented on 2026-10-15:

- [Claude session operations](https://code.claude.com/docs/en/agent-sdk/sessions)
  and [session browser guide](https://platform.claude.com/cookbook/claude-agent-sdk-05-building-a-session-browser):
  local listing, title/tag mutation, and reading without a model request.
- [Codex app-server](https://learn.chatgpt.com/docs/app-server):
  `thread/list`, `thread/read`, `thread/name/set`, and `thread/resume`.
- [Claude plugin manifests](https://code.claude.com/docs/en/plugins-reference)
  and [Codex hooks](https://learn.chatgpt.com/docs/hooks): plugin-bundled
  lifecycle hooks and prompt/session identity input.

Spec 17 must record tested versions, active-session behavior, archive
behavior, and runtime dependencies before implementation commits to a
provider API. These specs do not claim universal GUI title synchronization.
