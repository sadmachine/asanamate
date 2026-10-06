# asanamate Portability Implementation Plan

Date: 2026-10-05
Status: P0 in progress; P1 deferred (see Deferred work)

## Goal and governing specification

Make a fresh macOS or Linux system ready to browse Asana with only the core
prerequisites. Make every additional integration explicit, documented, and
recoverable when its environment changes.

Follow the [portability specification](../specs/2026-10-05-portability-release.md).
This plan supplements the original implementation plan; it does not inherit
its subagent requirement or stale scaffolding instructions. Work stays inline
unless the user explicitly requests delegation.

## Execution rules

- Trace the current behavior and review similar files before each implementation
  task. The audit is a baseline, not permission to overwrite later changes.
- Ask before writing or committing test files, as required by the repository's
  user instructions. Planned proof below identifies needed coverage; it does
  not authorize those file changes in advance.
- Prefix external shell commands with `rtk`. Use the caveman proxy if invoking
  any model provider executable.
- Keep tasks independently reviewable. No staging, commits, release tags, or
  live credential/config changes are authorized by this plan alone.
- Run existing focused tests for each changed owner package, then the full suite
  and vet at the release gate. Add new coverage only after permission.
- Respect released config and command contracts. Inspect release history before
  making a breaking change; request approval when required by `AGENTS.md`.
- After any implementation changes local config shape or action files, finish
  the implementation and ask whether to migrate the user's local config.

## Phase 1 Establish the core onboarding contract

### Task 1 Complete install and dependency documentation

Priority: P0. Dependencies: none; finalize examples alongside later tasks.

Primary files: `README.md`, `.goreleaser.yaml`, `.github/workflows/release.yml`.

- [ ] State the platform/architecture scope.
- [ ] Write complete macOS/Linux archive installation steps with `curl`,
  checksum verification, user-writable placement, and `PATH`. Give the
  `xattr -d com.apple.quarantine` fallback for browser downloads on macOS.
- [ ] Document source installation prerequisites from `go.mod` and the Go binary
  output directory. Explain upgrade and uninstall without deleting user data.
- [ ] Publish an optional-feature matrix with authoritative install links,
  authentication, compatibility, activation, verification, and disable steps.
- [ ] Explain token persistence and tmux/launcher environment propagation without
  storing tokens in config or silently modifying shell files.
- [ ] Record tested OS environments and tool versions. Avoid unsupported claims.

Done when someone can follow each documented install route through
`asanamate version`, and every shipped integration has a complete setup path.

### Task 2 Make repo setup skippable and pager activation explicit

Priority: P0. Dependencies: Task 1's baseline contract.

Primary files: `internal/setup/setup.go`, `internal/config/config.go`,
`cmd/asanamate/main.go`, `README.md`; existing setup/config tests after permission.

- [ ] Add a clear skip path to the repo-directory prompt. Preserve workspace and
  token validation and existing config overwrite confirmation.
- [ ] Verify empty repo source already supports manual path entry; reuse that
  behavior so later repo actions remain possible without first-run discovery.
- [ ] Keep the pager action enabled. Setup warns when `$PAGER` (first word),
  else `less`, is not on `PATH`.
- [ ] Explain how to configure repo discovery later.
- [ ] Keep agent and Harvest integrations off, and keep tool-specific examples
  inactive until explicitly enabled.

Proof: setup with no repo directory and no optional tools produces loadable
config/actions; browsing works; declining overwrite preserves files; rerunning
setup preserves existing actions.

## Phase 2 Remove portability assumptions in owned features

### Task 3 Implement native default repo discovery

Priority: P0 (pulled forward; it rewrites the same setup template line as Task 2).
Dependencies: Task 2.

Primary files: `internal/repo/repo.go`, `internal/setup/setup.go`,
`cmd/asanamate/main.go`, `README.md`; repo/setup tests after permission.

- [ ] Implement direct-child discovery using the Go standard library in the repo
  package; recognize `.git` directories and worktree `.git` files.
- [ ] Add `repo_source.root`, a directory whose direct children are listed in
  process. Setup writes it (keeping `~`) instead of a generated `find` command.
  `repo_source.command` takes precedence when set.
- [ ] Remove default dependence on `find` and `dirname`, and replace the current
  apostrophe rejection with TOML encoding.
- [ ] Preserve custom source commands and manual entry. Report unreadable roots
  and newline-containing paths explicitly; do not silently return corrupt paths.
- [ ] Distinguish missing Git from invalid repositories in repo validation.
- [ ] Document updating old generated commands without overwriting custom ones.

Proof: direct-child repositories and worktrees are found; nested grandchildren
are not scanned; spaces, apostrophes, Unicode, and shell metacharacters stay
literal; custom commands still work; missing Git has a clear message.

### Task 4 Make Harvest task behavior account independent

Priority: P0. Dependencies: none; may follow Task 2.

Primary files: `internal/timetracking/hrvst.go`, `README.md`; provider tests after
permission.

- [ ] Confirm the supported `hrvst` version, assignment response shape, and
  credential location/override behavior from authoritative documentation.
- [ ] Resolve the configured task's real name and assigned projects instead of
  displaying Engineering for every account.
- [ ] Reject an unavailable task/project combination before entry creation.
- [ ] Replace author-specific README wording and provide installation, login,
  task-selection, verification, and disable instructions.
- [ ] Preserve saved choices, Asana external reference, configured IDs, and
  protection against duplicate entries after ambiguous failures.

Proof: an account whose task is not Engineering shows its actual name; invalid
assignments cannot create an entry; successful entries retain the Asana link.
Use fixtures for routine proof and explicit live authorization for any time entry.

### Task 5 Repair relocated or partial Codex hooks

Priority: P1. Dependencies: stable installed binary path from Task 1.

Primary files: `internal/setup/hook.go`, `cmd/asanamate/main.go`, `README.md`;
hook tests after permission.

- [ ] Inspect current hook matching and distinguish current complete installation
  from stale paths or missing event registrations.
- [ ] Offer repair through `asanamate setup hooks`, keeping consent explicit.
- [ ] Preserve unrelated hook groups, back up modified files, and avoid duplicate
  registrations on repeated install/repair.
- [ ] Document binary relocation, trust/feature requirements, repair, and removal.

Proof: repeated installation is idempotent; relocated executable and partial
events repair correctly; unrelated hooks survive; declining repair changes no
files; backups contain the original content. Use temporary Codex directories.

## Phase 3 Make missing dependencies understandable

### Task 6 Extend doctor with core readiness and feature diagnostics

Priority: P0. Dependencies: Tasks 2 and 4; integrate Tasks 3 and 5 as available.

Primary files: `cmd/asanamate/main.go` and existing owner packages where their
validation belongs; `README.md`; focused tests after permission.

- [ ] Preserve existing task/agent linkage diagnostics and their documented
  read-only source invocation.
- [ ] Add no-ticket readiness reporting for config/actions, resolved locations,
  token presence and bounded authentication, and known enabled integrations.
- [ ] Classify core failures, optional warnings, disabled features, and checks
  that cannot be verified safely. Define and document exit status behavior.
- [ ] Check known executables/capabilities without executing actions, time logs,
  arbitrary provider probes, installers, or configuration writes.
- [ ] Report Git absence distinctly and connect failures to concrete recovery
  instructions. Redact credentials from all errors.
- [ ] Reuse package validation; avoid a generic plugin registry or shell parser.

Proof: missing/invalid token and bad config are core failures; missing optional
tools report feature-local problems; disabled tools are not required; diagnostics
do not write files or mutate remote data; existing ticket diagnostics still work.

### Task 7 Improve optional feature failure messages and relocation guide

Priority: P1. Dependencies: Task 6.

Primary files: `internal/browser/browser.go`, `internal/tui/inputbox.go`,
`internal/tui/keys.go`, existing repo/link flows, `README.md`; tests after permission.

- [ ] Verify Linux `Ctrl+V` behavior with missing clipboard utilities and display
  actionable feedback while retaining terminal paste and text editing.
- [ ] Preserve OSC 52 link copying and document that a sent sequence is not proof
  the clipboard changed.
- [ ] On browser failure, retain a visible/copyable URL and explain Linux desktop
  prerequisites. Keep headless browsing functional.
- [ ] Document graphics, tmux passthrough, fonts, and optional motion tools.
- [ ] Add a moving-systems guide covering config, state, private exports/logs,
  authentication, repo reselection, custom action dependencies, and hook repair.
- [ ] Audit executable defaults and user-facing documentation for personal paths,
  real workspace/task IDs, organization labels, and assumptions about installed
  tools. Historical examples must remain clearly illustrative.

Proof: each missing optional dependency affects only its feature; users can
continue browsing, editing input, and recovering links using documented steps.

## Phase 4 Prove the packaged release

### Task 8 Add platform and fresh-environment verification

Priority: P0. Dependencies: Tasks 1 through 7.

Primary files: `.github/workflows/ci.yml`, `.github/workflows/release.yml`,
`.goreleaser.yaml`; new smoke/test files only after permission.

- [ ] Run existing tests and vet on macOS and Linux; build all four target binaries
  with release settings. Keep expensive integration checks separate from routine
  package tests.
- [ ] Smoke-test archives, checksums, executable extraction, version, and help.
- [ ] Exercise setup/config/state against temporary homes and alternate XDG paths,
  with a minimal `PATH` that excludes optional tools. Do not use personal config.
- [ ] Use local API fixtures for routine automated authentication/setup proof.
  Perform real Asana authentication and ticket browsing separately with a
  designated account; do not embed production tokens in fixtures or logs.
- [ ] Verify default setup without repos/pager/Git and optional-feature failure
  behavior, including SSH/headless Linux and tmux capability differences.
- [ ] Run native runtime checks for each target where infrastructure is available.
  Record any remaining architecture gap as a release decision, not a passing test.
- [ ] Run `rtk go test ./...`, `rtk go vet ./...`, and release configuration checks
  appropriate to the installed GoReleaser version.

Done when every release acceptance condition in the spec has current evidence.
If a required condition fails, keep the release blocked rather than expanding
scope into unrelated features.

## P0 progress (2026-10-05)

Implemented on branch `forlorn-mantis`, uncommitted:

- Tasks 2 and 3: setup accepts `-` to skip the repo directory; native
  `repo_source.root` replaces the generated `find` command; setup warns when
  the pager is missing; `repo.Resolve` reports missing Git distinctly.
- Task 4: the Harvest form lists only projects where the configured task is
  active, under the task's real name. Verified against hrvst 3.0.0: its JSON
  output passes Harvest's `task_assignments` through, and it keeps credentials
  at `~/.hrvst/config.json` with no override. Harvest's API rejects other
  project/task pairs at creation, so no extra pre-check was added.
- Task 6: `doctor` without a gid reports config, actions, state, Asana
  token/workspace (core, nonzero exit), and Git, repo source, pager, editor,
  browser, time tracking, and agents (warnings). Agent source failures no
  longer abort it. Custom commands are reported as unchecked, not run.
- Task 1: README install (curl archive with checksum, `PATH`, macOS quarantine
  fallback, Homebrew, source), upgrade/uninstall, token persistence and tmux,
  optional-feature table, and troubleshooting.
- Task 8: CI tests and vets on Ubuntu and macOS, builds a GoReleaser snapshot,
  checks checksums and archive contents, and runs the linux/amd64 and
  darwin/arm64 binaries with a fresh home, alternate XDG paths, and a minimal
  `PATH`. A local snapshot build passed the same archive checks.

Remaining before release (tracked with the deferred work): darwin/amd64 and
linux/arm64 are cross-built only, with no native run; tested OS versions and
live Asana evidence are recorded at release time; CI has not run yet.

## Deferred work (P1)

Not part of the current P0 pass. Pick up in this order before publishing.

- Task 5: repair relocated or partial Codex hooks.
- Task 7: optional feature failure messages, the moving-systems guide, and the
  personal-reference audit.
- Release publishing: make the repository public, create the
  `sadmachine/homebrew-tap` repository and `HOMEBREW_TAP_TOKEN` secret, verify
  published artifacts, test Homebrew through the real tap, and recheck
  Homebrew's unsigned-cask policy.
- Review follow-ups from the P0 code review: a fuller optional-feature table
  (install links, activation, verification, and disable steps per feature);
  running setup against a local Asana API fixture in CI; replacing the two
  string arguments of `repo.Candidates` with `config.RepoSource`; detecting
  the pager action and the hrvst provider without matching command text;
  reporting unreadable child directories in `repo.List`.
- Live release evidence: the table below is filled against the final candidate
  at release time.

## Release evidence and handoff

Fill this record against the final candidate, not intermediate builds.

| Check | Required evidence | Result |
|---|---|---|
| Candidate | Commit, intended tag, build settings | Pending |
| macOS | OS versions, architectures, archive/Homebrew install results | Pending |
| Linux | Distributions, architectures, archive and headless results | Pending |
| Minimal setup | No optional tools; temporary home/XDG; valid config/actions | Pending |
| Live Asana | Authentication, My Tasks, project, ticket reading | Pending |
| Optional integrations | Tested versions, activation, missing-tool behavior | Pending |
| Relocation | Repo reselection, hook repair, unrelated settings retained | Pending |
| Automated checks | Tests, vet, cross builds, archive/checksum verification | Pending |
| Compatibility | Existing config/actions and public contracts preserved | Pending |
| Documentation | Install commands and dependency instructions followed successfully | Pending |

- [ ] Review remaining gaps against the release gate; record explicit decisions.
- [ ] If local config/action shape changed, ask whether to migrate local files.
  On approval, create `.bak` siblings, preserve user values, and run the branch's
  `asanamate doctor` until clean, as required by `AGENTS.md`.
- [ ] Request staging/commit and release authorization separately when needed.
- [ ] Stop once the specification is satisfied. Track additional package formats,
  Windows support, or convenience installers as separate future work.
