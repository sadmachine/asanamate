# 21: Use Harvest directly and guide provider setup

**Depends on the existing time tracking and feed protocols. Independent of
20; update its Harvest catalog entry when both are present. Additive; no
breaking changes.**

## Goal

A user can install the bundled Harvest provider, connect a Harvest account,
choose a task by name, and enable time tracking or today's feed without
Node.js, npm, or hrvst-cli. Existing hrvst-based configurations continue to
work without migration.

## Context

- `cmd/asanamate-hrvst/harvest.go` already calls Harvest API v2 to create
  entries, read ticket totals, identify the current user, and load feeds.
- Only project/task assignment retrieval shells out to `hrvst`; credentials
  are read directly from `~/.hrvst/config.json`.
- Entry creation bypasses the CLI because its create command loses the
  nested `external_reference` linking the entry to Asana.
- The provider already has authenticated HTTP requests, pagination, response
  validation, and tests with fake HTTP servers. Extend those owners.
- `asanamate-hrvst check` currently checks for the CLI and readable saved
  credentials, without contacting Harvest.
- Setup currently requires `hrvst login`, manually finding a numeric task
  ID, and setting `time_tracking.command` with `--task-id`.
- `internal/timetracking`, `internal/feed`, and `docs/extensions.md` define
  generic protocols. Harvest stays an external provider using them.

## Decisions

- Retain the released `asanamate-hrvst` executable name, `hrvst` provider ID,
  and feed source ID. A rename would add migration work without helping
  this goal.
- Use personal access tokens for the first native setup. Do not implement
  OAuth registration, browser callbacks, or token refresh in this task.
- Native secrets come from environment variables, following core's Asana
  token pattern. Do not put tokens in core config, command arguments, logs,
  or saved provider settings.
- Preserve legacy credential-file support as a fallback. It is compatibility
  support, not a required dependency or the recommended new-user setup.
- Keep the current model of one default Harvest task, eligible across the
  user's projects. Setup selects that task by name; this task does not add
  dependent project/task fields to the generic form protocol.

## Requirements

### Direct API access

- Replace `hrvst users project-assignments me --output=json --page=all` with
  authenticated, paginated API v2 requests for the current user's project
  assignments. Fetch every page, validate the response, and preserve active
  project/task filtering and Harvest names.
- Reuse the existing HTTP client and pagination code where they fit. Keep
  Harvest-specific data and credential handling under the provider command;
  extract a small provider-owned file/module if clearer. Do not add a
  single-implementation service interface or a broad Harvest SDK.
- `form`, `log`, `summary`, and `feed` all run with no `hrvst` executable on
  PATH. Remove subprocess execution and CLI JSON compatibility handling
  from assignment retrieval once the HTTP path replaces it.
- Retain Asana project-link suggestions, remembered project/task choices,
  explicit date and positive-hour validation, exact-ticket totals, today's
  totals, pagination, and existing feed behavior.
- Validate submitted project/task eligibility against current assignments
  before creating an entry. A stale or forged selection must not create an
  entry for an inactive or unassigned project/task.

### Native settings and credential resolution

- Add an optional `harvest.toml` beside the main config, respecting the same
  XDG config directory. It belongs to the provider and contains only:

  ```toml
  account_id = "123456"
  task_id = "987654"
  ```

  Both values, when present, must be positive numeric IDs. Reject unknown
  keys with the file path and key in the error. Document missing-file
  behavior and defaults. A feed-only configuration does not require a task.
- Read native credentials from `HARVEST_ACCESS_TOKEN` and
  `HARVEST_ACCOUNT_ID`; a saved `account_id` supplies the latter when the
  account environment variable is absent.
- Credential precedence is explicit:
  1. If any native credential input is supplied (token/account environment
     variables or a saved account ID), require a complete native pair.
     Report incomplete or invalid native credentials; never silently retry
     using another account's legacy credentials.
  2. Otherwise read the existing `~/.hrvst/config.json` pair, without needing
     the CLI installed. Do not edit that file or copy its token elsewhere.
  3. If neither source is available, give native setup instructions.
- Explicit `--task-id` takes precedence over saved `task_id`. Retain every
  currently valid `--task-id` invocation and config command. Missing task ID
  blocks form/log only; summary/feed remain available.
- A malformed native file is an error, not permission to ignore settings or
  fall back. Never combine a native token with a legacy account ID.
- The provider documents its own settings, defaults, validation, and tests.
  No new core config key or setting is necessary; retain the existing
  `[time_tracking]` and `feed.commands` contracts.

### Guided setup

- Add `asanamate-hrvst setup`, an interactive terminal command. Keep protocol
  invocations noninteractive and stdout reserved for their JSON replies.
- Explain the token creation page, required account ID, and environment
  variables. Never ask for a token as a command-line argument, print it, or
  edit shell startup files. If the token is absent, explain how to export
  it and rerun setup. Do not claim a token exists in future shells merely
  because the current process sees it.
- Accept the account ID from native inputs or a prompt, then explicitly
  verify access with read-only API requests. Show authenticated user and
  account ID. Legacy-only users may explicitly choose to retain their saved
  login; that path saves no native account ID or token.
- Ask whether to set up time tracking, feeds, or both. For time tracking,
  retrieve active assignments and show unique tasks by name, ID, and count
  of eligible projects. Let the user choose without knowing a numeric ID.
  Disambiguate duplicate names. Feed-only setup skips task selection.
- Show a summary before saving nonsecret provider settings. Preserve existing
  values unless the user explicitly changes them. Cancellation before save
  leaves files unchanged; validation or network failures never replace
  working settings. Repeated setup with the same values writes nothing.
- Use private permissions, an atomic replacement, a timestamped sibling
  backup before changing an existing file, and a changed-file check before
  saving. Never overwrite an existing backup or an edit made during setup.
- Finish with exact core config snippets for the selected capabilities:

  ```toml
  [time_tracking]
  id = "hrvst"
  command = "asanamate-hrvst"
  ```

  For feeds, show how to add `"asanamate-hrvst"` to existing
  `feed.commands`, preserving other commands. Explain not to duplicate a
  TOML table or replace a custom provider accidentally. Provider setup does
  not write core config; point to `asanamate config` and spec 20's guide.
- Explain that an existing command with `--task-id` keeps using that explicit
  task. Show the edit needed to adopt the newly saved default without making
  that edit automatically.
- Give first-use verification: open a ticket's time form without submitting,
  or check today's feed. Creating an actual time entry is always a separate,
  explicit user action.

### Checks and failures

- Retain `asanamate-hrvst check` as a local-only check: validate credential
  source and settings without requiring hrvst-cli or contacting Harvest.
  Clearly report that remote access is unverified. Task readiness is a
  separate result so a working feed is not failed for lacking a default task.
  Continue to exit successfully when valid credentials exist and no default
  task is configured, preserving the existing check's credential-only use.
- Add `asanamate-hrvst check --online` for explicit, read-only authentication
  and assignment checks. Report account/user identity, task eligibility when
  configured, and actionable failures. Never create an entry during checks.
- Errors distinguish absent/incomplete credentials, rejected authentication,
  inaccessible account, no assignments, ineligible task, rate limiting,
  timeout, network failure, and invalid API response. Do not label an outage
  as a request to replace credentials.
- Bound HTTP requests and response sizes; handle cancellation and invalid or
  looping pagination without returning incomplete totals or project lists.
  Preserve credential confidentiality across redirects and diagnostics.
- Respect rate-limit responses. Do not automatically retry entry creation:
  the server may have created the entry before a timeout or invalid reply.
  Preserve the existing warning not to submit again after uncertain success.
- Keep nested Asana `external_reference` creation and response verification.
  No login, setup, check, or form request may write to Harvest or Asana.

### Onboarding, docs, and compatibility

- Update README install requirements, Optional features, time tracking,
  feeds, troubleshooting, and moving-machine instructions. Remove Node.js,
  npm, and hrvst-cli from the recommended Harvest path.
- Explain native token/account setup, provider-file location, task precedence,
  legacy fallback, credential removal, and how to disable either capability.
- Update the commented core config example to use `asanamate-hrvst` after
  setup, while documenting `--task-id` as a supported explicit override.
  Update template expectations without changing existing configured values.
- When spec 20 is present, update only its Harvest catalog data: dependency
  list, `setup` action, local check, explicit online check, native snippets,
  and per-capability guidance. Reference its shared Time tracking v1 and
  Feeds v1 contracts and documentation; distinguish them from Harvest API
  v2. List `form`/`log`, optional `summary` with daily totals, and `feed` as
  this provider's implemented operations. No Harvest-specific UI branch is
  needed.
- Document that another tracker can replace this provider by implementing
  the same asanamate contract and choosing its own provider ID. Native
  Harvest credentials, task defaults, and API calls are implementation
  details, not requirements for a Clockify or other compatible provider.
- Release archives still include the same provider executable. Retain JSON
  protocol versions, request/reply shapes, provider ID, and existing saved
  choices. Core browsing remains independent of Harvest availability.
- Existing users are not forced to move credentials or alter config. Record
  the additive native setup path in release documentation; no breaking key
  migration is required. Removing legacy support requires a separate decision.

## Out of scope

- OAuth, OS credential stores, automatic token import, token rotation, and
  simultaneous account profiles.
- Timers, editing/deleting entries, billing, reporting, and Harvest admin
  operations.
- A renamed binary/provider, another repository, or a generic HTTP provider.
- Per-project default tasks, a dynamic form protocol, core config rewriting,
  and automatic changes to the user's live config during implementation.

## Acceptance

- With neither Node.js nor hrvst-cli installed, native setup selects a task
  by name, form lists eligible projects, log creates an Asana-linked entry,
  summary returns correct totals, and feed returns the requested day's entries.
- Feed-only setup works without a task ID. Local check performs no network
  request; online check and setup perform no remote writes.
- When spec 20 is present, Harvest details link both implemented contracts
  and the custom-provider guide. Contract metadata does not expose Harvest
  API v2 or its task/credential settings as generic provider requirements.
- Multi-page assignments retain every eligible project. Inactive/stale or
  forged project/task selections are rejected before entry creation.
- Explicit `--task-id` overrides saved defaults. Existing `hrvst` IDs,
  command strings, saved choices, and legacy credential files keep working
  without the CLI executable, including users who make no config changes.
- Partial native credentials, conflicting sources, malformed settings,
  failed pagination, and invalid totals produce actionable failures without
  fallback to another account or partial success.
- Cancellation, repeated setup, concurrent edits, and failed saves preserve
  prior settings and backups. Secrets never appear in stdout, errors,
  generated snippets, provider settings, or logs.
- During implementation, obtain required permission before writing test
  files. Extend existing fake-server and provider-protocol coverage for native
  and legacy authentication, assignments, safe writes, and failure paths;
  run `go test ./...` and `go vet ./...`. This task writes specs only.
- Manually validate fresh native setup and an untouched legacy setup against
  a real account. Create a real time entry only with explicit authorization;
  do not claim live write verification based on local fixtures.

## API references

Verify current endpoint and authentication details against Harvest's official
API v2 documentation during implementation:

- [Authentication](https://help.getharvest.com/api-v2/authentication-api/authentication/authentication/)
- [User project assignments](https://help.getharvest.com/api-v2/users-api/users/project-assignments/)
- [Time entries](https://help.getharvest.com/api-v2/timesheets-api/timesheets/time-entries/)
