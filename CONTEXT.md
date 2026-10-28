# asanamate

Asana work in the terminal, extended by user-chosen commands and actions.

## Language

**Extension**:
A command or action that adds a capability to asanamate through one or more
extension contracts.
_Avoid_: Addon, plugin (as names for an asanamate extension).

**Extension contract**:
The documented rules an extension follows to supply a capability to
asanamate. Different extensions can implement the same contract.

**Provider**:
An extension that supplies data or operations for a capability, such as
time tracking or project links.

**Hook**:
A `[[hooks]]` entry in config that runs a built-in prompt or an action when
an event happens to a ticket.
_Avoid_: bare "hook" for a Claude Code or Codex hook; say "Claude Code hook"
or "Codex hook".
