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
