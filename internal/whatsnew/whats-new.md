# What's new

Curated highlights shown after an upgrade. Each version gets a `### New`
section and, only when needed, a `### Changed` section; see AGENTS.md.

## Unreleased

### New

- **Group by where work stands.** The `state` grouping sorts tickets into
  Needs me, Active, Blocked, and Ready, from agent state, uncommitted or
  unpushed work on the ticket's branch, and open dependencies. Try it:
  press `b` and pick `state`.
- **Your own groupings.** Define `[[groupings]]` in `config.toml`: sections
  with filter queries, where each ticket joins the first section it
  matches. Try it: copy the `state` example from
  [Rule groupings](https://github.com/sadmachine/asanamate#rule-groupings)
  and change its sections.
- **Filter by git and blockers.** New terms `git:dirty`, `git:unpushed`,
  `blocked:any`, and `branch:<name>`. Try it: press `/` and type
  `git:dirty`.

## v0.4.4

### New

- **Fold group sections.** In a grouped list, fold a section down to its
  header to hide tickets you don't need right now. Try it: press `-` on a
  ticket to fold its group, or `+` to fold every other group. Folds are
  saved per project and in saved views.
- **Since you looked.** Opening a My Tasks ticket lists the comments and
  changes others made since you last viewed it, in a card above the
  description. Try it: open a ticket someone commented on. Press `s`, then
  `w` to turn it off.
- **Streamlined reader view.** The same ticket with less chrome: fields sit
  in compact rows under the title. Try it: press `v` to step through cards,
  streamlined, and markdown.
- **What's new.** After an update, asanamate shows the release's highlights
  once. Reopen them from Settings & tools (`,`, then `e`), or run
  `asanamate whats-new`. Press `d` in What's new to stop it opening on
  startup.

### Changed

- **The reader starts on the title.** Entering the reader selects the
  ticket's title, and `enter` edits it, like every other field in the cards
  view.
