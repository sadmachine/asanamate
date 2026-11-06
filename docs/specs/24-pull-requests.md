# 24: Pull requests

**Depends on nothing.** Builds on rule groupings (#17) and the Now place (#21),
both on `main`.

## Goal

asanamate knows which pull request belongs to each ticket and where it
stands: open or merged, checks passing or failing, approved or waiting on
changes. Filters, rule groupings, and Now use that to decide where a ticket
belongs. The reader shows the ticket's pull requests and opens them.

GitHub calls them pull requests and GitLab calls them merge requests. This
spec, the docs, and the UI say **pull request** for both, and mention merge
requests once where a GitLab user would look for them.

## Decisions

Agreed with the user on 2026-11-05:

- **An extension protocol plus a bundled adapter.** Core never calls GitHub
  or GitLab. A new JSON extension point returns each ticket's pull requests.
  A bundled adapter, `asanamate-pr`, implements it with the `gh` and `glab`
  CLIs and ships in the release archives like `asanamate-hrvst` and
  `asanamate-ccmux`.
- **Any git host.** Nothing in the protocol is specific to GitHub or GitLab.
  `pull_requests.commands` is a list, like `feed.commands`, so providers for
  different hosts (Bitbucket, Gitea, Azure DevOps, Gerrit) run side by side
  with `asanamate-pr`, each answering for the repos on its host. The request
  carries each repo's remotes, so a provider picks its repos without running
  git. Agreed with the user on 2026-11-06.
- **Match by branch, then by link.** A pull request belongs to a ticket when
  its head (source) branch is the ticket's branch, or when its description
  links the Asana task. Branch matches come first.
- **Filter terms `pr:`, `checks:`, and `review:`** register through the
  `filter.Env.Lookup` seam, like `agent:`, `git:`, and `new:`. Grouping code
  never learns where values come from. The terms work in `/`, saved views,
  hooks' `when`, and `[[groupings]]`.

Decided while writing this spec. **Confirm them with the user before you
start**, since they change what users see:

- The adapter is named `asanamate-pr`.
- The built-in `state` and `now` groupings gain pull request sections (see
  [Built-in groupings](#built-in-groupings)). Without a provider configured
  the new terms match nothing, so nothing changes for those users.
- The lookup runs with the Feed panel's refresh (start, every
  `feed.refresh_interval`, and `r`) instead of with its own interval key.

## Context

Read these before you start:

- `AGENTS.md`: config key rules, What's new files (`internal/whatsnew/unreleased/`),
  PR workflow.
- `docs/extensions.md`: the contract docs. Project links and Feeds are the
  closest models for the new section: one JSON request on stdin, one JSON
  reply on stdout, `version`, compatibility, exit status, timeouts, and a
  fixture example.
- `internal/jsoncmd/`: `RunBounded` runs a provider with output limits and
  process-group cleanup. Use it; do not exec providers yourself.
- `internal/feed/feed.go` and `internal/projectlinks/`: small packages that
  each own one protocol (`ProtocolVersion`, request and reply types, a
  `Fetch`-style function, `testdata/*.sh` fixtures, and protocol tests). The
  new `internal/pullrequests/` package follows them.
- `internal/tui/feedpanel.go`:
  - `refreshFeed` batches `runFeeds` and `checkGit`; the new lookup joins
    that batch.
  - `checkGit` builds one target per loaded ticket with linked repos, using
    `action.Branch` and `State.LinkedRepos`. The pull request lookup uses the
    same targets.
  - `gotGit` stores per-ticket values in `m.feed.states` and calls
    `applyFilter` only when they changed. Copy that pattern.
- `internal/tui/listrow.go`: `filterEnv` registers lookups. Add the new keys
  there.
- `internal/filter/filter.go`: `keys`, the `Key*` constants, `lookupKeys`,
  and the package doc. `Validate` rejects unknown keys, so groupings that use
  the new terms only validate once the keys exist.
- `internal/config/groupings.go`: `builtinGroupings` (`state`, `now`).
- `internal/extensions/catalog.go` and `contracts.go`: catalog entries and
  contract descriptions shared by `asanamate extensions` and Settings & tools.
- `cmd/asanamate-hrvst/` and `cmd/asanamate-ccmux/`: bundled adapters.
  `.goreleaser.yaml` and `.github/workflows/ci.yml` (archive contents and
  smoke tests) list them; add the new one in the same places.
- `internal/tui/cards.go`, `streamlined.go`, and `markdown.go`: the reader
  layouts. They share targets through `fieldTargets`, `cardTargets`,
  `fieldLines`, and `fieldKey`. A layout decides only how to draw a target,
  never what it does (see `docs/mocks/k-reader-title.png` and the Since
  card in `since.go` for a section that every layout draws).
- `internal/browser/`: `browser.Open` validates and opens URLs.
- `internal/display/`: `Line` and `Clean` strip controls from untrusted text.

## Protocol

Add a `## Pull requests` section to `docs/extensions.md`, a row to its
table, and a `pull-requests` contract to `extensions.Contracts()`.

**When it runs.** Each command in `pull_requests.commands` runs once per
refresh, as a new process, with every loaded ticket that has a linked repo
in one request. Commands run concurrently and independently:

- when a list loads (`gotTasks`), with the git check
- every `feed.refresh_interval`, and on `r`, with the feeds
- `asanamate doctor` runs it once and shows its stderr

Skip the run when no ticket has a linked repo. Loaded tickets are `m.tasks`,
the pin extras, and the Viewing ticket.

**Input.** One JSON object on stdin:

| Field | Value |
|---|---|
| `version` | protocol version, `1` |
| `operation` | `lookup` |
| `tickets` | the tickets to look up |

Each ticket has:

| Field | Value |
|---|---|
| `gid` | the Asana task GID |
| `url` | the task's `permalink_url` |
| `branch` | the ticket's branch as `action.Branch` returns it, including the ID-field and title-slug fallbacks |
| `repos` | the ticket's linked repos (`State.LinkedRepos`) |

Each repo has:

| Field | Value |
|---|---|
| `path` | the repo's absolute path |
| `remotes` | its git remotes, `origin` first, then by name; each has `name` and `url` as `git remote -v` prints the fetch URL. Empty when the repo has none or git fails |

```json
{"version":1,"operation":"lookup","tickets":[
  {"gid":"1201","url":"https://app.asana.com/0/0/1201","branch":"feature/login",
   "repos":[{"path":"/code/web","remotes":[{"name":"origin","url":"git@github.com:acme/web.git"}]}]}
]}
```

**Several providers.** Every command gets the same request. A provider
answers for the repos on hosts it serves and silently skips the rest, so it
does not fail or warn about repos another provider handles. asanamate merges
the replies in `pull_requests.commands` order and drops a pull request whose
`url` an earlier reply already gave.

**Compatibility.** As for the other JSON protocols: providers ignore
`version` and unknown fields if they like. The version changes only for a
change existing providers could not ignore.

**Output.** One JSON object on stdout:

```json
{"pull_requests":[
  {"ticket_gid":"1201","url":"https://github.com/acme/web/pull/42","number":42,
   "title":"Fix login redirect","state":"open","checks":"failing",
   "review":"changes","author":"zed","updated_at":"2026-11-05T14:05:00Z",
   "repo":"/code/web","branch":"feature/login"}
]}
```

| Field | Value |
|---|---|
| `ticket_gid` | required: a GID from the request; asanamate drops items for any other ticket |
| `url` | required: an `http` or `https` URL; `enter` opens it |
| `title` | required, one line |
| `state` | required: `open`, `draft`, `merged`, or `closed` |
| `number` | optional: shown as `#42`, or `!42` for GitLab when the provider says so (see `ref`) |
| `ref` | optional: the display reference, such as `#42` or `!42`; overrides `number` |
| `checks` | optional: `passing`, `failing`, `pending`, or empty for none or unknown |
| `review` | optional: `approved`, `changes` (changes requested), `pending` (a review is required and none is given), or empty |
| `author`, `updated_at` | optional: login, and RFC 3339 time |
| `repo`, `branch` | optional: where it was found |

A ticket can have several pull requests; list branch matches first, then
link matches, without duplicates (same `url`). Return
`{"pull_requests":[]}` when nothing matches. asanamate treats an unknown
`state`, `checks`, or `review` value as empty, so providers can add values
later.

**Exit status.** Non-zero or invalid output fails that command's run.
asanamate keeps that command's previous results, and the other commands'
results are unaffected. The Feed panel's summary shows `pull requests
failed`, as a failed feed does. `asanamate doctor` runs each command and
shows its stderr.

**Timeouts.** `feed.Timeout` (1 minute); the command is killed after that and
the run fails. Output limits are `jsoncmd.DefaultLimits`.

**Example.** `internal/pullrequests/testdata/provider.sh`, a fixture that
answers by branch, used by the protocol tests and quoted in the docs.

## Core

### Package

`internal/pullrequests`:

- `ProtocolVersion = 1`, request and reply types, and
  `Lookup(ctx, command, tickets) ([]PullRequest, error)` over
  `jsoncmd.RunBounded`, for one command.
- `Merge(replies [][]PullRequest) []PullRequest`: in command order, dropping
  repeated URLs.
- Validates each item. It drops items with an unknown `ticket_gid`, a
  non-http(s) `url`, an empty `title`, or an unknown `state`, and blanks
  unknown `checks` and `review` values. It passes `title`, `author`, and
  `ref` through `display.Line`.
- Unit tests for validation, and protocol tests that run the fixture through
  `Lookup`.

### Config

New table `[pull_requests]` with one key, `commands` (default `[]`, off). A
list from the start: changing a released `command` key into a list later
would be a breaking config change. A table, not a top-level key: the concept
will grow (for example, a separate refresh interval). Follow every step of
"Every key change touches" in `AGENTS.md`. `validate` rejects a blank entry
with `pull_requests.commands must not hold a blank command`, like
`feed.commands`. It is not a TUI setting, so it stays out of `SettingKeys`.

### Remotes

Add `repo.Remotes(ctx, path) ([]Remote, error)` (`Remote{Name, URL}`, fetch
URLs, `origin` first, then by name), parsed from `git -C <path> remote -v`.
`repo.Matching` already parses that output; make it use `Remotes` instead of
its own parsing. Read each linked repo's remotes once per refresh, not once
per ticket, inside the lookup command (off the UI goroutine).

### TUI

- **Lookup.** Add `checkPullRequests()` beside `checkGit()`:
  - It uses the same targets, its own sequence number, and a message type.
  - It runs every command concurrently and keeps results per command, so a
    failed command keeps its previous results. The lookup values come from
    `pullrequests.Merge` over them.
  - On reply, store results by ticket GID in a new field. Call
    `applyFilter` only when the results changed, and re-render the reader
    when its ticket's results changed.
  - Keep the previous results on failure.
- **Filter terms.** In `filter`:
  - Add `pr`, `checks`, and `review` to `keys`, `KeyPR`, `KeyChecks`, and
    `KeyReview` constants, and `lookupKeys`.
  - Update the package doc, `filterhelp.go` (both widths; the narrow guide
    has a height test), and README "Filtering".

  The values are:

  | Term | Values | From |
  |---|---|---|
  | `pr:` | `open`, `draft`, `merged`, `closed`, `any`, `none` | every pull request of the ticket |
  | `checks:` | `passing`, `failing`, `pending`, `any`, `none` | open and draft pull requests only |
  | `review:` | `approved`, `changes`, `pending`, `any`, `none` | open and draft pull requests only |

  A merged pull request's old failing checks must not put a ticket in a
  "Checks failing" section, which is why `checks:` and `review:` skip merged
  and closed ones.
- **Reader.** Add a **Pull requests** section to every layout (cards,
  streamlined, markdown), after the details and before the description, only
  when the ticket has pull requests:
  - Each pull request is one target, `pr:<url>`, with one line:
    `#42 open · checks failing · changes requested  Fix login redirect`.
  - `enter` on the target opens its URL with `browser.Open`. It is the same
    target and action in every layout; only the drawing differs.
  - Color the state like the theme colors similar states: failing in the
    error color, pending in the warning color, merged dim.
  - Add the section to the jump menu (`'`).
- **Statusline/Feed.** None in this spec (see Out of scope).

### Built-in groupings

Add these sections to `builtinGroupings`. Confirm with the user first (see
Decisions):

- `state`: **In review** (`pr:open`, `pr:draft`), right after Needs me. A
  ticket with an open pull request is in review even with local work, so the
  order becomes Needs me, In review, Active, Blocked, Ready.
- `now`: **Review feedback** (`review:changes`, `checks:failing`), after
  Agent waiting. Add `review:pending` to **Waiting on others**.

Update the README and the config template examples of both groupings, and
`TestBuiltinGroupingsAreValid` keeps them valid.

### Extensions catalog

Add an entry `pull-requests` (name "Pull requests", benefit "Show each
ticket's pull requests and their checks and reviews"):

- **Capability:** contract `pull-requests`, key `pull_requests.commands`,
  executable `asanamate-pr`.
- **Steps:** install the bundled adapter; install and authenticate `gh`
  (`gh auth login`) and/or `glab` (`glab auth login`); add
  `[pull_requests] commands = ["asanamate-pr"]`, keeping any other
  providers in the list; link the project's repo with `L`.
- **Check:** `asanamate-pr check`.
- **Example:** open a ticket whose branch has a pull request.
- **Remove:** steps that match the other entries.

`asanamate extensions` and `TestCatalog*` tests cover the shape.

## Adapter: `asanamate-pr`

`cmd/asanamate-pr/`, a Go binary with no dependencies beyond the standard
library and the CLIs it calls.

- **Operations.** It reads one request on stdin and answers `lookup`. Any
  other operation exits 1 with `unknown operation` on stderr.
- **Hosts.** Group the request's tickets by repo. Pick each repo's CLI from
  the request's `remotes`, the first remote that matches; the adapter never
  runs git:
  - `gh` for `github.com`, or for a host `gh auth status --hostname <host>`
    accepts.
  - `glab` for `gitlab.com`, or for a host `glab auth status --hostname <host>`
    accepts.
  - Skip repos with neither silently: another provider in
    `pull_requests.commands` may serve them.
- **One call per repo per run.** List recent pull requests once per repo
  and match locally. Never one call per ticket: refreshes run every minute
  by default, and GitHub rate-limits GraphQL.
  - **GitHub:** `gh pr list --repo <owner/repo> --state all --limit 100
    --json number,title,url,state,isDraft,headRefName,body,updatedAt,author,reviewDecision,statusCheckRollup`.
    Map:
    - `isDraft` to `draft`; `MERGED`, `CLOSED`, and `OPEN` to `merged`,
      `closed`, and `open`.
    - `reviewDecision` `APPROVED`, `CHANGES_REQUESTED`, and
      `REVIEW_REQUIRED` to `approved`, `changes`, and `pending`.
    - Roll `statusCheckRollup` up to `failing` if any check failed, else
      `pending` if any is running, else `passing`; empty when there are no
      checks.
  - **GitLab:** `glab mr list --repo <group/project> --all --per-page 100
    --output json` (check the flags against the installed `glab`; older
    versions differ). Map:
    - `opened` (with `draft`) to `open`/`draft`; `merged` and `closed`
      unchanged.
    - `head_pipeline.status` to checks. If the list lacks the pipeline or
      approvals, add at most one `glab api` call per open MR, and only for
      MRs that match a ticket.
    - Set `ref` to `!<iid>`.
- **Matching.**
  - **Branch:** `headRefName` / `source_branch` equals the ticket's
    `branch` exactly.
  - **Link:** the description contains an `app.asana.com` URL with the
    ticket's GID as a whole path segment. Asana uses
    `/0/<project>/<task>`, `/0/<project>/<task>/f`, and
    `/1/<workspace>/project/<p>/task/<task>`.
  - Branch matches first, de-duplicated by URL.
- **Commands.** Run `gh` and `glab` with argument arrays, never
  through a shell, with a per-call timeout under the 1-minute budget. Never
  log tokens. A missing CLI or an expired login on one host fails only
  that host's repos: answer with what the other hosts found and explain on
  stderr. Exit non-zero only when every host failed.
- **`check` subcommand.** Reports which of `gh` and `glab` are installed
  and logged in (`gh auth status`, `glab auth status`), read-only. It exits
  0 when at least one is.
- **`-h` / `help`.** Usage text; the CI smoke test runs it.
- **Tests.**
  - Fake `gh` and `glab` scripts on `PATH` in `testdata/` cover: branch and
    link matching, the state, checks, and review mapping, one list call per
    repo, silently skipped repos on other hosts, partial host failure, and
    unknown operations.
  - No live network in tests.

## Docs

- README:
  - "What it does": a **Pull requests** (optional) bullet.
  - A **Pull requests (optional)** section: setup, what shows where, the
    filter terms, and that merge requests count.
  - Config table row for `pull_requests.commands`.
  - How to add a provider for another host next to `asanamate-pr`.
  - Filtering terms, and the updated grouping examples.
- `docs/extensions.md` section and table row, including "Several
  providers" and a fixture for a non-GitHub host.
- Config template (`internal/setup/setup.go`): a commented
  `[pull_requests]` block.
- What's new: `internal/whatsnew/unreleased/pull-requests.md` under
  `### New`, ending with how to try it.
- `docs/specs/README.md`: mark this spec done.

## Out of scope

- Creating, merging, approving, or commenting on pull requests. asanamate
  only reads them.
- A pull request feed in the Feed panel, list row badges, and a statusline
  count. Each is a small follow-up once the data exists. Record them in
  `docs/deferred-ideas.md` with this reason.
- A disk cache. Results live in memory and refresh with the feeds.
- The `asanamate list` CLI: `pr:`, `checks:`, and `review:` match nothing
  there, like `agent:`. Say so in the README.
- Bitbucket, Gitea, and other hosts in the bundled adapter. They work
  through another provider in `pull_requests.commands`.

## Acceptance

- `pull_requests.commands = ["asanamate-pr"]` with an authenticated `gh` shows a
  ticket's open pull request in the reader in every layout, and `enter`
  opens it in the browser.
- `/pr:open`, `/checks:failing`, and `/review:changes` filter the list, and
  a `[[groupings]]` section using them places tickets.
- With the built-in groupings updated, `b` > `state` shows In review, and
  Now shows Review feedback.
- With two commands, such as `asanamate-pr` and the fixture for another
  host, tickets get pull requests from both, and a URL both return shows
  once.
- A command that exits non-zero keeps its previous results, leaves the other
  commands' results alone, and marks the Feed panel summary; `asanamate
  doctor` shows its stderr.
- Without `pull_requests.commands`, nothing runs and nothing changes.
- `asanamate extensions` lists the entry, and `asanamate extensions check
  pull-requests` runs `asanamate-pr check`.
- Release archives contain `asanamate-pr`, and CI's archive and smoke checks
  cover it.
- `go vet ./...`, staticcheck, and `go test -race ./...` pass.
