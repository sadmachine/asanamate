# Extensions

For installation and onboarding, use `asanamate extensions` or **Settings > Extensions**.
See [Extensions](../README.md#extensions) for the shared catalog. This page remains the
protocol contract; custom commands need no catalog registration.

asanamate is extended by external commands: any program or shell snippet you
name in the config. This page is the contract between asanamate and those
commands, one section per extension point:

| Extension point | Config | Command gets | Command returns |
|---|---|---|---|
| [Actions](#actions) | `actions/*.toml` | `ASANAMATE_*` variables and ticket files | exit status |
| [Agent sources](#agent-sources) | `agents.command`, `agents.states` | nothing | one tab-separated line per agent |
| [Conversation history](#conversation-history) | `conversations.command`, `conversations.open` | JSON discovery request; private snapshot for foreground open | versioned metadata and diagnostics; opener exit status |
| [Repo sources](#repo-sources) | `repo_source.command` | nothing | one repo path per line |
| [Time tracking](#time-tracking) | `time_tracking.command` | one JSON request on stdin | one JSON reply |
| [Project links](#project-links) | `project_links.command` | one JSON request on stdin | one JSON reply |
| [Feeds](#feeds) | `feed.commands` | one JSON request on stdin | one JSON reply |

Each format is covered by a test that runs a small fixture script through the
real code path (`internal/*/testdata/`), so a format change cannot slip in
unnoticed. Changes are called out in the release notes.

## Implementing an extension

An extension implements an asanamate contract, independently of the external
service or tool it uses. A provider can implement one contract or several.
Bundled commands use the same contracts as custom commands; no registration,
special executable prefix, or core code change is required.

1. Choose the capability in the table above and read its full contract.
   [Conversation discovery](#discovery) and [foreground opening](#foreground-opening)
   are separate interfaces; persisted conversations are not running agents.
2. Start from that contract's fixture example. JSON protocols declare their
   version and operation, while actions, agent sources, and repo sources are
   unversioned. Do not confuse an asanamate protocol version with a provider
   release or remote API version. Time tracking requires `form` and `log`;
   `summary` is optional, and a summary's daily total is optional too.
3. Parse structured input rather than matching raw text. Treat task text,
   record IDs, URLs, and paths as untrusted data. Pass them through argument
   arrays or files, never interpolated shell syntax. Own your credentials
   and remote API client; do not copy another provider's credential format.
4. Keep JSON/line-protocol stdout free of prompts and logs. Use sanitized
   stderr for diagnostics. Follow the contract's lifecycle, timeout, error,
   partial-result, and compatibility rules. Noninteractive requests cannot
   run a login flow. Conversation open instead inherits the terminal and
   reads its selected-record snapshot from the environment.
5. Validate against a fixture or fake server first. Repository examples under
   `internal/*/testdata/` and their protocol tests demonstrate real core
   transport paths without live credentials. A fixture's no-op `log` branch
   is not a working time logger: a production provider exits zero only once
   its entry exists. Do not automatically retry a possibly completed write.
6. Put the command on PATH, or configure a quoted absolute path, and edit
   only the relevant config slot. Restart asanamate after editing. Inspect
   forms/lookup and error handling before making any explicit real write or
   opening a native conversation. Discovery must not rename/link sessions,
   resume a provider, start a model turn, or grant plugin trust.

### Replacing a time provider

For example, you could write `asanamate-clockify` against the
[Time tracking contract](#time-tracking). This is a hypothetical adapter,
not a shipped or advertised Clockify integration. Configure it as:

```toml
[time_tracking]
id = "clockify"
command = "asanamate-clockify"
```

Use a new provider ID so remembered choices remain separate from Harvest.
Keep the old ID's saved choices and credentials if you want to revert.
Your adapter needs its own Clockify authentication and must implement the
asanamate request/reply contract; a Clockify API client alone is not enough.
It need not implement Harvest flags, task defaults, or credential files.

Replacing time tracking leaves `feed.commands` unchanged. Only add your
command as a feed if it also implements [Feeds](#feeds), replacing just the
intended entry and preserving other sources. Tracker-specific project links
such as `harvest_project` are not translated into another tracker's IDs.
You may explicitly return `{"unsupported":true}` for optional time totals.
Open the time form and cancel to verify configuration without logging time.

The same substitution rule applies to conversation sources/openers. Implement
their documented interfaces and configure both `conversations.command` and
`conversations.open`. A custom implementation need not use the standalone
`asana-sessions` CLI, its SDKs, or its optional native linking plugins.

## Rules for every command

- **Shell.** asanamate runs the configured string with `/bin/sh -c`, so it can
  be a script path, a pipeline, or a one-liner. Quote paths with spaces.
- **Data never enters the command string.** asanamate passes ticket and
  provider data only through environment variables, files, and stdin, so
  ticket text cannot inject shell syntax. Treat that data as untrusted.
- **Environment.** Commands inherit asanamate's environment. Actions drop
  inherited `ASANAMATE_*` variables first, so values from an outer run cannot
  leak in.
- **Working directory.** asanamate's own, except actions with `repo = true`,
  which run in the ticket's repo.
- **No terminal.** Except foreground and exit actions, commands run without
  a terminal: stdin is empty (or the JSON request), and prompts fail.
- **State directory.** Paths below under `~/.local/state/asanamate` use
  `$XDG_STATE_HOME/asanamate` when `XDG_STATE_HOME` is set.

## Actions

An action is one TOML file in `~/.config/asanamate/actions/`. See the
README's [Actions](../README.md#actions) section for its keys (`name`, `key`,
`mode`, `repo`, `agent`, `context`, `form`, `input`, `confirm_writes`) and
for the action builder.

**When it runs.** When you pick the action in the TUI's action menu, after
asanamate resolves its repo (`repo = true`), its agent (`agent = true`), its
form, and its input box. Cancelling any of those skips the run.

**Input.** The ticket arrives in environment variables and files:

| Variable | Value |
|---|---|
| `ASANAMATE_GID`, `ASANAMATE_TITLE`, `ASANAMATE_URL` | task gid, name, permalink |
| `ASANAMATE_SLUG` | branch-friendly title, e.g. `fix-login-bug` |
| `ASANAMATE_COMPLETED`, `ASANAMATE_ASSIGNEE`, `ASANAMATE_DUE`, `ASANAMATE_TAGS` | task details |
| `ASANAMATE_MY_SECTION` | My Tasks section |
| `ASANAMATE_PROJECT`, `ASANAMATE_PROJECT_GID`, `ASANAMATE_SECTION` | active project and the ticket's section in it |
| `ASANAMATE_REPO` | resolved repo (`repo = true` actions) |
| `ASANAMATE_WORKTREE` | the repo's worktree with `$ASANAMATE_BRANCH` checked out, the main one included; empty when none has it |
| `ASANAMATE_BRANCH` | the ticket's branch: the one saved for it, else `branch_field`'s value, else the ID field, else the title slug |
| `ASANAMATE_AGENT_STATE`, `ASANAMATE_AGENT_STATUS`, `ASANAMATE_AGENT_PATH`, `ASANAMATE_AGENT_TARGET`, `ASANAMATE_AGENT_TITLE` | the agent the action is about (the chosen one for `agent = true`, else the most urgent): normalized state, raw status, directory, jump id, title. Empty without agents |
| `ASANAMATE_AGENT_TARGETS` | every linked agent, one `target<TAB>state<TAB>path<TAB>title` line each |
| `ASANAMATE_COMMENT_GID`, `ASANAMATE_COMMENT_AUTHOR`, `ASANAMATE_COMMENT_AUTHOR_GID`, `ASANAMATE_COMMENT_DATE`, `ASANAMATE_COMMENT_TEXT` | the highlighted comment when the menu opened: story gid, author name and gid, `created_at` timestamp, and body as Markdown. Empty when no comment is highlighted |
| `ASANAMATE_TICKET_JSON`, `ASANAMATE_TICKET_MD` | full ticket as JSON / Markdown |
| `ASANAMATE_INPUT_FILE` | text typed into the `input` box, possibly empty. Empty path for actions without `input` |
| `ASANAMATE_CONVERSATION_JSON`, `ASANAMATE_CONVERSATION_PROVIDER`, `ASANAMATE_CONVERSATION_ID` | private selection snapshot, provider, and exact native session ID for a conversation opener; empty for unrelated actions |
| `ASANAMATE_PARAM_<ID>` | selected action form value, with its field ID uppercased |
| `ASANAMATE_FIELD_<NAME>` | custom field display values, e.g. `ASANAMATE_FIELD_BRANCH_NAME` |
| `ASANAMATE_CONFIRM_WRITES` | `1`/`0`, read by the write-back subcommands |

The variables above are set on every run, empty when they do not apply,
except `ASANAMATE_PARAM_*` and `ASANAMATE_FIELD_*`, which exist only for the
action's form fields and the ticket's custom fields. Control characters other
than newline and tab are removed from values. The files live in
`~/.local/state/asanamate/tickets/<gid>/run-<unique>/` (`ticket.json`,
`ticket.md`, `input.md`). Each run keeps its own private snapshot, so later
actions on the same ticket cannot overwrite a running action's context.
Snapshots remain until you remove them; commands must use the exported paths.
Foreground and exit actions read stdin from the terminal; background actions
get empty stdin.

`mode` decides how the command runs: `foreground` suspends the TUI and
resumes it afterwards, `background` runs detached, and `exit` quits
asanamate, then runs the command in its place. `asanamate --exit-on-action`
turns background actions into exit actions.

**Output.** asanamate does not read it. Foreground and exit actions write to
the terminal. Background actions append to
`~/.local/state/asanamate/actions.log`, under a `--- <time> <command>` line.
To change the ticket, call `asanamate comment`, `move`, or `field`; they
ask before writing unless `$ASANAMATE_CONFIRM_WRITES` is `0` or `--yes` is
given.

**Exit status.** For foreground and background actions, zero shows
`<name>: done` in the status line, and non-zero shows `<name> failed: exit
status <n>` (with the log path for background actions). An exit action
replaces asanamate, so its exit status becomes asanamate's.

**Timeouts.** None. A background action runs in its own session, without a
terminal, and keeps running after asanamate quits.

**Example.** A shorter `internal/action/testdata/action.sh`:

```sh
#!/bin/sh
printf 'gid=%s\n' "$ASANAMATE_GID"
printf 'branch=%s\n' "$ASANAMATE_BRANCH"
head -n 1 "$ASANAMATE_TICKET_MD"
```

```toml
# actions/show.toml
name = "Show ticket basics"
key = "i"
mode = "foreground"
command = '~/bin/show-ticket.sh | less'
```

## Conversation history

This is the generic core contract, version **1**. When both commands are set,
the action menu (`a`) gains a built-in **Conversations** entry, and
`asanamate conversations` and `asanamate conversation open` use the same
lookup and opener ([README](../README.md#conversations-optional)). The
provider companion and plugins are separate
([specs 17–18](specs/17-conversation-companion.md)).
Core has no Claude/Codex SDK, provider title parser, transcript index, or disk
cache. The TUI keeps each ticket's last successful lookup in memory only, shown
as stale while a refresh runs. Running agents and persisted conversations
remain different extension points.

Both settings default to empty, leaving the extension off. Set both together:

```toml
[conversations]
command = "asana-sessions asanamate list"
open = "asana-sessions asanamate open"
```

The optional `asana-sessions` command is not bundled or installed by core; it
lives in [`packages/asana-sessions`](../packages/asana-sessions/README.md).
Any external program can implement this contract. Both config strings are
trusted user-authored shell text; selected metadata never enters those strings.

### Discovery

`internal/conversations.Source.List` runs `command` in asanamate's working
directory with one JSON request on stdin, then EOF. It validates the selected
task GID before starting the command:

```json
{"version":1,"operation":"list","asana":{"task_gid":"42"}}
```

Sources must validate version, operation, and decimal task GID before starting
providers. Ignore unknown optional request fields; reject unsupported versions.
IDs are exact ASCII decimal GIDs accepted by `asana.ValidGID`, not numbers
extracted from prose, session IDs, branch names, or project IDs.

Return exactly one JSON object on stdout, with no banners or extra objects:

```json
{
  "version": 1,
  "conversations": [
    {
      "provider": "codex",
      "session_id": "native-session-id",
      "task_gid": "42",
      "title": "Fix sorting [asana:42]",
      "cwd": "/home/me/code/project",
      "branch": "fix-sorting",
      "updated_at": "2026-10-15T16:00:00Z",
      "archived": false,
      "activity": "unknown"
    }
  ],
  "diagnostics": [
    {"provider":"claude","code":"unavailable","severity":"info","message":"History is not installed"}
  ]
}
```

Record rules:

- Required: string `provider`, `session_id`, `task_gid`, `title`, and `activity`.
  Provider/session IDs must be nonblank without control characters; keep their
  exact values. Title is display metadata, never identity or executable text.
- `task_gid` must be valid and exactly equal the requested GID. Core does not
  infer links from titles, branch names, paths, or running agents. The portable
  trailing marker `[asana:<GID>]` and Claude tag conflict rules belong to the
  companion, as defined in [spec 16](specs/16-conversation-history.md#canonical-ticket-metadata).
- `activity` is exactly `active`, `inactive`, or `unknown`. Sources must return
  explicit `unknown` when they cannot prove activity. Old timestamps and an
  app-server's `notLoaded` status do not prove inactivity.
- Optional string `cwd` must be absolute when supplied, without NUL. It may
  point to a deleted worktree; the opener checks existence and offers recovery.
  Optional `branch` is display context. Optional `updated_at` is an RFC 3339
  timestamp, with fractional seconds allowed. Omit unknown paths/timestamps.
- Optional boolean `archived` defaults to false. Archived history is included.
  Unknown optional fields are ignored; wrong types and null known fields fail.
- Identity is `(provider, session_id)` in this one local source. Exact duplicate
  normalized records collapse, preserving first-seen order; conflicting records
  with the same identity fail the entire response. Core does not choose a winner.

`conversations` is a required array. A complete lookup with no matches returns
`{"version":1,"conversations":[]}`. `diagnostics` may be omitted or an array.
Each diagnostic requires string `provider`, `code`, `severity`, and `message`.
Codes are `unavailable`, `unsupported_version`, `permission_denied`,
`metadata_conflict`, `incomplete`, or `provider_error`; severity is `info`,
`warning`, or `error`. Invalid diagnostics also fail validation.

`Result.Complete()` is false when any diagnostic has warning/error severity.
Usable rows and all diagnostics survive a valid partial lookup. Informational
absence of an unused provider does not degrade a healthy single-provider
installation. An installed provider's failed lookup must produce warning/error,
even when another provider succeeds. If no provider is available, the source
must exit nonzero with recovery information on stderr; it must not return an
apparently complete empty lookup. Core cannot infer provider availability from
the absence of rows or informational diagnostics.

Transport limits are **4 MiB stdout**, **64 KiB stderr**, and **10 seconds**,
including shell/helper startup. Caller cancellation can shorten the deadline.
`jsoncmd.RunBounded` caps capture and terminates the command process group on
overflow/cancellation, also cleaning up remaining group members after exit.
Discovery subprocesses must remain in that process group, not detach into
separate sessions. Descendants retaining pipes after the shell exits get a
250 ms drain period before transport failure and cleanup. Existing unbounded
`jsoncmd.Run` callers retain their original contract.

Overflow, timeout, cancellation, and nonzero exit fail the whole lookup and
return no rows. Malformed or conflicting responses also return no rows.
`conversations.Failure` classifies a failed lookup as one `error` diagnostic
with provider `source`: `unavailable` (nonzero exit, such as no provider
installed), `incomplete` (timeout, overflow, cancellation),
`unsupported_version`, or `provider_error` (malformed response).
`asanamate conversations --json` prints that diagnostic in a version-1
response with no rows, and the picker shows it, keeping any earlier result as
stale instead of an empty list.
Never decode truncated output as a successful result, or silently convert
failure into no conversations. Diagnostics use one display-safe line and at
most 1024 Unicode code points, truncating longer messages with `…`. Core strips
terminal escapes, control characters, and Unicode format characters from
diagnostic messages. Use `conversations.DisplayText` for every displayed title,
path, branch, provider, and session ID. Preserve original metadata for opening.
Do not log prompts or transcript content; sources return metadata only.

**Fixture example.** `internal/conversations/testdata/source.sh` reads the full
request into `$REQUEST_FILE`, then returns the JSON in `$RESPONSE_FILE`:

```sh
#!/bin/sh
cat > "$REQUEST_FILE" || exit 1
cat "$RESPONSE_FILE"
```

Use a version-1 response like the example above. A source command can be
`/bin/sh /absolute/path/source.sh`, with those two paths supplied in its
environment. The fixture test verifies exact request bytes, validation,
duplicate/conflict handling, diagnostics, and failures without provider tools
or an Asana network request.

### Foreground opening

`action.ConversationCommand` validates the selected record against the ticket,
then writes `conversation.json` into a private action run. The TUI first
exports the ticket (`action.WriteFiles`), so the run also holds `ticket.json`
and `ticket.md`. `asanamate conversation open` reads no ticket from Asana, so
its run holds only the snapshot and `ASANAMATE_TICKET_JSON`, `ASANAMATE_TITLE`,
and other ticket exports are empty; a custom opener must accept that. The
directory is `0700`; each file is `0600`. Snapshot paths are absolute, and
every open gets its own directory. The selection file is:

```json
{
  "version": 1,
  "operation": "open",
  "asana": {"task_gid":"42"},
  "conversation": {
    "provider":"codex",
    "session_id":"native-session-id",
    "task_gid":"42",
    "title":"Fix sorting [asana:42]",
    "archived":false,
    "activity":"unknown"
  }
}
```

The command receives `ASANAMATE_CONVERSATION_JSON` (snapshot path),
`ASANAMATE_CONVERSATION_PROVIDER`, and `ASANAMATE_CONVERSATION_ID`. Unrelated
actions get empty values; inherited `ASANAMATE_*` values are always removed.
The helper runs in asanamate's cwd. Ticket repository/worktree selection and
branch action steps are not applied to saved conversations.

The caller runs the returned command through the existing foreground handoff,
with terminal stdin/stdout/stderr. JSON never occupies interactive stdin. Read
the selection through the environment's snapshot path, not from stdin. The
terminal fixture `internal/action/testdata/conversation.sh` verifies this with
the system `script` command and checks successful/nonzero exit propagation.

Before resuming, an opener must re-read native history to validate exact
provider/session identity and its current ticket link. Refuse missing or changed
links. Check cwd existence at open time; offer a replacement or cancellation
without silently checking out a branch or creating a worktree. Use argument
arrays or file data for native IDs, GIDs, and paths; never execute commands from
records or interpolate metadata into shell text. Zero means normal completion,
including a normally exited provider; nonzero means failure. Foreground callers
restore terminal state and report failure through their existing action path.

The supplied companion opener must refuse `active` conversations. For unknown
activity, explicit confirmation is necessary but does not establish safety:
native exclusion must still protect independently launched conversations.
The [live feasibility observations](specs/17-provider-feasibility.md) show that
Claude Code `2.1.301` lacks that exclusion. Under the approved conservative
revision, Claude discovery/linking remain supported, but the companion's default
Claude opener must refuse unknown activity. The companion derives Claude
`active`/`inactive` from Claude Code's live-session registry and resumes only
`inactive` conversations.
This restriction belongs to the provider opener, not a provider-specific branch
in generic core. A custom host opener must use an explicit native-ID mapping
and authoritative ownership behavior. The remaining companion feasibility and
release checks are still required; this core contract is not their approval.

## Agent sources

Set `agents.command` to list running coding agents; asanamate links each one
by explicit task GID when supplied, otherwise by branch and repo. asanamate has no built-in
sources: `asanamate-ccmux` (`cmd/asanamate-ccmux`), the ccmux adapter, is an
agents command like any other.

**When it runs.** When the TUI starts, then 5 seconds after each run
finishes, while the TUI is open. `asanamate doctor` runs it once.

**Input.** None: no arguments, empty stdin.

**Output.** One line per agent on stdout:

```
<path>\t<status>[\t<target>\t<title>\t<branch>\t<repo>\t<task_gid>\t<provider>\t<session_id>]
```

- `path`: the agent's working directory. asanamate asks git for its branch
  and repository (worktrees resolve to their repository); a path outside git
  is listed but has no branch to link by.
- `status`: the tool's raw status. A status equal to a state name (`waiting`,
  `working`, `completed`, `idle`) maps to it, ignoring case;
  `[agents.states]` maps more statuses to each state; anything else is
  `unknown`.
- `target`: optional id your tool uses to jump to the agent; actions get it
  as `$ASANAMATE_AGENT_TARGET`.
- `title`: optional short description; control characters become spaces,
  spaces collapse, and it is cut to 60 characters.
- `branch`, `repo`: optional, as a pair: the branch and main repository to
  link the agent by, for a tool that knows them better than git does at
  `path`. When both are set, asanamate uses them and skips git; otherwise git
  decides, and a set `branch` is used only when git finds none.

- `task_gid`: optional explicit decimal task GID. It overrides both branch
  and repo matching, including outside Git. Empty preserves branch/repo fallback.
  `-` suppresses all matching, for example when conversation metadata conflicts.
  Any other value fails the refresh rather than silently falling back.
- `provider`, `session_id`: optional native conversation identity, scoped by
  provider. Core treats these as opaque strings; it never parses provider titles
  or accesses native history. A decorator can use them to fill `task_gid`.

Fields are trimmed, and a trailing `\r` is dropped. Lines with no tab or an
empty path are skipped; columns past the ninth are ignored. An empty output
means no agents. Leave a column empty to skip it, such as `title` before
`branch`.

**Exit status.** Non-zero fails the whole refresh: the status line shows
`agents: agents command failed: exit status <n>`, and the previous list
stays. stderr is not shown.

**Timeouts.** 10 seconds per run in the TUI; the command is killed after
that, and the run counts as failed.

**Example.** `internal/agents/testdata/agents.sh` prints this shape:

```sh
#!/bin/sh
printf '%s\t%s\t%s\t%s\n' "$HOME/code/web" busy web-1 'Fix login'
printf '%s\t%s\n' "$HOME/code/api" permission
printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$HOME/code/web/vendor/lib" idle web-2 '' fix-login "$HOME/code/web"
```

```toml
[agents]
command = '~/bin/my-agents.sh'

[agents.states]
working = ["busy"]
waiting = ["permission"]
```

### Optional conversation links

`asanamate-ccmux` includes the provider and native session ID when ccmux knows
those fields. The separately installed companion can decorate any compatible
agent source, without enabling or calling `conversations.command`:

```toml
[agents]
command = "asana-sessions asanamate agents -- asanamate-ccmux"
```

The decorator executes the source directly, with its arguments as separate
values and empty stdin. Source failure, output over 4 MiB, or a source taking
more than 2 seconds fails the refresh. It queries each supported provider in
parallel once, only when the source includes a usable native identity, with a
6-second metadata deadline. Status, focus target, and existing explicit task
links stay intact. An exact `(provider, session_id)` match supplies the task
GID using the same title/tag rules as conversation discovery. Conflicts supply
`-`; no guessed ticket association is made.

Unknown providers, missing identities, missing history, metadata failures and
timeouts preserve branch/repo fallback for affected agents. Such fallback can
still associate an agent heuristically even when its explicit metadata could
not be read. Diagnostics go to stderr (visible when running the command directly;
the TUI does not display successful commands' stderr).

To fall back automatically when the companion executable is missing:

```toml
[agents]
command = """
if command -v asana-sessions >/dev/null 2>&1; then
  asana-sessions asanamate agents -- asanamate-ccmux
else
  asanamate-ccmux
fi
"""
```

Older sources without native identity still work through branch/repo matching.
Older core versions ignore the extra columns and retain their existing matching.
Other tools can supply `task_gid` directly; no dependency on this companion,
ccmux, Claude Code, or Codex is required by core.

## Repo sources

Set `repo_source.command` to fill the repo picker from your own list, such
as `sesh list -z`. It replaces `repo_source.root`.

**When it runs.** Each time asanamate opens the repo picker, such as for the
first `repo = true` action on a project with no linked repo, or when you link
a repo with `L`.

**Input.** None: no arguments, empty stdin.

**Output.** One repo path per line on stdout. asanamate trims each line,
expands a leading `~` to your home directory, and skips blank lines and
repeats, keeping the order. Paths are not checked until you pick one; then
asanamate needs a git working tree there.

**Exit status.** Non-zero opens the picker with no listed repos and the error
`repo_source command failed: exit status <n>`; you can still type a path.
stderr is not shown.

**Timeouts.** 1 minute; the command is killed after that, and the run counts
as failed.

**Example.** `internal/repo/testdata/repos.sh` prints this shape:

```sh
#!/bin/sh
printf '%s\n' '~/code/web' '~/code/api'
```

```toml
[repo_source]
command = '~/bin/repos.sh'
```

## Time tracking

Set `time_tracking.command` to log time to a tracker, and `time_tracking.id`
to a name for it. asanamate's Harvest provider, `asanamate-hrvst`
(`cmd/asanamate-hrvst`), is a separate binary that uses only this protocol,
like any other provider. Use a different `id` when changing trackers so saved
choices stay separate.

**When it runs.** Once per request, as a new process each time:

| Operation | When |
|---|---|
| `form` | `t` on a ticket, after choosing an Asana project when it has several |
| `log` | submitting the form |
| `summary` | loading a ticket's details, and after a successful log |

**Input.** One JSON object on stdin, then end of input:

| Field | Value |
|---|---|
| `version` | protocol version, `1` |
| `operation` | `form`, `log`, or `summary` |
| `values` | `log` only: form values keyed by field ID. Select values are option IDs; `hours` is positive decimal hours as typed, such as `"1.25"`; `date` is `YYYY-MM-DD` |
| `asana` | `task_gid`, `project_gid`, `title`, `url`. `summary` sends only `task_gid` and `url`; the others are empty |
| `links` | `form` only, when [project links](#project-links) knows the chosen Asana project: its links, best first. Omitted otherwise |

```json
{"version":1,"operation":"form","asana":{"task_gid":"42","project_gid":"7","title":"Fix login","url":"https://app.asana.com/0/7/42"},"links":[{"type":"harvest_project","value":"123"}]}
{"version":1,"operation":"log","values":{"hours":"1.5","project_id":"123","spent_date":"2026-10-05"},"asana":{"task_gid":"42","project_gid":"7","title":"Fix login","url":"https://app.asana.com/0/7/42"}}
{"version":1,"operation":"summary","asana":{"task_gid":"42","project_gid":"","title":"","url":"https://app.asana.com/0/7/42"}}
```

**Compatibility.** Providers may ignore `version`, and must ignore request
fields they do not know: new optional fields are added without changing the
version. The version changes only for a change existing providers could not
ignore.

**Output.** One JSON object on stdout, depending on the operation:

- `form`: a form spec. IDs are unique snake_case names, and labels are not
  empty. Types are `select` (needs `options`, each with a stable, unique `id`
  and a `name`), `bool` (`"true"` or `"false"`), `number` (any decimal),
  `string` (any text, may be empty), `hours` (positive decimal hours), and
  `date` (`YYYY-MM-DD`; the user may type `date -d` text such as `+1 day`,
  which asanamate resolves first). Every field except `string` needs a value. A time form needs exactly one
  `hours` field. `remember: true` on a select saves the choice per Asana
  project. The optional string `default` sets a field's initial value; hours
  cannot have one. A select's `default` names one of its options, such as the
  project a `links` entry suggests; a remembered choice that is still offered
  wins over it. A `bool` without a default starts as `"false"`, and a `date`
  as today.

  ```json
  {"fields":[
    {"id":"project_id","label":"Project","type":"select","remember":true,
     "options":[{"id":"123","name":"Web"}]},
    {"id":"hours","label":"Hours","type":"hours"},
    {"id":"spent_date","label":"Date","type":"date"}
  ]}
  ```

- `log`: nothing. Exit zero only after the entry exists. asanamate never
  retries a failed log, because the provider may have created the entry
  before failing.
- `summary`: `{"total_seconds":13500,"today_seconds":4500}`. `total_seconds`
  is a required nonnegative integer: all-time,
  unrounded time from stopped entries linked to this exact ticket, across
  projects, visible to the account. Return `0` when there are none. Match by
  the ticket, not its title, and fetch every page first; never return a
  partial total. Optional `today_seconds` is a nonnegative integer no greater
  than `total_seconds`: the subset logged on today's local calendar date,
  using the entry's logged date rather than its creation time. Return `0`
  when none were logged today; omit it or return `null` when daily totals
  are unavailable. The header shows the total followed by today's value,
  when supplied. Harvest calculates both values from the same pages.
  A provider without totals returns `{"unsupported":true}`.

**Exit status.** Non-zero is a failure; asanamate shows the provider's stderr
with the error. A failed `form` or `log` shows in the status line or the
form. A failed or invalid `summary` shows the ticket's time as unavailable
(`{"unsupported":true}` shows unsupported) and blocks neither reading the
ticket nor logging time, so providers that reject `summary` as an unknown
operation keep working.

**Timeouts.** 1 minute per request; the command is killed after that, and the
request counts as failed.

**Example.** `internal/timetracking/testdata/provider.sh`, without its
request log:

```sh
#!/bin/sh
request=$(cat)
case $request in
*'"operation":"form"'*)
	printf '%s\n' '{"fields":[{"id":"project_id","label":"Project","type":"select","remember":true,"options":[{"id":"123","name":"Web"}]},{"id":"hours","label":"Hours","type":"hours"}]}'
	;;
*'"operation":"log"'*)
	;; # create the entry here; exit non-zero if that fails
*'"operation":"summary"'*)
	printf '%s\n' '{"total_seconds":13500}'
	;;
*)
	echo "unknown operation" >&2
	exit 1
	;;
esac
```

Matching the raw request text keeps the example free of dependencies; a real
provider should parse it with a JSON tool such as `jq`.

```toml
[time_tracking]
id = "my-tracker"
command = '~/bin/my-tracker.sh'
```

## Project links

Set `project_links.command` to suggest where each Asana project lives in
other systems: its git repository and its time tracking project. imarc's
Hub provider, `asanamate-hub`, lives in its own repository and uses only this
protocol, like any other provider. asanamate only reads links;
providers never receive anything to write.

**When it runs.** Once per lookup, as a new process each time:

- opening the repo picker for a project, such as for a `repo = true` action
  on a project with no linked repo, or when you link a project's repo with `L`
- `t` on a ticket, after choosing an Asana project, before the time form's
  `form` request

**Input.** One JSON object on stdin, then end of input:

| Field | Value |
|---|---|
| `version` | protocol version, `1` |
| `operation` | `links` |
| `asana` | `project_gid`, the Asana project's GID |

```json
{"version":1,"operation":"links","asana":{"project_gid":"7"}}
```

**Compatibility.** As for [time tracking](#time-tracking): ignore `version`
and unknown fields if you like; the version changes only for a change
existing providers could not ignore.

**Output.** `{"links":[...]}` on stdout: the project's links, best first, each
with a nonempty `type` and `value`. Return `{"links":[]}` when the project is
unknown.

| Type | Value | Used by |
|---|---|---|
| `git` | a git remote URL, such as `https://github.com/acme/web` or `git@github.com:acme/web.git` | the repo picker: local repos with a remote that matches, ignoring scheme, user, port, case, and a trailing `.git`, come first, marked `suggested`, and the cursor starts on the first. Clones come before their worktrees |
| `harvest_project` | a Harvest project ID | `asanamate-hrvst`, which preselects it in the time form |

asanamate reads only `git` itself. Every link, of any type, goes unchanged to
the time provider's `form` request as `links`, so other trackers can define
their own types.

```json
{"links":[
  {"type":"git","value":"https://github.com/acme/web"},
  {"type":"harvest_project","value":"123"}
]}
```

**Exit status.** Non-zero is a failure; asanamate shows `no suggestions:`
with the provider's stderr in the picker or status line, and the picker and
time form work without suggestions.

**Timeouts.** 1 minute per lookup, counted with the rest of the request it
belongs to; the command is killed after that, and the lookup counts as failed.

**Example.** `internal/projectlinks/testdata/provider.sh`, without its
request log:

```sh
#!/bin/sh
request=$(cat)
case $request in
*'"project_gid":"7"'*)
	printf '%s\n' '{"links":[{"type":"git","value":"https://github.com/acme/web"},{"type":"harvest_project","value":"123"}]}'
	;;
*)
	printf '%s\n' '{"links":[]}'
	;;
esac
```

```toml
[project_links]
command = '~/bin/project-links.sh'
```

## Feeds

Add commands to `feed.commands` to show their lines in the Feed panel under
the ticket list, such as time entries, CI runs, deploys, or meetings. A feed
only reports; asanamate never sends it anything to write. asanamate's Harvest
provider answers this protocol too: add `asanamate-hrvst` to list the day's
Harvest time entries.

**When it runs.** As a new process for each feed when the TUI starts, every
`feed.refresh_interval` while it is open, and on reload (`r`).
`asanamate doctor` runs each once.

**Input.** One JSON object on stdin, then end of input:

| Field | Value |
|---|---|
| `version` | protocol version, `1` |
| `operation` | `feed` |
| `date` | today's local date, `YYYY-MM-DD` |

```json
{"version":1,"operation":"feed","date":"2026-10-12"}
```

**Compatibility.** As for [time tracking](#time-tracking): ignore `version`
and unknown fields if you like; the version changes only for a change
existing providers could not ignore.

**Output.** One JSON object on stdout:

| Field | Value |
|---|---|
| `source` | required, 1 to 8 characters, shown at each item's right edge, such as `ci` |
| `summary` | optional; joined into the panel's top edge, cut to 24 characters, such as `1 failed` |
| `icon` | optional: the glyph before `summary`, one of `clock`, `due`, `branch`, `repo`, `clip`, `folder`, `refresh`, `bell`, or `warning`. Glyphs follow `symbols`: `nerd` draws them all, `unicode` and `ascii` only `clock`; an unknown name draws none |
| `items` | required, may be empty: the feed's lines |

Each item has:

| Field | Value |
|---|---|
| `time` | required, RFC 3339. A time after now is an upcoming item, listed first under `Next` and shown as `in 25m` |
| `text` | required, one line; control characters become spaces, and it is cut to fit |
| `tone` | optional: `ok`, `warn`, `error`, or `muted`, colored by the theme; empty is the plain text color |
| `ticket_gid` | optional: the Asana task the item belongs to |
| `url` | optional: an Asana task URL links the item to that task; any other URL is what `enter` opens |
| `branch`, `repo` | optional: link the item like an agent, to the loaded ticket on `branch`, limited to tickets linked to `repo` when it is set |

asanamate links an item by `ticket_gid`, then `url`, then `branch`: the first
that names a loaded ticket wins, and with several tickets on one branch, the
first in the list. A linked item shows its ticket's title after its text,
unless its text already ends with " · " and the title, and `enter` opens
the ticket. An item that links to nothing still shows.

```json
{"source":"ci","summary":"1 failed","icon":"warning","items":[
  {"time":"2026-10-12T14:05:00-04:00","text":"build failed","branch":"feature/x","tone":"error"},
  {"time":"2026-10-12T15:30:00-04:00","text":"deploy window","url":"https://example.com/deploys"}
]}
```

**Exit status.** Non-zero or invalid output fails that run: the panel shows
`<source> failed` in its summary and keeps the feed's previous items. stderr
is not shown in the panel; `asanamate doctor` shows it. Other feeds are
unaffected.

**Timeouts.** 1 minute per run; the command is killed after that, and the run
counts as failed.

**Example.** `internal/feed/testdata/feed.sh`, without its request log:

```sh
#!/bin/sh
request=$(cat)
case $request in
*'"operation":"feed"'*)
	printf '%s\n' '{"source":"ci","summary":"1 failed","icon":"warning","items":[
	  {"time":"2026-10-12T14:05:00-04:00","text":"build failed","branch":"feature/x","repo":"/code/web","tone":"error"},
	  {"time":"2026-10-12T15:30:00-04:00","text":"deploy window","url":"https://example.com/deploys"}]}'
	;;
*)
	echo "unknown operation" >&2
	exit 1
	;;
esac
```

```toml
[feed]
commands = ["asanamate-hrvst", "~/bin/ci-feed.sh"]
```
