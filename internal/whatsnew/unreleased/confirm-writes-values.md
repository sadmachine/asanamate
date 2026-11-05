### Changed

- **`ASANAMATE_CONFIRM_WRITES` takes true and false.** The write commands
  accept `true` and `false` as well as `1` and `0`, and stop with an error on
  any other value instead of quietly using `confirm_writes`. Try it:
  `ASANAMATE_CONFIRM_WRITES=true asanamate comment <gid> "hi"` asks first.
