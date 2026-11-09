# What's new

Curated highlights shown after an upgrade. Each version gets a `### New`
section and, only when needed, a `### Changed` section; see AGENTS.md.
Unreleased highlights wait in `unreleased/`, one file per change, until a
release moves them here.

## v0.4.9

### New

- **Find files to attach.** The comment's **Files** menu searches the
  ticket's repo, Downloads, Desktop, and Documents, and attaches several
  files at once; dropping several files on the terminal works too. On macOS
  and Linux desktops it can also open the system file dialog. Try it: press
  `c`, then `tab`, `enter`, and `s`.
- **Choose your own icons.** The new `[icons]` table in `config.toml` replaces
  any icon by name, such as the project or due date icon, or hides one to show
  its label. Try it: run `asanamate config update`, then set
  `project = "\uf07b"` under `[icons]` to bring back the folder icon.

### Changed

- **Easier-to-scan ticket fields.** Dots now separate the fields in the
  streamlined view, and the built-in fields stack one to a row, labeled and
  with their values lined up, while you select one. Every view now lists the
  My Tasks section above the ticket's projects. Try it: press `v` until the
  reader shows the streamlined view, then `tab` into the reader and move onto
  Assignee.
- **What's new is a list.** What's new lists each highlight's title under its
  version, and shows the highlighted one in full beside the list, so you can
  skim the titles and read only what interests you. Try it: press `,`, then
  `e`, and move with `j` and `k`.

## v0.4.8

### New

- **Files on comments.** Attach files to a new comment from the **Files** row
  above the task details: type a path or drop a file on the terminal. Files
  someone attached to a comment in Asana now show under that comment, and the
  comment menu opens them. Try it: press `c`, then `tab` and `enter`; or
  select a comment with files, press `enter`, then `f`.

## v0.4.7

### New

- **Pull requests beside their tickets.** asanamate finds each ticket's
  GitHub pull requests and GitLab merge requests by branch or by an Asana
  link in their description, and shows their checks and reviews in the
  reader; `enter` opens one. New filter terms `pr:`, `checks:`, and
  `review:` sort tickets into In review in the `state` grouping and Review
  feedback in Now. Try it: add `asanamate-github` or `asanamate-gitlab` to
  `[pull_requests] commands`, then see
  [Pull requests](https://github.com/sadmachine/asanamate#pull-requests-optional).

## v0.4.6

### Changed

- **`ASANAMATE_CONFIRM_WRITES` takes true and false.** The write commands
  accept `true` and `false` as well as `1` and `0`, and stop with an error on
  any other value instead of quietly using `confirm_writes`. Try it:
  `ASANAMATE_CONFIRM_WRITES=true asanamate comment <gid> "hi"` asks first.

## v0.4.5

### New

- **Command palette.** Find and run any command without remembering its
  key: type a few words, see the command's key, and press `enter`. It also
  lists your actions and commands that have no key. Try it: press `:` and
  type `due`.
- **Now: what needs you.** A new place lists only your tickets that need
  you, grouped by why: Needs reply, Agent waiting, New to you, Updated, and
  a folded Waiting on others. Opening a ticket marks it seen, and it leaves
  Now when you move on. Try it: press `W`, or pick Now at the top of the
  Places panel.
- **Filter by what is new.** New terms `new:comment`, `new:mention`,
  `new:assigned`, `new:change`, `new:ticket`, and `waiting:reply` work in
  My Tasks and Now. Try it: press `/` and type `new:mention`.
- **Sections that start folded.** A rule grouping section with
  `folded = true` starts folded. Try it: see
  [Rule groupings](https://github.com/sadmachine/asanamate#rule-groupings).
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

### Changed

- **The Places panel lists only places.** The wide-screen sidebar, renamed
  from Views to Places, lists My Tasks, Activity, saved views, and projects.
  Grouping, sorting, the Activity filter, and agent counts moved out of it;
  they already show in the bottom bar. Try it: press `b` to group, `B` to
  sort, and `[`/`]` in Activity.

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
