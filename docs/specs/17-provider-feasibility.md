# Provider feasibility observations

Tested on 2026-10-16 on macOS, using disposable provider homes and a disposable
working directory. Provider executables ran through `caveman`; interactive
checks used a real PTY through `rtk run` (`rtk proxy` captures streams and does
not preserve terminal input). No personal conversation history was read or
copied. Codex authentication alone was copied into its isolated home; Claude
was authenticated separately by the user.

## Gate status: local provider checks pass

Local live provider checks now pass at the tested versions below. The earlier
conservative revision allowed generic spec 16 work to proceed independently;
the 2026-10-17 Claude retry closes the account-quota gap. These local results
are not a release approval or proof of the remote Node/platform CI matrix.

## Tested versions

- Claude Code: `2.1.301`.
- `@anthropic-ai/claude-agent-sdk`: `0.3.301`, installed in a temporary
  directory, outside the repository. Its package declares Node `>=18.0.0`,
  platform-specific optional native packages, and peer dependencies on
  `@anthropic-ai/sdk`, `@modelcontextprotocol/sdk`, and `zod`. The installed
  dependency tree contained 105 packages. This does not establish Node 18
  compatibility; only Node `24.18.0` was exercised.
- Codex CLI: `0.162.1`.
- Node: `24.18.0`.

## Claude observations

1. A native CLI print-mode conversation was created directly with an explicit
   UUID. SDK `listSessions` discovered it across projects; `getSessionInfo`
   returned the same UUID, cwd, title, and modification time. One-record local
   lookup took about 2 ms, excluding SDK startup. This is not a representative
   history benchmark.
2. SDK `renameSession` changed its title, and a separate SDK invocation read
   the new title. Interactive `--resume <uuid>` restored the same conversation.
3. While that interactive process remained open, another SDK invocation renamed
   the title again. A subsequent prompt completed in the original terminal;
   SDK lookup still returned the externally changed title. The original
   terminal retained its old cached display title.
4. While the first interactive terminal remained open, a second print-mode
   `--resume <uuid>` accepted and completed a prompt. A second interactive
   `--resume <uuid>` also opened the same conversation without refusal or a
   concurrency warning. Both interactive terminals printed the same UUID in
   their exit instructions. No additional conversation ID appeared in SDK
   discovery. These checks establish that CLI resumption does not provide the
   cross-process exclusion required by the proposed opener.
5. `SDKSessionInfo` exposes no activity or archive field. Listing cannot prove
   inactivity. Archive behavior was not established.

Claude discovery and explicit native-ID targeting work in these checks.
Safe default resumption of an independently launched conversation is **not
established**: an addon-only lock cannot exclude either native terminal.
Do not treat a confirmation about unknown activity as proof of exclusion.

## Codex observations

1. Native `codex exec` created a persisted conversation directly. An initialized
   stdio app-server connection discovered it using explicit source kinds
   (`cli`, `exec`, `appServer`). The one-record list request took about 3 ms,
   excluding process startup. This is not a representative history benchmark.
2. `thread/read` returned its exact ID, cwd, source, timestamps, and null name
   without starting a turn. `thread/name/set` set a canonical Asana suffix;
   a separate connection read that name back.
3. One app-server connection resumed the thread. A separate connection could
   rename and read it, but reported `status.type: "notLoaded"`. Thus this
   status cannot establish global inactivity.
4. Resumption through the second connection failed with code `-32600`:
   `thread <id> already has an active writer`.
5. Interactive `codex resume <id> --no-daemon` restored the same saved
   conversation. Inherited `CODEX_THREAD_ID` and `CODEX_SESSION_ID` referred to
   the parent agent; the explicit selected ID still targeted the disposable
   conversation. This is one nested-launch check, not a complete environment
   compatibility guarantee.

## Package checks (2026-10-16)

The `asana-sessions` package was then run against a new disposable Codex home,
using a conversation created directly by `codex exec`:

1. Listing found it with explicit source kinds. `thread/list` `searchTerm`
   matched the canonical marker in a name set through `thread/name/set`.
2. `link`, a repeated `link` (`changed: false`), a conflicting link, `--replace`,
   and `unlink` all verified by separate reads. `thread/name/set` cannot clear
   a name, so unlinking an otherwise untitled thread leaves `Conversation`.
3. After `thread/archive`, the archived-only query listed it as archived.
   `open` confirmed unknown activity, asked for a replacement for the missing
   cwd, rejected a nonexistent replacement, disclosed the restore, and called
   `thread/unarchive`; the thread then listed as not archived, under the same
   ID, with no additional conversation created.
4. `codex resume <id>` under `expect` exited immediately in both the package
   and a direct run, because that pseudo-terminal does not answer Codex's
   terminal queries. A Python PTY that answers them proved it (item 8).

A second disposable Codex home then covered the remaining Codex items:

5. Titles: a 5,011-character name (non-ASCII prefix plus marker) round-tripped
   unchanged, so no limit was found. `thread/name/set` rejects an empty name
   with `thread name must not be empty`.
6. Active-turn mutation: during a `codex exec resume` turn running
   `sleep 25`, a separate connection read `status.type: "notLoaded"` and
   renamed the thread. The turn completed normally; the new name persisted
   afterwards and appeared in the resumed TUI. Codex did not overwrite it.
7. Interactive `asana-sessions open` in a real PTY, with a bogus inherited
   `CODEX_THREAD_ID`, resumed the selected thread with its earlier turns and
   exited 0. Codex printed the same ID in its reconnect instructions.
8. Two concurrent `open` runs of one thread both attached to the shared
   daemon's single copy of it, with no refusal, fork, or new session file.
   Codex owns the one writer; `--no-daemon` would bypass that.
9. After `thread/archive`, interactive `open` restored and resumed the thread
   under the same ID with its earlier turns, creating no session file.
10. `codex resume` starts a background daemon under `CODEX_HOME` that keeps
   running after the TUI exits and can update itself: one disposable home's
   daemon had moved to `0.163.0` while the CLI was `0.162.1`. Stop such
   daemons before deleting a disposable home.

Cold lookup against the user's real homes (metadata only; counts and times
recorded, no titles): 282 Claude and 207 Codex sessions. The first full
lookup took 991 ms (Codex alone up to 1,076 ms); repeats took 367 ms. This fits
the 10-second deadline without an index.

## Claude checks (2026-10-16)

A disposable `CLAUDE_CONFIG_DIR`, logged in by the user, and a conversation
created directly by `claude -p --session-id`:

1. Archive: neither Claude Code `2.1.301` (`claude --help`) nor the SDK's
   session API has an archive concept, so records report `archived: false`.
2. SDK metadata reads, `link`, `unlink`, and `list` worked with no `claude`
   executable on `PATH`. The CLI is needed only to resume.
3. Missing title: the untitled conversation's `summary` equalled its first
   prompt, and `link` named it `Conversation [asana:<GID>]`.
4. Titles: a 5,013-character title round-tripped unchanged; no limit found.
5. In-flight turn: during a `claude -p --resume` turn running a 25-second
   shell command, an SDK rename persisted after the turn finished.
6. Open interactive session: an SDK rename made while `claude --resume` was
   open in a real PTY survived `/exit`; the CLI did not write its cached title
   back. A separate process read the new title afterwards (restart
   persistence). It resumed the selected ID; no conversation was created.
7. Nested launch: inherited from a parent Claude Code session,
   `CLAUDE_CODE_CHILD_SESSION` made the resumed CLI warn "Transcript saving is
   off". With only that variable removed, saving stayed on and the inherited
   `CLAUDE_CODE_SESSION_ID` did not change the resumed ID. The Claude resume
   command clears it.

## Claude activity registry (2026-10-16)

Concurrent resumption is still not excluded (Claude observation 4), but each
running CLI process writes `<CLAUDE_CONFIG_DIR>/sessions/<pid>.json` with
`pid`, `sessionId`, `procStart`, `pidDomain`, `kind`, and `status`. A second
disposable home (CLI `2.1.301`, user login) showed:

1. A fresh interactive session registers; the file is removed on `/exit`.
2. `claude --resume <id>` registers that ID.
3. `/clear` switches the entry to the new ID; `/resume <id>` to the target.
4. `claude -p` registers the ID (`busy`) while it runs, then removes it.
5. `kill -9` leaves the file behind with a dead PID; a later start removed it.
6. `procStart` equals `ps -o lstart=` under `TZ=UTC LC_ALL=C`, which guards
   against PID reuse. The user's real home (counts only) had 9 entries, all
   live, matching 9 `active` records, including the testing session.

The companion now reports `active` for a conversation held by a live entry and
`inactive` otherwise; a missing or unexpected registry gives `unknown`, which
the Claude opener still refuses. End to end: a stale entry was ignored, a
conversation open in another terminal was refused as active, and an inactive
one was resumed directly in a PTY with the inherited
`CLAUDE_CODE_CHILD_SESSION` cleared. The registry is undocumented; a small
window remains between the check and the launch.

## Remaining gate work

Hook reentrancy passed on 2026-10-19 (see below). Claude has no native exclusion; the opener relies on its live-session
registry instead and refuses when the registry cannot be read. Do not publish minimum supported versions
until the hook checks pass.

The user-approved conservative revision retains Claude discovery/linking but
refuse default Claude resumption when inactivity and exclusion cannot be
established. This changes the advertised default Claude resumption outcome.
A custom host opener still needs an
explicit native-session mapping and authoritative ownership behavior.

## References

- [Claude session APIs](https://platform.claude.com/docs/en/agent-sdk/sessions).
- [Codex app-server](https://developers.openai.com/codex/app-server).
- Exact exported Claude SDK declarations shipped in `0.3.301`.


## Spec 18 implementation evidence (2026-10-16)

Deterministic checks pass on local Node 24.18.0: 55 package tests, plugin asset
validation, and a tarball installed in an empty prefix and provider home. The
packed install includes native manifests/hooks/skills, license and an npm
shrinkwrap; installed dependency versions are checked against that shrinkwrap.
The core conversation/Asana/CLI checks pass (103 tests). Node 22 and Linux are
CI targets; this local run does not establish their results.

Both native plugin installers were exercised twice in fresh temporary provider
homes. Each reported exactly one `asana-sessions@asana-sessions` registration
at version 0.1.0, then native plugin and marketplace removal succeeded. Claude's
`plugin validate` accepts its plugin manifest. Codex `hooks/list` discovers
exactly one enabled, untrusted `userPromptSubmit` hook, timeout 15 seconds, and
no Stop hook. No trust was granted by these checks.

Earlier hook probes in this worktree established that Claude's first prompt can
precede the readable transcript. The implementation retains only pending
provider/session/GID/time and attempts completion from Stop. Earlier Codex
probes linked immediately after deriving CODEX_HOME from `transcript_path`:
a login-shell profile had overridden the isolated home. The implementation
therefore verifies transcript/session shape and uses the event's provider home.
Codex has no deferred state or Stop hook.

### Authenticated isolated checks

The user authorized authenticated model turns. A packed 0.1.0 install was used
in fresh provider homes and a fresh work directory. Only available authentication
was supplied; no personal history or provider configuration was copied. Provider
executables ran through `caveman`. The exact owned Codex hook definition was
reviewed and trusted in its disposable home; setup did not grant that trust.

Codex 0.162.1 passed these live checks:

- A first submitted task URL linked the exact native ID immediately. A later
  prompt and subsequent separate `exec resume` processes preserved the marker
  in native metadata/history. No Stop hook or pending link was needed.
- Repeated identical URLs were idempotent. Two distinct tasks produced a
  nonblocking ambiguity notice. A different existing task produced a conflict
  notice, without changing the saved link. Every submitted prompt had at most
  one owned hook invocation after repeated native registration.
- The installed explicit skill was loaded by the model and used `--current`
  successfully from its tool environment. Changing the task used `--replace`.
  Unlink and relink preserved a human title supplied through the native naming
  API. Hook environments themselves did not expose CODEX_THREAD_ID; automatic
  linking correctly used the event ID instead.
- Disabled and untrusted plugins ran no owned hook and did not block replies.
  Removing the helper from PATH also did not block the reply. Existing links
  remained unchanged in all three cases.
- Local upgrade fixtures advanced the isolated package/plugin manifests to
  0.1.1 and then 0.1.2; repository/package versions remained 0.1.0. This exposed
  a Claude installer issue: repeated native install retained its old version.
  Setup now removes only the stale owned registration before reinstalling. Its
  regression test fails without that fix and passes with it. Native upgrades
  then succeeded for both providers, and Codex still loaded exactly one hook.
- Changing only the disposable hook timeout during the next upgrade produced
  native trustStatus `modified`; setup reported `untrusted`. It did not approve
  the changed definition. With matching PATH and reviewed unchanged content,
  standalone setup returned 0 and reported `ready`.
- Native removal succeeded for both providers, cleared their owned pending
  state, and preserved all three synthetic transcript files byte-for-byte.
  The installed helper still listed the Codex link and its human title after
  plugin removal. The opener implementation is unchanged from the earlier
  native-open checks above; a new interactive open was not run here.

Measured Codex hook durations were 596, 35, 196, 36, 208, 33 and 38 ms,
including wrapper/process startup and native metadata verification. This
supports the 10-second helper / 15-second host budget for these disposable
Codex histories; it is not a large-history benchmark. The deterministic suite
covers malformed/oversize events, unreadable/unknown sessions, deadlines,
readback failures, and late-write prevention without spending model tokens.

Claude Code 2.1.301 authenticated successfully, exported the matching
CLAUDE_CODE_SESSION_ID to its hook, and ran exactly one UserPromptSubmit hook
before its transcript existed (96 ms, exit 0, no notice). Its first reply was
rejected with `You've hit your session limit · resets 7:10pm (America/New_York)`
on 2026-10-16. No real Stop ran in that attempt; the remaining Claude checks
were deferred until the account limit reset.

### Claude retry (2026-10-17)

A fresh packed 0.1.0 install, isolated provider home and working directory were
used with the user's existing authorization. Claude Code remained 2.1.301;
Node remained 24.18.0. No personal history or configuration was copied, and all
provider executable calls ran through `caveman`.

- The first authenticated prompt completed successfully. UserPromptSubmit ran
  before the transcript existed (81 ms), retained the pending GID, and a real
  nonblocking Stop linked it after persistence (71 ms). Independent SDK/history
  lookup found the exact native ID and `Conversation [asana:424242420018]`.
- Separate `--resume` processes submitted later prompts. The title persisted.
  Duplicate task URLs were silent/idempotent; two tasks produced an ambiguity
  notice, and another existing task produced a conflict notice with no relink.
  Each prompt completed with exit 0 and an assistant reply.
- The installed `asana-sessions:link-asana-task` skill was invoked natively.
  It used `--current --replace` to change the task, then separate helper calls
  unlinked and linked it again. Independent SDK reads after both operations
  verified the exact session, human title and unrelated `keep-test-tag` tag.
- Native plugin disablement ran no owned hooks and did not block the reply.
  With the helper absent from PATH, the host reported nonblocking hook errors
  and still completed the reply. The saved title and unrelated tag survived.
- Across seven enabled submissions there were exactly seven owned prompt hooks
  and seven owned Stop hooks, all exit 0. Durations were 81, 71, 32, 31, 70, 37,
  54, 32, 72, 31, 36, 42, 39 and 34 ms. No pending entries remained. These checks
  support the 10-second helper / 15-second native budget for the tested history;
  they do not establish large-history latency.
- Native removal preserved the synthetic transcript files byte-for-byte and
  retained the title/tag. The installed helper then opened the exact inactive
  session through `claude --resume` in a real terminal, without either plugin
  installed. Its native title and exit resume command identified the same
  session. After closing it, SDK lookup still returned the expected title/tag.

The quota blocker is resolved and no product-code change was needed for this
retry. Disposable homes were removed after verification. The remaining release
requirements are the exact-main remote Node/platform CI gates, clean/local-remote
commit agreement, and explicit authorization to tag; follow the
[standalone release gate](../../packages/asana-sessions/RELEASING.md).

## Hook reentrancy and integrated proof (2026-10-19)

`scripts/live-reentrancy.sh both` installs the packed helper into an empty
prefix and races its prompt hook against one real conversation per provider in
disposable homes. Claude used a synthetic transcript read and renamed by the
pinned SDK, with no model turn. Codex 0.163.0 created its conversation with one
`codex exec` turn, authenticated by copying only `auth.json`, and ran through
`caveman`. Both providers passed every check:

- Eight simultaneous hooks for one task linked once. Exactly one hook claimed
  the link; the others were silent or reported a busy write. All exited 0
  within 1 second.
- Hooks for another task, racing an explicit `link --replace`, reported a
  conflict or busy write and never relinked. The saved title matched the
  replace command's exit status.
- Raw native renames that bypass the helper's lock, racing hooks, left either
  the human title or the human title with one marker. No hook claimed an
  unverified link.
- `unlink` racing hooks kept the human title with at most one marker, and no
  lock remained afterwards.

The user's manual spec 19 proof passed for Claude Code and Codex: plugin
linking outside asanamate, lookup and resumption of the same native ID from the
ticket picker, returning to the ticket, manually named chats with hooks off,
archived Codex display and resumption, missing-directory replacement and
cancel, provider failure reporting, refusal of an active Claude conversation,
and both CLI commands without an Asana token. Codex unknown activity asked for
confirmation, and both answers behaved correctly. Resuming the archived Codex
conversation restored it, but its disclosure printed after the confirmation and
the provider's full screen hid it. The disclosure now precedes the question.

The first Codex attempt exposed an environment issue, not a product defect:
Codex ran hooks in its shared app-server daemon, which kept the PATH of the
directory it started in. That directory pinned another mise Node version, so
the hook could not find `asana-sessions` and exited 127.
Linking the helper into a directory on every PATH fixed it; the package README
now documents this.

## 0.1.1 patch verification (2026-10-19)

The deferred-link cancellation fix shares the native metadata lock across
prompt deferral, Stop completion, and explicit unlink. Five new regressions
cover unlink before/after persistence, both in-flight hook phases, and a
conflicting native link. The pre-fix code failed both cancellation cases;
all 60 package tests pass with the fix.

Disposable histories through the pinned real Claude SDK verified that unlink
cancels a queued link both before and after transcript persistence; a later
Stop is silent and ticket lookup returns no linked conversation. No model
turn or personal history was needed for those cases.

The packed helper passed `scripts/live-reentrancy.sh both` again. Claude's
synthetic native transcript and real SDK metadata operations passed all
concurrent-hook, replace, raw-rename, and unlink cases within 1 second.
Codex CLI 0.163.0 created one authenticated disposable conversation through
`caveman`; the same cases passed within 2 seconds. Neither provider left a
metadata lock behind. The dependencies, native hook definitions, and CLI
resume behavior are unchanged; the earlier native plugin lifecycle and
interactive-open observations above remain the host integration evidence.

The release workflow now opts out of GitHub Latest designation. The existing
core `v0.3.1` release was restored as Latest without moving either tag or
replacing published assets. Version 0.1.1 publication still requires the
exact-main package/platform CI and downloaded-artifact checks.

## 0.1.2 locked-install verification (2026-10-21)

The installer now extracts the release tarball into an isolated project root and
runs `npm ci --omit=dev` against its shipped shrinkwrap. The former global npm
install could re-resolve dependencies, including the Anthropic SDK and
transitive packages. The packaged installer check verified all 106 installed
package manifests against the shrinkwrap. All 71 package tests passed, including
five installer regressions covering upgrades, failed installs, unsafe targets,
archive validation, and concurrent installs.

The packed 0.1.2 helper passed `scripts/live-reentrancy.sh both` using Node
24.18.0, the pinned Claude Agent SDK 0.3.301, and Codex CLI 0.163.0. Claude Code
2.1.306 also passed native plugin validation, with only the optional icon
warning. Claude used a synthetic transcript and real SDK metadata operations;
it did not spend a model turn. With explicit user authorization, Codex created
one disposable conversation through `caveman`, copying only `auth.json` into its
isolated home. No personal history or provider configuration was copied.

Both providers passed all concurrent-hook, conflicting-task, explicit replace,
raw native rename, and unlink checks. Eight parallel hooks linked exactly once,
with exactly one success notice and all hooks exiting 0. The measured batches
finished in 1 second for Claude and 2 seconds for Codex, below the 10-second
helper deadline. Saved titles matched operation results, human titles survived,
no hook claimed an unverified success, and neither provider left a metadata lock.
Disposable homes and working files were removed after verification.

Dependencies, native hook definitions, and CLI resume behavior are unchanged
from 0.1.1. Earlier native lifecycle, trust, Stop, skill, resume, removal, and
interactive-open observations above remain the host integration evidence;
those interactive checks were not repeated for this packaging-only fix.
Publication still requires all package/platform and integration CI jobs on the
exact release commit, followed by downloaded-artifact verification.

## 0.1.3 release without live gate (2026-10-29)

The user waived the live provider gate for 0.1.3 and released it on
deterministic tests and their own install check. This release adds Codex's
PostToolUse hook, which completes a deferred link at the first tool call after
Codex generates the thread title. Its command skips Node while the pending
directory is empty; through a login zsh it measured about 17 ms per tool call.
Codex 0.165.0 names this event `postToolUse` in its hook list. Not yet observed
live: that Codex fires PostToolUse for every tool type, and that its event
carries `transcript_path`. The next live gate should cover both.
