# Deferred ideas

Improvements found during the 2026-09-28 code review that were left out of the
cleanup. Each one either adds a feature or rewrites behavior, so it needs its
own design pass. They are recorded here so they can be picked up later.

## Configurable key bindings

**Now:** `keyBindings()` in `internal/tui/keys.go` is the one table of list
and reader keys: `bindingFor` looks keys up in it for `handleKey`, and the `?`
help and the NORMAL mode hints render from it. Modals, the filter, the views panel, and the
cards view's selected row still handle their own keys first, and the hints
for those modes are written out in `Model.mode`.

**Idea:** Let a `[keys]` config table rebind entries of `keyBindings()`.

**Watch out for:** Adding config keys is a public contract (see `AGENTS.md`).
Action keys are only read inside the action menu, so they do not collide with
list keys today. An override system must keep that true, and
`TestKeyBindingsAreUniqueAndDocumented` should check the merged table.

## Theme-aware colors

**Now:** `theme = "light"` only changes the glamour Markdown style. The TUI's
own colors are fixed ANSI numbers, all defined at the top of `styles.go`
(`borderColor`, `okStyle`, `warnStyle`, `errorStyle`). ANSI numbers follow the
terminal's color scheme; a render with Tokyo Night Day
(`docs/mocks/actual-wide-light.png`) reads well, but faint text and color 8
borders depend on how pale the scheme draws them.

**Idea:** Add a small palette struct with a dark and a light variant, chosen
by `theme`, and build the styles from it.

**Watch out for:** This changes how the light theme looks, so review it
visually in both themes.

## Action run steps

**Now:** `pendingRun` in `internal/tui/flow.go` tracks progress with four
flags (`projectChosen`, `awaitingFields`, `inputDone`, `formDone`) and an
empty `branch` string.
`continueRun` and `execute` re-enter each other until every flag is settled.
The flow works, but each new step adds another flag and another re-entry
point.

**Idea:** Model the run as an ordered list of steps: pick agent, pick project,
resolve repo, collect input, fill the action form, load project fields, ask
for a branch when the branch field is empty. Each step either finishes right
away or opens a modal and resumes when it is answered.

**Watch out for:** This is the most intricate flow in the TUI. The tests in
`flow_test.go` and `agents_test.go` cover it and should pass unchanged.

## Multi-enum and people fields from the CLI

**Now:** The TUI can set `multi_enum` and `people` custom fields
(`pickedValues` in `internal/tui/edit.go`). `asanamate field` goes through
`writeback.FieldValue`, which only supports text, number, enum, and date.

**Idea:** Teach `FieldValue` to accept a comma-separated list of option names
or people names and resolve them to gids. Then the CLI and TUI share one
conversion.

**Watch out for:** People names need a workspace user lookup and a rule for
ambiguous names. `CommentHTML` already refuses two users with the same name.
Clearing the field with `""` must keep working.

## Cheaper project list

**Now:** `Client.MemberProjects` loads every unarchived project in the
workspace with its member list, then keeps the ones the user belongs to. In
large workspaces this is slow and moves a lot of data.

**Idea:** Check whether the Asana API has a server-side way to list only the
user's projects, for example through team memberships
(`/users/{gid}/team_memberships`, then `/teams/{gid}/projects`), and compare
the cost.

**Watch out for:** A team-based list may include projects the user can see
but is not a member of, which changes what the project picker shows.

## Shared shell runner

**Now:** `agents.List`, `action.Command`, `repo.Candidates`, and the
`timetracking` provider each build `/bin/sh -c <command>`.

**Idea:** Add one helper that builds the command.

**Why deferred:** It is a one-line duplicate in four places, and a new
package for it costs more than it saves. Revisit if shell handling grows, for
example a configurable shell or Windows support.

## Known issues found during the review

These were already broken before the cleanup:

- `TestRunBackgroundStartsNewSession` in `internal/action` fails under
  `go test -race`. It passes without `-race`. The race is between
  `action.RunBackground` and the test.
