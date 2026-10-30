# AGENTS.md

## Config keys

The config is TOML, decoded into the structs in `internal/config/config.go`.

- **Shape:** before adding a key, judge whether its concept will grow more
  settings. A growing concept gets its own sub-table, even with one key today
  (`[list.header]` holds `style`, `spacing`, `color`). A standalone setting
  stays a snake_case key in its parent table (`list.layout`). When unsure,
  ask the user.
- **Every key change touches:** the struct field and its doc comment,
  `Default()`, `validate()` (errors name the dotted key, such as
  `list.header.style`), the template in `internal/setup/setup.go`, the README
  config table, and the config tests. A key the TUI can change belongs in
  `config.SettingKeys` and `settings.toml`, never in state; update the
  settings template and README settings table too.
- **Released keys are a public contract:** `Load` rejects unknown keys, and
  `asanamate config update` (`Merge` in `internal/setup/update.go`) fails on
  keys the template lacks, so renaming, moving, or removing a released key, or
  narrowing its accepted values, breaks existing configs. Before the first
  release, change keys freely. After it, add keys and values rather than
  changing them, and make a breaking change only with the user's approval.
- **Approved breaking changes:** register each change in the ordered config
  update pipeline so `PendingUpdates` reports it and `Merge` applies it; the
  TUI startup prompt then covers it. Mark the commit breaking (`feat!:` plus a
  `BREAKING CHANGE:` footer naming the old and new keys), add the upgrade steps under `## Unreleased` in
  `docs/migrations.md` (it becomes the release notes' opening section; use
  absolute links), and tell the user the next release tag needs a breaking
  version bump (see Version bumps).

## Pull requests

Every change reaches `main` through a GitHub pull request, including the
user's own branches. Release commits are the only direct pushes to `main`
(see Version bumps and the standalone release gates).

To land a branch when the user asks:

1. Push it and open a PR with `gh pr create`. The title is a Conventional
   Commits subject; the body states the problem, the change, and how it was
   verified, and links the Asana task when there is one.
2. Wait until `gh pr checks <n> --watch` shows every check passing on the
   PR head. Any new push to the branch restarts the wait.
3. Merge with a GitHub merge commit, which keeps each commit and its
   authorship. Its subject is the Conventional Commits subject plus the PR
   number:
   `gh pr merge <n> --merge --delete-branch --subject "fix(tui): list accessible projects in the add-project picker (#2)"`.
   That subject decides the PR's bump when its own commits are not
   conventional.
4. Update local `main` with `git pull --ff-only origin main`, then follow
   "Version bumps".

Contributor PRs take the same steps 2 to 4, after this review:

- Review the full diff against the PR description and repository conventions
  before approving contributor CI workflows.
- For bug fixes, verify that the regression test fails without the fix and
  passes with it.
- Check compatibility with pending local changes. Adapt callers and tests to
  config or API changes while preserving the PR's intended behavior.

Before a release, check open PRs and identify relevant fixes for inclusion.

## Commits

- Subjects follow Conventional Commits: `type(scope): summary`, lowercase,
  imperative. The type sets the release bump (see Version bumps), so pick it
  by what users get: `feat` for new behavior, `fix` for a correction. The
  scope is the area changed, usually a package or command (`tui`, `config`,
  `filter`, `hrvst`, `asana-sessions`).
- One logical change per commit, with its tests and docs in the same commit.
- Breaking changes follow "Approved breaking changes" under Config keys.

## Deferred ideas

`docs/deferred-ideas.md` records improvements set aside with their reasons.
When a change implements an entry, or makes any of its text untrue (such as
its **Now** description of the code), update or delete that entry in the
same commit.

## Version bumps

Core asanamate releases come only from `v*` tags; there is no core version file.
The independent companion uses `asana-sessions/vX.Y.Z` tags and its package
version. Follow [standalone release gates](packages/asana-sessions/RELEASING.md)
for those tags; they never release core or trigger hub tagging. Everything on
`main` since the latest tag (`git describe --tags --match 'v*' --abbrev=0 main`) ships in
the next release, so the pending bump is the largest one any of those commits
needs:

| Change | Before `v1.0.0` | From `v1.0.0` |
|---|---|---|
| Breaking (`!` or `BREAKING CHANGE:`; see Config keys) | minor | major |
| `feat` | patch | minor |
| `fix`, `perf`, or another change users see | patch | patch |
| `docs`, `test`, `ci`, `chore`, `refactor` only | none | none |

A `v2` or later tag also requires the module path in `go.mod` to end in `/v2`.

After a PR merges and local `main` is updated, read `git log --oneline <tag>..main`
and tell the user the pending bump and the version it gives, such as "patch:
v0.2.0 to v0.2.1". The user often holds several merges for one release, so
recommend a tag; tag only when the user asks. Say whether this merge raised
the pending bump; a merge that adds only `none` commits, or no more than the
bump already pending, needs no new recommendation beyond naming it.

When the user asks for a release, on local `main`:

1. In `docs/migrations.md`, rename `## Unreleased` to the tag's `## vX.Y.Z`;
   the release workflow copies only the section whose heading matches the tag
   exactly. Delete the heading instead if nothing is under it.
2. Commit it as `docs(migrations): release vX.Y.Z`.
3. Push `main` without a tag: `git push origin main`.
4. Wait for the `ci` workflow on that exact `main` commit to finish with
   every job successful. All tests must pass on every environment in
   `.github/workflows/ci.yml`, including archive checks and smoke tests.
   Local checks alone are insufficient. Failed, pending, skipped, or cancelled
   jobs block tagging. Any new commit requires a new successful CI run.
5. Verify local `main` and `origin/main` still match the tested commit and
   the working tree is clean. Then create an annotated tag:
   `git tag -a vX.Y.Z -m vX.Y.Z`.
6. Push only the tag: `git push origin vX.Y.Z`. This starts the release
   workflow; verify it succeeds before reporting the release complete.

Keep pushed version tags immutable, including tags whose release workflow
failed. Release a corrected commit under a new patch version.

## Local config migration

When a task changes what lives in `~/.config/asanamate` (a config key or
value, an action key, a new file, or a new layout), finish the task, then ask
the user whether to migrate their local config to it. Files only the app
writes, such as state, migrate silently on load. On yes:

- Copy each file you will change to a timestamped sibling first, named
  `<file>.yyyy-mm-dd--hh-mm-ss.bak` (`date +%Y-%m-%d--%H-%M-%S`), such as
  `config.toml.2026-10-06--14-30-05.bak`. Never overwrite an existing
  backup. `asanamate config update` names its own backups this way.
- Bring `config.toml` and `actions/` onto the new shape, keeping every value
  the user set. `asanamate config update` handles new template keys in
  `config.toml`; everything else is a hand edit.
- Validate with the branch's build: `go run ./cmd/asanamate doctor` loads the
  config and actions and names the first bad file or key. Done when it runs
  clean; then report what changed and where the backups are.
- Check whether `~/.config/asanamate` sits in a git repo
  (`git -C ~/.config/asanamate rev-parse --show-toplevel`). If it does, ask
  the user whether to commit the changes. On yes, stage and commit only paths
  under `~/.config/asanamate`, never unrelated changes elsewhere in that repo.
- Once the changes are committed, delete each `*.bak` under
  `~/.config/asanamate` whose exact content git already holds (its
  `git hash-object` names a committed version of the original), since git now
  keeps that state. Keep any other backup: it is the only copy of its state.

## Asana writes

Every write to Asana goes through `writeback.Service`
(`internal/writeback/writeback.go`), so the TUI and the CLI share one
implementation. Add a new write there first, with tests, then expose it in
both: a TUI edit that calls the `Service` method (with no `Confirm`), and a
CLI subcommand in `cmd/asanamate/main.go` with `--yes`, its `usage` line, and
the README "Writing back to Asana" section. The TUI never calls the Asana
client's write methods directly.
