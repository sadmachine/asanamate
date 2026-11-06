#!/bin/sh
# Waits for the named workflow's push run on main for this exact commit and
# fails unless it succeeded, so a tag pushed before or with its commit cannot
# release untested code. Usage: require-ci.sh <workflow file>
set -eu
workflow=$1
for _ in $(seq 1 120); do
  run=$(gh api "repos/$GITHUB_REPOSITORY/actions/workflows/$workflow/runs?head_sha=$GITHUB_SHA&event=push&branch=main" \
    --jq '.workflow_runs | sort_by(.run_number) | last | select(.) | "\(.status) \(.conclusion) \(.html_url)"')
  case $run in
    "completed success "*) echo "$workflow passed: ${run##* }"; exit 0 ;;
    completed*) echo "$workflow did not succeed on $GITHUB_SHA: $run" >&2; exit 1 ;;
  esac
  sleep 30
done
echo "$workflow has no finished push run on main for $GITHUB_SHA" >&2
exit 1
