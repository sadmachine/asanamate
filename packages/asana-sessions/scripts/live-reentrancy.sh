#!/bin/sh
# Live release gate: hook reentrancy. Runs the packed helper's prompt hook many
# times at once against one real conversation in disposable provider homes,
# racing explicit link --replace, unlink, and raw native renames that bypass
# this package's lock. Every hook must exit 0, finish inside its deadline,
# never claim a link it did not verify, and never leave duplicate or foreign
# markers or a held lock.
#
# Usage: scripts/live-reentrancy.sh [claude|codex|both]
#
# Claude needs no model turn: the conversation is a synthetic transcript that
# the pinned SDK reads and renames. Codex creates its conversation with one
# short `codex exec` turn, authenticated by copying only auth.json from
# $SOURCE_CODEX_HOME (default $CODEX_HOME, else ~/.codex). Provider
# executables run through `caveman`; set REAL_CODEX to choose the binary.
set -eu
which=${1:-both}
pkg=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
nodebin=$(node -p "require('node:path').dirname(process.execPath)")
fail=0
source_codex=${SOURCE_CODEX_HOME:-${CODEX_HOME:-$HOME/.codex}}
real_codex=${REAL_CODEX:-$(command -v codex || true)}

tarball=$(cd "$pkg" && npm pack --silent --pack-destination "$work")
tar -xOf "$work/$tarball" package/install.mjs > "$work/install.mjs"
node "$work/install.mjs" "$work/$tarball" --prefix "$work/prefix"
installed=$(node -p 'require("node:path").dirname(require("node:path").dirname(require("node:fs").realpathSync(process.argv[1])))' "$work/prefix/bin/asana-sessions")
mkdir -p "$work/shims" "$work/repo" "$work/state" "$work/claude/projects/p" "$work/claude/sessions" "$work/codex"
export XDG_STATE_HOME="$work/state" CLAUDE_CONFIG_DIR="$work/claude" CODEX_HOME="$work/codex"
export PATH="$work/shims:$work/prefix/bin:$nodebin:/usr/bin:/bin"

# probe reads or natively renames a conversation through the provider module,
# outside the helper's lock: the same API a provider's own rename uses.
cat > "$work/probe.mjs" <<EOF
import { createProviders } from '$installed/src/engine.js';
const [op, provider, id, title] = process.argv.slice(2);
const p = createProviders({ env: process.env })[provider];
if (op === 'title') process.stdout.write(((await p.read(id))?.name ?? '') + '\n');
else await p.rename(id, title);
EOF
probe() { node "$work/probe.mjs" "$@"; }

check() { # description, condition result (0 = pass)
  if [ "$2" -eq 0 ]; then echo "PASS $1"; else echo "FAIL $1"; fail=1; fi
}

url() { echo "https://app.asana.com/0/1/$1"; }

# hooks runs n prompt hooks with task gid in parallel, writing out.<i> and code.<i>.
hooks() { # provider id transcript gid n prefix
  i=0
  while [ "$i" -lt "$5" ]; do
    (printf '{"hook_event_name":"UserPromptSubmit","session_id":"%s","cwd":"%s","prompt":"see %s please","transcript_path":"%s"}' \
      "$2" "$work/repo" "$(url "$4")" "$3" | asana-sessions hook "$1" > "$work/$6.out.$i"; echo $? > "$work/$6.code.$i") &
    i=$((i + 1))
  done
}

# outcomes checks every hook in batch prefix exited 0 and only printed allowed notices.
outcomes() { # prefix allowed-regex
  bad=0
  for f in "$work/$1".code.*; do [ "$(cat "$f")" = 0 ] || bad=1; done
  for f in "$work/$1".out.*; do
    [ -s "$f" ] || continue
    grep -Eq "$2" "$f" || { echo "  unexpected notice: $(cat "$f")"; bad=1; }
  done
  return $bad
}

claims() { cat "$work/$1".out.* | grep -c "linked this conversation to Asana task $2" || true; }
markers() { printf '%s' "$1" | grep -o '\[asana:[0-9]*\]' | wc -l | tr -d ' '; }
nolock() { [ -z "$(ls -A "$XDG_STATE_HOME/asana-sessions/locks" 2>/dev/null)" ]; }
busy='another asana-sessions write to this conversation is in progress'

gate() { # provider id transcript
  p=$1 id=$2 tr=$3
  echo "== $p $id"
  probe rename "$p" "$id" "Reentrancy $p"

  start=$(date +%s)
  hooks "$p" "$id" "$tr" 101 8 same; wait
  elapsed=$(( $(date +%s) - start ))
  t=$(probe title "$p" "$id")
  check "$p: 8 parallel hooks link once ($t)" "$([ "$t" = "Reentrancy $p [asana:101]" ] && echo 0 || echo 1)"
  outcomes same "linked this conversation to Asana task 101|$busy"; check "$p: hooks exit 0 with honest notices" $?
  n=$(claims same 101)
  check "$p: exactly one hook claims the link ($n)" "$([ "$n" -eq 1 ] && echo 0 || echo 1)"
  check "$p: batch finished in ${elapsed}s, under the 10s deadline" "$([ "$elapsed" -lt 10 ] && echo 0 || echo 1)"

  hooks "$p" "$id" "$tr" 202 4 other
  replaced=0; asana-sessions link --provider "$p" --session "$id" --task 303 --replace > /dev/null 2>&1 || replaced=$?
  wait
  t=$(probe title "$p" "$id")
  case "$t" in *"[asana:202]"*) foreign=1 ;; *) foreign=0 ;; esac
  check "$p: hooks for another task never relink ($t)" "$foreign"
  outcomes other "already links ticket|$busy"; check "$p: conflicting hooks report conflict or busy" $?
  if [ "$replaced" -eq 0 ]; then want="Reentrancy $p [asana:303]"; else want="Reentrancy $p [asana:101]"; fi
  check "$p: explicit --replace (exit $replaced) matches the saved title" "$([ "$t" = "$want" ] && echo 0 || echo 1)"

  # Raw native renames bypass the lock, like a provider's own title writer.
  hooks "$p" "$id" "$tr" 303 4 native
  for _ in 1 2 3; do probe rename "$p" "$id" "Retitled $p" & done
  wait
  t=$(probe title "$p" "$id")
  case "$t" in "Retitled $p" | "Retitled $p [asana:303]") shape=0 ;; *) shape=1 ;; esac
  check "$p: native rename race leaves a clean title ($t)" "$shape"
  outcomes native "linked this conversation to Asana task 303|did not verify|$busy"; check "$p: racing hooks never claim unverified success" $?

  asana-sessions link --provider "$p" --session "$id" --task 303 > /dev/null 2>&1 || true
  hooks "$p" "$id" "$tr" 303 3 unlink
  unlinked=0; asana-sessions unlink --provider "$p" --session "$id" > /dev/null 2>&1 || unlinked=$?
  wait
  t=$(probe title "$p" "$id")
  case "$t" in "Retitled $p" | "Retitled $p [asana:303]") shape=0 ;; *) shape=1 ;; esac
  check "$p: unlink racing hooks keeps the human title, at most one marker ($t, unlink exit $unlinked)" "$shape"
  outcomes unlink "linked this conversation to Asana task 303|did not verify|$busy"; check "$p: hooks during unlink exit 0" $?
  check "$p: no lock left behind" "$(nolock && echo 0 || echo 1)"
}

if [ "$which" = claude ] || [ "$which" = both ]; then
  id=7d1f6a2e-1c3b-4f5a-9e8d-2b4c6a8e0f13
  tr="$work/claude/projects/p/$id.jsonl"
  printf '{"type":"user","message":{"role":"user","content":"hi"},"uuid":"a1b2c3d4-0000-4000-8000-000000000001","parentUuid":null,"sessionId":"%s","cwd":"%s","timestamp":"2026-10-17T10:00:00.000Z","isSidechain":false,"userType":"external"}\n' "$id" "$work/repo" > "$tr"
  gate claude "$id" "$tr"
fi

if [ "$which" = codex ] || [ "$which" = both ]; then
  [ -n "$real_codex" ] || { echo "codex not found; set REAL_CODEX"; exit 1; }
  # caveman resolves codex by name, so the shim drops itself from PATH.
  printf '#!/bin/sh\nPATH="%s:%s:/usr/bin:/bin" exec caveman codex "$@"\n' "$(dirname "$real_codex")" "$nodebin" > "$work/shims/codex" && chmod +x "$work/shims/codex"
  cp "$source_codex/auth.json" "$work/codex/auth.json" && chmod 600 "$work/codex/auth.json"
  codex exec --skip-git-repo-check -C "$work/repo" "Reply with exactly: OK" < /dev/null > "$work/exec.log" 2>&1 || { cat "$work/exec.log"; exit 1; }
  tr=$(find "$work/codex/sessions" -name 'rollout-*.jsonl' | head -1)
  id=$(basename "$tr" .jsonl | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
  gate codex "$id" "$tr"
fi

[ "$fail" -eq 0 ] && echo "reentrancy gate passed" || { echo "reentrancy gate FAILED"; exit 1; }
