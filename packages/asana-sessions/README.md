# asana-sessions

Find, link, and resume Claude Code and Codex conversations by Asana ticket.

`asana-sessions` reads each provider's own saved conversation metadata and
looks for a ticket marker at the end of the conversation title:

```text
Fix task sorting [asana:1234567890]
```

It needs no Asana token, makes no model requests, and does not need asanamate.
It reads titles and other metadata only, never transcript text.

Releases are distributed as GitHub tarballs under the
[standalone release gates](RELEASING.md). The package is marked `private`
so it cannot be published to npm by accident.

## Install

The package lives in the asanamate repository for now. From the repository
root, `sh ./install.sh --interactive` builds and installs it as below, then runs
`asana-sessions setup`; later `sh ./install.sh` runs update it and its plugins. By hand, with
Node and npm:

```sh
cd packages/asana-sessions
npm ci
npm pack
node install.mjs ./asana-sessions-0.1.2.tgz
asana-sessions doctor
```

The installer needs Node 22 or later, npm, and tar. It defaults to `~/.local`;
pass `--prefix /your/directory` to choose another prefix. Put `<prefix>/bin`
on every provider's `PATH`, before any older npm-global installation.

The installer unpacks into a separate directory under
`<prefix>/lib/asana-sessions`, runs `npm ci --omit=dev` against the shipped
shrinkwrap, and switches the executable symlink only after installation succeeds.
An unsuccessful install leaves the current command intact. Existing ordinary
files or directories at the command path are never replaced. Previous install
directories remain available to processes already using them.

Dependencies install only at installation time. Package CI targets Node 22 and
24 on macOS and Linux. Use this installer rather than `npm install --global`:
global tarball installs can re-resolve dependencies despite the included
shrinkwrap. No registry account is needed to build or install locally.

Standalone releases use `asana-sessions/vX.Y.Z` tags and supply a `.tgz`,
`install.mjs`, and `checksums.txt`. Download all three from the same release.
Verify downloaded assets with `sha256sum -c checksums.txt`
(or `shasum -a 256 -c checksums.txt` on macOS), then run
`node install.mjs ./asana-sessions-<version>.tgz`.
No public npm registry name is reserved or published.

## Commands

| Command | Behavior |
|---|---|
| `list <task-url-or-gid> [--json]` | List every local conversation linked to the ticket |
| `open <task-url-or-gid> --provider <name> --session <id>` | Re-check one linked conversation, then resume it in this terminal |
| `link --provider <name> --session <id> --task <task-url-or-gid>` | Add the ticket marker to one conversation's title and verify it |
| `link ... --replace` | Replace a link to another ticket |
| `unlink --provider <name> --session <id>` | Remove the ticket marker and keep the rest of the title |
| `setup [--provider <claude\|codex\|both>]` | Register optional native plugins; show remaining trust and restart steps |
| `setup --refresh` | Re-register only stale registered plugins; the installer runs it after each install |
| `remove [--provider <claude\|codex\|both>]` | Remove only this addon's plugins and pending state; preserve saved links |
| `doctor` | Check Node, the Claude Agent SDK, both CLIs, and history access |
| `help`, `version` | Work without any provider installed |
| `asanamate list`, `asanamate open` | The [asanamate](../../README.md) conversation adapter |

`<name>` is `claude` or `codex`. A task is an HTTPS `app.asana.com` task URL
(`/0/<project>/<task>`, `/1/<workspace>/.../task/<task>`, either with an
optional `/f`) or a numeric task GID. Project and profile URLs, other hosts,
credentials, ports, and numbers in prose are rejected.

### Exit status

- `0`: success, an already-correct link (`"changed": false`), or a complete
  lookup, including no matches when one provider is simply not installed.
- `1`: a failed operation, a failed verification, a cancelled open, or a
  partial or unavailable lookup. `list` still prints what it found.
- `2`: a usage error.
- `open` otherwise exits with the resumed provider's status.

### JSON

`list --json` prints the record shape of asanamate's conversation protocol,
version 1 ([docs/extensions.md](../../docs/extensions.md#conversation-history)):
`version`, `conversations` (with `provider`, `session_id`, `task_gid`,
`title`, `activity`, `archived`, and, when known, `cwd`, `branch`,
`updated_at`), and `diagnostics` (`provider`, `code`, `severity`, `message`).
Warning or error diagnostics mean the lookup is partial. The JSON version is
independent of the package version.

`link` and `unlink` print `provider`, `session_id`, `task_gid` (for `link`),
the verified `title`, and `changed`.

## Link rules

- One marker per conversation. Linking again is a no-op; duplicate identical
  markers collapse to one.
- A different ticket's marker is a conflict unless you pass `--replace`.
- An untitled conversation becomes `Conversation [asana:<GID>]`. The first
  prompt is never copied into a title.
- Codex prompt hooks defer linking an unnamed thread until Stop, allowing
  Codex to generate its title first. Stop appends the marker to that title,
  or uses `Conversation` if the thread is still unnamed. Named threads and
  explicit `link` commands still link immediately. During the first turn,
  an unnamed thread has no saved ticket marker yet.
- Claude Code generates its own title after the conversation starts, but
  hides it once a title is set. For a `Conversation [asana:<GID>]` link,
  `list` shows the generated title from the transcript instead, and the Stop
  hook renames the conversation to `<generated title> [asana:<GID>]` at the
  first turn that has one. Only the exact fallback prefix is replaced. The
  transcript format is not a documented API; if it changes, the fallback
  title stays.
- Renaming a conversation natively so the marker disappears removes the link.
- Explicit `unlink` also cancels a deferred first-prompt link, even before
  Claude saves the transcript or Codex generates its title. A later Stop
  hook will not restore it.
- Claude's tag `asana:<GID>` is also recognized. A tag and title that disagree
  are a conflict; `link --replace` and `unlink` clear only that tag.
- Writes re-read the title first and verify it afterwards. A rename by the
  provider between those steps is detected and reported, not retried. A short
  local lock under `$XDG_STATE_HOME/asana-sessions/` (default
  `~/.local/state/asana-sessions/`) serializes this tool's own writes only.

## Opening

`open` re-reads the exact conversation and refuses it when it is missing or no
longer linked to the ticket. It then:

1. Refuses a conversation that is active in another process.
2. For unknown activity: Claude is refused. Codex asks for confirmation on the
   terminal; without a terminal it fails.
3. Uses the recorded directory, or asks for an existing replacement for this
   launch. It does not change Git branches or create worktrees.
4. Restores an archived Codex conversation first. It says so before the
   unknown-activity question, because the provider's full screen hides later output.
5. Resumes in the foreground: `claude --resume <id>` without an inherited
   `CLAUDE_CODE_CHILD_SESSION`, or `codex resume <id>` without the parent
   agent's `CODEX_THREAD_ID` and `CODEX_SESSION_ID`.

**Claude activity.** Claude Code lets two processes resume one conversation at
once, which can interleave its history. Every running Claude Code process
records its conversation in `$CLAUDE_CONFIG_DIR/sessions/<pid>.json` (default
`~/.claude/sessions/`), updates it on `/clear` and `/resume`, and removes it on
exit. A conversation listed there by a live process (same PID and start time)
is `active`; otherwise it is `inactive`, and `open` resumes it directly. If
the registry is missing or has an unexpected shape, activity is `unknown` and
`open` prints the `claude --resume <id>` command instead of running it. The
registry is not a documented API, so a Claude Code update could change it.
Conversations run under a different `CLAUDE_CONFIG_DIR` are not visible.

**Codex activity** is always `unknown`: a helper connection cannot see other
servers. Resuming through the shared daemon attaches to its one copy of the
conversation rather than forking it.

## Compatibility

Exercised on macOS with disposable provider homes:

| Component | Version |
|---|---|
| Node | 24.18.0 |
| `@anthropic-ai/claude-agent-sdk` | 0.3.301 (pinned) |
| Claude Code | 2.1.301 |
| Codex CLI | 0.162.1 |

Plugin compatibility floors are Claude Code 2.1.301 and Codex 0.162.1;
older versions are skipped by setup. Newer versions still need the live gate
before a standalone release. The pinned SDK brings 105 installed packages,
including peers and platform packages. Metadata operations do not need an
Asana token or model request. Explicit Claude metadata operations work without
the Claude executable; plugin installation needs the CLI.

Proven with real conversations:

- Claude: the session registry's `active`/`inactive` result for fresh,
  resumed, print-mode, `/clear`, `/resume`, and killed processes, and direct
  `open` of an inactive conversation; listing, lookup, `link`, and `unlink`
  through the SDK with no `claude` executable on `PATH`; naming an untitled conversation; a
  5,013-character title; renaming during a running turn and while the
  conversation is open in a terminal, which survives exit. Claude has no
  archive concept.
- Codex: listing with explicit sources and the title search; `link`,
  `unlink`, and `--replace`; a 5,011-character title; renaming during a
  running turn; archive listing; and interactive `open` of an active and an
  archived conversation. Two concurrent `open` runs attach to the shared
  daemon's single copy of the thread rather than forking it.
- Cold lookup over 282 Claude and 207 Codex sessions took about 1 second.

`codex resume` leaves a background daemon running under `CODEX_HOME`, and
that daemon can update itself independently of the `codex` CLI. A Claude Code
session launched from inside another one inherits `CLAUDE_CODE_CHILD_SESSION`
and stops saving its transcript; run `claude --resume` from a plain shell.

Earlier live checks pass for both providers at the versions above. They cover
immediate Codex linking and Claude's real Stop hook completing the first
deferred link after persistence. The new Codex title deferral has regression
coverage; its native Stop/title timing still needs the live release gate.
Both providers preserve links across later prompts and process restarts,
reject ambiguity/conflicts without blocking, and support the explicit skill.
Claude also preserves an unrelated tag through change/unlink/relink and reopens
natively after plugin removal. Measured hook durations are 33–596 ms for Codex
and 31–81 ms for Claude on disposable histories. Native upgrade, trust,
disable/removal and missing-helper checks are recorded in the
[feasibility notes](../../docs/specs/17-provider-feasibility.md). Remote
Node/platform CI remains a separate release gate.

## Live agent ticket links

`asana-sessions asanamate agents -- <agent-source> [args...]` runs an existing
asanamate agent source and fills explicit task GIDs from native conversation
metadata. For example:

```toml
[agents]
command = "asana-sessions asanamate agents -- asanamate-ccmux"
```

The source supplies live status, terminal focus and optional exact provider/session
identity. The companion adds ticket identity only; it never infers identity from
a directory, branch, newest session, or display title. Core remains provider
independent, and `conversations.command` need not be enabled. Any source can
provide the same optional columns or explicit task GIDs directly.

Missing identities, unsupported providers, absent history and metadata errors
retain repo/branch matching. Conflicting metadata suppresses matching until
repaired. Source failures fail the refresh, preserving the previous agent list.
Limits are 2 seconds and 4 MiB for the source, then 6 seconds for metadata lookup.
Diagnostics go to stderr; run the command directly to inspect them. The TUI
does not display successful commands' stderr. See the
[extension contract](../../docs/extensions.md#optional-conversation-links) for
column details and automatic fallback when this executable is missing.

## Optional native plugins

The helper works on its own. Install plugins only when you want automatic links:

```sh
asana-sessions setup                 # choose Claude, Codex, both, or cancel
asana-sessions setup --provider both # noninteractive selection
asana-sessions doctor
```

A submitted prompt with exactly one supported Asana task URL links that exact
conversation, **even when you only quote the URL**. Duplicate URLs for the same
task count once. No URL does nothing. Multiple tasks produce a nonblocking
notice. An existing different task is a conflict; automation never replaces it.
Only the submitted user prompt is examined. System context, tool output,
assistant text and transcript contents are not searched. To link deliberately,
keep hooks disabled and use the bundled `link-asana-task` skill or the CLI.

A launcher can name the task instead: start the provider with
`ASANA_SESSIONS_TASK` set to a task URL or GID, and the prompt hook links that
task whatever the prompt contains, such as a full ticket dump with links to
other tasks. The variable counts only when the hook runs inside that
conversation's own process, which `CLAUDE_CODE_SESSION_ID` or `CODEX_THREAD_ID`
matching the event's session shows; otherwise, as in a shared Codex daemon,
the prompt rule above applies. An invalid value produces a notice and no link.
Every conversation started with the variable links to its task, including
nested ones.

The skill supports linking, changing and unlinking. `--current` reads
`CLAUDE_CODE_SESSION_ID` or `CODEX_THREAD_ID`; if the host does not export it,
supply `--session <id>` explicitly. Never select the newest conversation.
An explicit request to change the linked task authorizes `--replace`.

Setup uses native local marketplaces, with stable identity
`asana-sessions@asana-sessions`. It prints installed provider versions and
registration status. Setup exits 1 while helper visibility or selected plugin readiness needs repair.
Registration is distinct from readiness: diagnostics report
missing, incompatible, stale, disabled, untrusted, ready or error states. Claude
loads hooks in a new session or after `/reload-plugins`. Codex requires hooks to
be enabled in the host and the exact prompt and Stop hooks reviewed and trusted
through `/hooks`. Setup does not enable features, approve trust or bypass host policy.
Review changed hook definitions after upgrades.

Hooks invoke `asana-sessions` from the provider's PATH. Nothing downloads at
hook time. If doctor finds no helper or a different installation, fix the PATH
used to start the provider and restart it. Login-shell profiles can change PATH
and provider homes: hooks validate `transcript_path` against the exact session ID
and derive the active home from it. Doctor checks its own inherited PATH; verify
PATH inside the provider too when those environments differ.

Codex can run hooks in its shared background app-server daemon. The daemon keeps
the PATH it started with, which may differ from your shell's PATH. A Codex hook
that fails with exit code 127 could not find `asana-sessions`. This happens when
a Node version manager such as mise or nvm installed the helper under one Node
version's `bin`, and the daemon's PATH names another Node version. Install the
helper into a directory on every PATH, such as the installer's default
`~/.local/bin`, or restart the daemon from a shell where `asana-sessions` resolves.

Named Codex threads link immediately through the native metadata API; unnamed
threads defer linking until Stop so Codex can generate a title first. Claude
can submit the first prompt before the session is saved. Both plugins use Stop
to complete deferred links. Private pending files contain provider, session ID,
GID and timestamp, never the prompt. They expire after 24 hours and are swept on subsequent linking
attempts; Stop removes the entry after attempting completion. State lives under
`$XDG_STATE_HOME/asana-sessions/pending/` (default
`~/.local/state/asana-sessions/pending/`). Errors do not silently replace links.

Event input is limited to 1 MiB. The helper deadline is 10 seconds, below the native 15-second hook timeout.
Local live checks confirm this budget on the tested disposable histories. Malformed events, unknown identities, write
failures and timeouts produce sanitized nonblocking notices. A timeout can leave
write status uncertain; inspect the saved title. Hooks never prompt or deny chat.
All linking stays local; GUI/Paseo title synchronization is not promised.

Upgrade by running the installer with the new package tarball and the same
prefix. The installer then runs `setup --refresh`, which re-registers plugins
you already registered so their hooks match the new version. It skips
providers you never registered, and skips everything when the new installation
is not the `asana-sessions` on `PATH`, since hooks would not run it. Doctor
reports stale versions or source paths; rerun setup if a refresh fails.
Setup replaces only this addon's stale
registration before reinstalling. Native hash checks retain trust for unchanged
hooks; changed definitions require review again.
Existing skills and AGENTS.md/CLAUDE.md files are left alone.

```sh
asana-sessions remove --provider both
# For the default installer prefix, after disabling core commands:
rm ~/.local/bin/asana-sessions
rm -rf ~/.local/lib/asana-sessions
```

Use your chosen prefix instead of `~/.local` when different. An older
npm-global installation is removed separately with
`npm uninstall --global asana-sessions`.

You can also disable the plugin through its host. Removing or disabling plugins
preserves conversation titles, tags and transcripts. The installed helper still
lists and opens links. Bulk unlink is not part of removal.

## Optional asanamate integration

The helper and plugins do not need asanamate. Core's conversation protocol is
version 1, supported by `asana-sessions` 0.1.x. The products have independent
versions; matching release numbers are not required. Opt in manually or through
core's normal config-update workflow:

```toml
[conversations]
command = "asana-sessions asanamate list"
open = "asana-sessions asanamate open"
```

Setup never probes or rewrites asanamate configuration. If you uninstall the
helper, clear both commands; `asanamate doctor` warns about a missing
configured executable. With both commands set, asanamate lists saved
conversations in its agents modal (`w`) and `asanamate conversations`; see its
[Conversations](../../README.md#conversations-optional) section.

## Development

```sh
npm ci
npm run check          # validate packaged native assets
npm test               # node:test, with fake SDK and app-server
scripts/check-pack.sh  # install the packed tarball in a clean prefix
```

Release gates and authenticated checks: [RELEASING.md](RELEASING.md).

`test/fixtures/task-urls.json` is shared with asanamate's Go URL parser test.

## License

[GNU GPL v3.0 or later](LICENSE).
