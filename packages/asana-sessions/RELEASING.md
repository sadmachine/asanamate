# Standalone releases

`asana-sessions` is independent of asanamate. Its version is in `package.json`
and `npm-shrinkwrap.json`; both native plugin manifests and the Claude marketplace
entry must match. Core uses `vX.Y.Z` tags and its existing all-environment CI
rules. Standalone uses `asana-sessions/vX.Y.Z`. Neither release triggers the other.
No npm registry name is reserved or published; the actual local package name is
`asana-sessions`. Distribution uses GitHub release tarballs and checksums.

## Before tagging

1. Update package, shrinkwrap, plugin and marketplace versions together; update
   `CHANGELOG.md`. Keep resolved dependencies pinned in the shrinkwrap. Release
   steps run `npm ci`, never a dependency update.
2. Run `npm ci`, `npm test`, `npm run check`, and `scripts/check-pack.sh`.
3. Complete the live gate below on supported provider versions. Record versions,
   timings and results in the provider feasibility notes. A missing result blocks
   release; deterministic fake tests cannot replace the gate.
4. Merge and push the commit to `main`. Require every `package` and `integration` job in
   `asana-sessions-ci` to succeed on that exact commit. Recheck after any change.
5. Verify local `main` and `origin/main` match that tested commit and the working
   tree is clean. Only on explicit user request, create an annotated
   `asana-sessions/vX.Y.Z` tag and push only that tag.
6. Publish standalone releases with `--latest=false` so GitHub's Latest release
   remains the core asanamate release. Verify `asana-sessions-release` succeeds
   and the release contains the tarball, `install.mjs`, and `checksums.txt`.
   Verify checksums and install the downloaded artifact with
   `node install.mjs <archive.tgz> --prefix <disposable-prefix>`. Do not use
   npm's global tarball install, which can re-resolve dependencies rather than
   reproduce the shipped shrinkwrap.
   Keep pushed tags immutable; correct failures under a new patch version.

Core releases still require every job of the core `ci` workflow on their exact
commit. Standalone CI does not replace or relax those gates. Report the two
products' release status independently. Do not tag or publish the hub for a
standalone release.

## Live provider gate

Use disposable homes and working directories, with user-authorized authentication
and model turns. Do not copy personal history. Run provider executables through
`caveman`. These checks spend tokens and never run in pull-request CI.

- Validate Claude's plugin using `claude plugin validate <plugin-root>`. Register
  each marketplace and plugin natively in its disposable home. Repeat setup and
  upgrades; verify exactly one owned prompt hook runs per submission. Codex
  `hooks/list` must show one prompt hook and no Stop hook for this plugin.
- Review native feature and trust requirements manually. Setup must not enable
  unrelated features, approve trust, or bypass policy. Verify untrusted and
  disabled hooks skip safely.
- Start each CLI directly outside asanamate and ccmux. Submit one task URL, then
  another prompt, then restart/resume. Read the exact ID through the metadata
  API and history listing after each stage; the marker must persist. Record
  total hook latency below the 10-second companion / 15-second host budget.
- Claude: prove first prompt precedes persistence and a real nonblocking Stop
  completes the pending write after title generation. Codex: prove immediate
  linking remains reliable without deferred state.
- Submit duplicate URLs, two distinct tasks, and a different task after linking.
  Verify idempotence or nonblocking notices, with no replacement.
- Exercise the explicit skill in each CLI. Verify host session identity is
  available, or document the need for an explicit ID. Link, change with
  `--replace`, and unlink; preserve human title and unrelated tags.
- Run `scripts/live-reentrancy.sh both`. It races concurrent prompt hooks with
  `link --replace`, `unlink`, and raw native renames on one disposable
  conversation per provider; Codex spends one short `codex exec` turn.
- Exercise missing helper, old provider version, malformed/oversize event,
  deadline, unwritable history, unknown identity, and readback failure. Chat
  must remain usable, with no blocking output fields or false success.
- Disable/remove each plugin. Verify titles and history survive; the installed
  helper still lists and opens the linked session. Check PATH diagnostics in
  the environment that actually starts each provider, including login shells.

GUI and Paseo title synchronization are outside this gate and are not promised.
