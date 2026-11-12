### New

- **Actions refresh the ticket and can block.** When an action finishes, the
  ticket now reloads, so a branch name or field the action wrote shows at
  once; set `refresh = false` to skip it. Set `block = true` on a background
  action to show a loading modal until it finishes. Try it: add
  `block = true` to a background action in `~/.config/asanamate/actions/` and
  run it with `a`.
