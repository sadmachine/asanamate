#!/bin/sh
# Minimal pull requests provider for the protocol test (docs/extensions.md),
# for a Gitea host, git.example.com. It appends each request to the file
# named by $1, then answers by branch. A real provider reads the JSON; this
# one matches text to stay short. It skips repos on other hosts, which
# another provider in pull_requests.commands serves.
request=$(cat)
printf '%s\n' "$request" >> "$1"
case $request in
*'"operation":"lookup"'*) ;;
*)
	echo "unknown operation" >&2
	exit 1
	;;
esac
case $request in
*'"branch":"feature/login","repos":[{"path":"/code/web","remotes":[{"name":"origin","url":"https://git.example.com/acme/web.git"}'*)
	printf '%s\n' '{"pull_requests":[
	  {"ticket_gid":"1201","url":"https://git.example.com/acme/web/pulls/42","number":42,
	   "title":"Fix login redirect","state":"open","checks":"failing","review":"changes",
	   "author":"zed","updated_at":"2026-11-05T14:05:00Z","repo":"/code/web","branch":"feature/login"}]}'
	;;
*)
	printf '%s\n' '{"pull_requests":[]}'
	;;
esac
