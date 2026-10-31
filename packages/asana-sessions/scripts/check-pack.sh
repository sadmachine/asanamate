#!/bin/sh
# Installs the packed tarball into an empty prefix with a fresh home and no
# checkout, asanamate, provider history, or Asana token, then checks that every
# public command runs and fails only for missing provider history.
set -eu
nodebin=$(node -p "require('node:path').dirname(process.execPath)")
pkg=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
node "$pkg/scripts/check-plugins.js"
tarball=$(cd "$pkg" && npm pack --silent --pack-destination "$work")
tar -xOf "$work/$tarball" package/install.mjs > "$work/install.mjs"
node "$work/install.mjs" "$work/$tarball" --prefix "$work/prefix" > "$work/install.txt"
cat "$work/install.txt"
grep -Fq 'If asanamate-ccmux is installed, you can link running agents to Asana tickets using asana-sessions.' "$work/install.txt"
grep -Fxq '[agents]' "$work/install.txt"
grep -Fxq 'command = "asana-sessions asanamate agents -- asanamate-ccmux"' "$work/install.txt"

run() {
  (cd "$work" && env -i HOME="$work/home" PATH="$work/prefix/bin:$nodebin:/usr/bin:/bin" asana-sessions "$@")
}
id=01a14609-3a63-7c02-bd2b-c313bedc3c37
mkdir "$work/home"

installed=$(node -p 'require("node:path").dirname(require("node:path").dirname(require("node:fs").realpathSync(process.argv[1])))' "$work/prefix/bin/asana-sessions")
test -f "$installed/npm-shrinkwrap.json"
node --input-type=module - "$installed" <<'CHECK'
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
const root = process.argv[2];
const lock = JSON.parse(readFileSync(join(root, 'npm-shrinkwrap.json')));
for (const [path, entry] of Object.entries(lock.packages)) {
  const manifest = join(root, path, 'package.json');
  if (existsSync(manifest)) {
    assert.equal(JSON.parse(readFileSync(manifest)).version, entry.version, path);
  } else {
    assert.ok(entry.optional, `missing pinned dependency ${path}`);
  }
}
CHECK
for provider in claude codex; do
  test -f "$installed/plugins/$provider/asana-sessions/skills/link-asana-task/SKILL.md"
  test -f "$installed/plugins/$provider/asana-sessions/hooks/hooks.json"
done
run hook codex <<'EVENT' > "$work/hook.txt"
{"hook_event_name":"UserPromptSubmit","session_id":"invalid","cwd":"/tmp","prompt":"https://app.asana.com/0/1/42"}
EVENT
grep -q 'skipped' "$work/hook.txt"
run version
run help > /dev/null
run doctor > "$work/doctor.txt" || { cat "$work/doctor.txt"; exit 1; }
grep -q '^ok: @anthropic-ai/claude-agent-sdk ' "$work/doctor.txt"
if run list https://app.asana.com/0/1/42 > "$work/list.txt"; then echo "list passed without history"; exit 1; fi
grep -q '^info: claude: unavailable' "$work/list.txt"
grep -q '^info: codex: unavailable' "$work/list.txt"
for cmd in "link --provider claude --session $id --task 42" "unlink --provider codex --session $id" "open 42 --provider codex --session $id"; do
  # shellcheck disable=SC2086
  if run $cmd 2> "$work/err.txt"; then echo "$cmd passed without history"; exit 1; fi
  grep -q 'not installed or has no history' "$work/err.txt"
done
echo "clean install ok"
