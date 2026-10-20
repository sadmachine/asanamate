# 20: Discover and set up optional extensions

**Depends on nothing. Additive; no breaking changes.**

Discovery can ship before specs 16–19. Their conversation entry is planned
until its implementation and compatibility gates pass; this task neither
implements nor bypasses those gates.

## Goal

A new user can find optional extensions, understand what each adds, install its
requirements, configure it, and verify the result without searching the
repository. The experience works the same way for any kind of extension.

An extension is a command or action that adds a capability through one or
more extension contracts. Users can inspect those contracts and implement
their own compatible providers, without modifying core or joining the
catalog. An installed executable, configured feature, and working connection
are different things; the UI must distinguish them.

## Context

- README introduces optional features, lists their requirements, and has
  separate setup sections. Instructions are useful but scattered.
- `internal/setup/setup.go` ends with a brief reference to README. It writes
  inactive example actions and commented provider settings.
- `internal/tui/settings.go` offers display controls and the action builder,
  but no place to discover integrations.
- `internal/tui/keys.go` hides time tracking until configured. Keep that
  behavior in normal ticket help; make discovery available elsewhere.
- Release archives contain `asanamate-hrvst` and `asanamate-ccmux`, but the
  primary README install command extracts only the main executable.
- `doctor` checks agents and feeds, but leaves time tracking and project
  links unchecked. A successful core check does not prove an extension works.
- `docs/extensions.md` documents six existing extension protocols. Keep
  them and the ability to configure arbitrary commands.
- Specs 16–19 on `main` propose conversation history through a separate
  `asana-sessions` package and generic discovery/foreground-open contracts.
  Account for those contracts without treating the proposal as released
  behavior or confusing persisted history with running-agent status.

## Decisions

- Use **Extensions** in the UI, CLI, guides, and catalog. A provider is an
  extension supplying a particular capability; a contract is the interface
  asanamate calls. Do not present addons or plugins as separate concepts.
- Provide a small catalog shipped with asanamate, shared by CLI and TUI.
  Do not introduce a remote registry, plugin loader, or manifest protocol.
- Installation is guided. Bundle adapters with the normal install, provide
  exact instructions for external requirements, and recheck after each
  step. Do not implement a universal package manager or run downloaded
  installation scripts automatically.
- Configuration remains in the files that own it. This task adds no core
  config keys or direct integration toggles to `settings.toml`.
- Provider-specific authentication and setup stay in provider executables.
  The catalog explains or launches an explicitly supported setup command;
  it does not implement Harvest, ccmux, or Hub business logic.

## Requirements

### Entry points

- Add **Extensions** to Settings (`s`). It remains available when every extension is
  off. Use the existing picker, detail/help, search, and navigation patterns.
  Do not add a global shortcut.
- Add these CLI forms and document them in usage:

  ```text
  asanamate extensions                    list extensions and local status
  asanamate extensions <id>               show setup, contracts, and removal instructions
  asanamate extensions check <id>         run the documented local checks
  ```

  Listing and guides work without an Asana token or completed core setup.
  A missing core config is shown as **core setup required**, not a catalog
  failure. Invalid existing config shows its validation error without hiding
  installation guides or proposing destructive repairs.
  Unknown IDs produce an actionable error and the available IDs. A failed
  requested check exits nonzero; an unperformed remote check is reported as
  unverified, not successful.
- End `asanamate setup` with a short invitation to run `asanamate extensions`
  or open Settings > Extensions. Include the same entry point in README Install
  and Setup. Do not interrupt first-run Asana setup with mandatory extension
  prompts.

### Shared catalog

- Give entries a stable ID, user-facing name, benefit, implemented contracts,
  availability, authoritative guide URL, install steps for supported
  platforms, configuration examples, verification steps, and disable/remove
  instructions. List external requirements separately from bundled adapters.
- Start with Harvest (`harvest`), ccmux (`ccmux`), and imarc Hub (`hub`). A
  single Harvest entry covers time tracking and feeds. Label Hub as private
  and requiring imarc access before sending users to its repository.
- Include Conversation history (`asana-sessions`) with the availability and
  separate setup paths specified below. Do not imply that it ships in the
  core archive or is usable before the relevant feature gates pass.
- Include an **Actions and custom commands** entry that opens the existing
  action builder and links to instructions for all supported extension points.
  Custom agent, repo, project-link, tracker, and feed commands remain valid
  without registering in the catalog, as do custom conversation sources and
  openers once their contracts are implemented.
- Core/terminal features such as clipboard support and inline images are
  not extensions. Keep their existing Optional features documentation.
- Store catalog content and known command descriptions in one small package,
  following repository conventions. CLI and TUI render the same content.
  Adding an entry must not require changing picker or command dispatch logic.
  Use ordinary data and functions; do not build a workflow engine.
- Do not maintain a second enabled flag, installation database, or credential
  store. Derive status from executable availability and current config.

### List and detail UX

- Each row shows name, one-line benefit, and concise status. Never hide an
  entry because it is unavailable or disabled.
- The detail view follows this order:
  1. What it adds, including each independently configurable capability.
  2. Implemented contracts, with links and **Build your own extension**.
  3. Requirements and current local status.
  4. Numbered installation, authentication, and configuration steps.
  5. Checks and a first-use example that demonstrates success.
  6. Disable, uninstall, and help links.
- Distinguish **adapter missing**, **dependency missing**, **not configured**,
  **configured**, **local check passed**, **connection unverified**, and
  **check failed**. Show status per capability where an extension supplies more
  than one. One missing feed setting must not make working time tracking
  appear broken.
- Show the next unresolved step and exact commands or TOML needed. Support
  copying commands/snippets and opening guide URLs through existing helpers.
  If clipboard or browser opening fails, leave the text/URL visible.
- Offer **Recheck** and, where supported, **Run provider setup**. The latter
  displays the command and resolved executable path, then suspends the TUI
  using the existing foreground process pattern. Provider prompts retain
  control over their own writes. Cancellation returns to extension details.
- Display configuration snippets and the actual config path. Users can open
  the existing config editor, or edit the file themselves. Do not overwrite
  custom providers or merge snippets into files automatically in this task.
  Explain that extension config edits require restarting asanamate; distinguish
  saved config from the current session's active config.
- Content wraps or scrolls in narrow terminals, works without Nerd Fonts or
  color, and leaves navigation and next actions visible.

### Contract information and documentation

- Each extension's CLI and TUI details name every asanamate contract it
  implements. One executable can implement several contracts; enabling or
  replacing one capability does not automatically enable or replace others.
- Keep a shared description per contract, referenced by catalog entries.
  Include its stable ID, display name, purpose, transport, protocol version
  when one exists, invocation/configuration location, and documentation
  anchor. Reuse existing protocol-version constants rather than maintaining
  another version number. These six contracts already exist; conversation
  contracts are covered separately below:

  | Contract ID | Contract | Transport | Version | Configuration | Documentation |
  |---|---|---|---|---|---|
  | `actions` | Actions | environment variables and ticket files; exit status | unversioned | `actions/*.toml` | [Actions](../extensions.md#actions) |
  | `agent-sources` | Agent sources | tab-separated lines on stdout | unversioned | `agents.command`, `agents.states` | [Agent sources](../extensions.md#agent-sources) |
  | `repo-sources` | Repo sources | repo paths, one per stdout line | unversioned | `repo_source.command` | [Repo sources](../extensions.md#repo-sources) |
  | `time-tracking` | Time tracking | JSON requests on stdin; replies/exit status | 1 | `time_tracking.id`, `time_tracking.command` | [Time tracking](../extensions.md#time-tracking) |
  | `project-links` | Project links | JSON requests on stdin; JSON replies | 1 | `project_links.command` | [Project links](../extensions.md#project-links) |
  | `feeds` | Feeds | JSON requests on stdin; JSON replies | 1 | `feed.commands` | [Feeds](../extensions.md#feeds) |

- Display the asanamate contract version separately from the extension's
  release and the external service's API version. Harvest API v2, for
  example, does not mean time tracking protocol v2. Do not invent version
  numbers for unversioned contracts.
- For JSON contracts, show operation names and whether they are required or
  optional. Time tracking requires `form` and `log`; `summary` is optional,
  and daily totals within `summary` are optional. Feeds uses `feed`; project
  links uses `links`. Link to the exact request/reply, compatibility, timeout,
  and error rules rather than duplicating the full specification in catalog
  entries. Non-JSON contracts show their input/output and lifecycle summary.
- Harvest references Time tracking and Feeds; ccmux references Agent sources;
  Hub references Project links. Mark documented optional capabilities per
  implementation, without claiming that every provider of a contract supports
  them. Catalog metadata describes known implementations; it is not a runtime
  handshake, proof of conformance, or reason to execute an unknown command.
- Both interfaces offer **Read contract documentation** and **Build your own
  extension** for each contract. The custom-command entry exposes all
  contracts even when no provider is installed. For configured custom
  commands, show the contract implied by their configuration slot without
  guessing the implementation's name or optional capabilities.
- `docs/extensions.md` remains the authoritative contract documentation.
  Link to its matching section in the installed release's GitHub tag when
  known; development builds may use an explicitly labeled default-branch
  fallback. Always print the URL if opening it fails. Essential contract
  summaries remain readable offline, and release archives include this
  document alongside README so users can retain the full reference.
- Add an **Implementing an extension** guide to `docs/extensions.md`. It
  explains choosing a contract, reading its reference example, implementing
  required operations, handling untrusted input and credentials, reserving
  protocol stdout, testing errors, and installing/configuring a custom
  command. Keep fixtures linked as examples; never present a no-op log stub
  as a provider that successfully writes time entries.
- State that conforming to a contract allows a new provider to fill that
  configuration slot without core changes or catalog registration. Separate
  asanamate's contract from the provider's own remote API/authentication;
  implementing a Clockify API client alone does not satisfy the contract.

### Conversation history coordination

- Specs `16-conversation-history.md` and `19-conversation-ui.md` own core's
  conversation contracts. Specs `17-conversation-companion.md` and
  `18-conversation-plugins.md` own the standalone tool, native-provider
  feasibility, optional linking plugins, installation, and independent
  releases. Use their final contracts, tested compatibility floors, and
  package-owned instructions; do not duplicate their implementation here.
- The catalog models two contracts for the one conversation extension:

  | Contract ID | Contract | Transport | Proposed version | Configuration |
  |---|---|---|---|---|
  | `conversation-discovery` | Conversation discovery | JSON `list` request on stdin; records and diagnostics on stdout | 1 | `conversations.command` |
  | `conversation-open` | Conversation open | private selected-record JSON snapshot via `ASANAMATE_CONVERSATION_JSON`; terminal stdin/stdout/stderr | 1 | `conversations.open` |

  Discovery/open documentation links point to the separate sections published
  in `docs/extensions.md` by specs 16/19. Before implementation, label these
  contracts **proposed** and link to the available design documentation,
  not missing API sections. Once implemented, link to the supported
  release's actual contract sections and reuse its protocol constants.
- Conversation history stays separate from Agent sources. Persisted sessions
  can exist without running agents. Discovering a session does not prove it
  is inactive or that it can safely be resumed; a successful lookup/check
  must not be labeled proof of successful terminal opening.
- Show the default adapter configuration, including the fact that both
  commands are required together:

  ```toml
  [conversations]
  command = "asana-sessions asanamate list"
  open = "asana-sessions asanamate open"
  ```

  Show a custom source/opener's applicable contracts from these slots without
  assuming it is `asana-sessions`. A third-party implementation need not
  implement the companion's public CLI, its native SDK/RPC internals, or its
  metadata-linking plugins to satisfy core's source/open contracts.
- Distinguish four independent facts in the conversation entry: the helper
  is installed, this core build supports its adapter contracts, discovery/open
  commands are configured, and optional automatic-linking plugins are ready.
  If the standalone tool ships first, explain that it works independently
  while core integration is not available in this asanamate version. Do not
  present core configuration as usable before core supports it.
  Helper availability follows spec 17; core history integration follows
  16/17/19; optional hook automation follows 17/18. Unfinished hook support
  must not keep a completed history integration labeled planned.
- Use the package's published/local-tarball install paths and verified
  Node/SDK/provider requirements. It is a separately installed extension,
  not a bundled Go executable. Installing core or opening Extensions must
  never install Node, the companion, SDKs, or native provider plugins.
- Keep historical discovery and terminal opening usable without automatic
  linking. Label `asana-sessions setup` as **Set up automatic linking
  (optional)**, not as a prerequisite for history. It may register native
  Claude/Codex plugins and require trust/restart steps; it does not configure
  core. Those are provider plugins, not an additional asanamate plugin API.
- Expose `asana-sessions doctor` as an explicit, documented nonmutating check.
  Distinguish missing helper/runtime, unsupported versions, inaccessible
  history, partial results, and unused-provider absence. Plugin registration,
  enablement, trust, and compatibility have separate statuses; untrusted or
  disabled optional hooks do not mark working history as unavailable.
- Discovery, setup verification, and checks never link/rename a session,
  resume a provider, start a model turn, unarchive history, read transcript
  bodies, or grant hook trust. Only a deliberate conversation-open action
  may resume a session under specs 16–19's validation and activity rules.
- Show separate removal paths: disable core integration; remove optional
  linking plugins; uninstall the helper. Preserve existing native histories
  and ticket markers. Explain that removing only plugins leaves lookup and
  opening available, while removing the helper requires disabling its core
  commands. Do not change unrelated agent-source configuration.
- Keep standalone package version, native plugin versions, core version, and
  asanamate protocol versions distinct. Use supported compatibility ranges
  from the companion's feasibility/release gates, never matching version
  numbers or an invented minimum runtime.

### Replacing a provider

- Include a concrete, explicitly hypothetical example: a user implements
  `asanamate-clockify` against Time tracking and swaps it for Harvest:

  ```toml
  [time_tracking]
  id = "clockify"
  command = "asanamate-clockify"
  ```

  The executable name is a convention, not a requirement. It must be on PATH
  or named by a quoted path, implement the required operations, and have its
  own valid credentials/settings. It need not implement Harvest's API,
  command flags, credential storage, or default-task model.
- Use a different provider ID so remembered choices stay separate. Keep
  Harvest's saved choices and credentials intact so reverting is possible.
- Changing time tracking does not change `feed.commands`. A Clockify feed
  requires implementing Feeds and replacing only the intended feed entry,
  preserving other sources. If only time tracking is implemented, do not
  suggest adding the command as a feed. Tracker-specific project-link types,
  such as `harvest_project`, are not automatically translated to Clockify.
- Provide verification steps that exercise form loading and explicitly
  unsupported optional features without creating a time entry. Any real
  write remains a separate explicit action; installation and discovery never
  perform a test write. Existing generic protocol fixtures show how authors
  can verify conformance locally without real credentials.

### Installation and setup guidance

- Update normal archive installation to extract and install all bundled
  adapter executables alongside asanamate. Installing an adapter does not
  enable it, authenticate it, or install its external dependencies.
- Keep source installation equivalent, using the existing `install.sh` or
  documented `go install ./cmd/...` plus theme installation. Check archives,
  PATH guidance, macOS quarantine instructions, and uninstall instructions
  remain consistent. Publishing Homebrew is not a prerequisite.
- Catalog instructions identify the install method, supported OS, dependency
  versions, authoritative source, and final executable location. Never
  suggest fetching a different asanamate release just to obtain its adapter.
- External package installation remains a visible command for the user to
  run. Do not install Node.js, package managers, tools, hooks, or private
  repositories merely by opening the screen or selecting an extension.
- For Harvest, use the existing CLI-based setup until spec 21 lands. Spec 21
  updates that entry to native setup; this task must ship independently.

### Checks and trust boundaries

- Listing and opening details perform local inspection only. Do not launch
  commands, contact extension services, or prompt for secrets during discovery.
- Explicit checks may run only cataloged, documented read-only checks.
  Invoke known executable/argument vectors directly, with bounded execution
  and output. A check must never create a time entry or mutate remote data.
- Do not infer extension identity by searching arbitrary shell command strings,
  execute an unknown command to discover its behavior, or send credentials
  to a URL supplied by catalog output. Recognize only known command forms;
  otherwise show **custom command** and its documented verification path.
- Extend `doctor` to reuse these checks for recognized configured extensions and
  report what was checked. Preserve current behavior for custom commands.
  Optional failures remain warnings and do not block browsing. If no safe
  check exists, say **unverified** and supply the next verification step.
- Authentication checks distinguish local credential availability from
  live authentication. A local success is never labeled connected. Live
  checks, when supported, require an explicit user action and identify the
  account/service being contacted.
- Never log tokens or credential file contents. Clean provider diagnostics
  before rendering them and keep arbitrary terminal controls out of output.

### Documentation and compatibility

- Add a concise README Extensions section linking to the shared CLI/TUI paths
  and existing detailed guides. Keep `docs/extensions.md` as the protocol
  contract, with links back to extension onboarding and the implementation
  guide. Describe bundled commands as replaceable implementations of those
  contracts, not privileged built-in providers.
- Preserve all released config keys, commands, protocol versions, custom
  shell commands, and saved provider IDs. No config migration is required.
- Add no integration-specific authentication or network client to core.

## Out of scope

- Remote catalogs, automatic extension discovery from the network, third-party
  manifest loading, dependency resolution, and automatic updates.
- A general installer, automatic privilege escalation, or package-manager
  abstraction.
- Automatic core config changes, integration toggles, and hot reloading.
- Replacing providers or implementing their authentication in the TUI.
- Implementing conversation history, changing specs 16–19's gated contracts,
  or managing native-provider plugins outside their supported setup flow.

## Acceptance

- A fresh user can find Harvest, ccmux, and Hub from Settings and CLI with
  every integration off; each explains benefit, access, and next steps.
- Normal release installation puts bundled adapters on PATH without enabling
  them. Archive and source instructions reach equivalent results.
- Each catalog entry has a complete install, configure, verify, and remove
  path. Missing prerequisites and failed browser/clipboard access have clear
  fallbacks.
- Harvest time tracking and feed configuration are reported independently.
- Harvest details name Time tracking v1 and Feeds v1 with their separate
  config locations and matching documentation links. ccmux and Hub show
  their own contracts. Unversioned contracts are not labeled v1.
- Each contract has a visible implementation-guide path in CLI and TUI,
  including offline summaries and a URL fallback. Shipped documentation and
  examples agree with the real protocol constants and fixture behavior.
- Conversation history appears with honest planned/available status and
  separate discovery/open contract information. Once implemented, its
  documentation links resolve to both released contract sections.
- The standalone companion remains installable without core. Helper install,
  core integration, and optional automatic-linking setup are separate steps;
  disabled/untrusted hooks do not block manually linked history.
- Conversation checks make no model requests or metadata writes and never
  resume a session. A custom source/opener can use the documented contracts
  without registering in the catalog or depending on the companion.
- A minimal custom time provider using the generic contract can replace
  Harvest without core changes or catalog registration. A different provider
  ID isolates saved choices; feed replacement remains independent. The
  Clockify example is explicitly illustrative, not an available product.
- A custom provider remains functional and is never mistaken for a known
  provider or executed during discovery.
- CLI and TUI use the same catalog and check results. Checks distinguish
  local readiness, unverified remote access, and failure.
- Cancelling provider setup leaves core config unchanged. Invalid edits are
  reported by the existing config validation path.
- Relevant existing CLI, TUI, config, and archive checks pass. During
  implementation, add focused coverage for the acceptance cases after
  obtaining the required permission to write test files; run `go test ./...`
  and `go vet ./...`. No test files are part of this specification task.
- Manually walk one bundled extension, one private/unavailable extension, and one
  custom command in a clean temporary config on macOS and Linux. Verify
  narrow-terminal navigation and disabled-feature discovery.
