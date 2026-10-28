#!/bin/sh
# Install or update asanamate from this checkout, then add any new standard
# themes. Extensions update only when already installed; --interactive asks
# which to install. Run it by hand; nothing else calls it.
set -eu

usage() { printf 'Usage: sh ./install.sh [--interactive]\n'; }
interactive=false
case $#:${1-} in
0:) ;;
1:--interactive) interactive=true ;;
1:-h | 1:--help) usage; exit 0 ;;
*) usage >&2; exit 2 ;;
esac

cd "$(dirname -- "$0")"
sessions=$HOME/.local/bin/asana-sessions
packages=./cmd/asanamate
skipped=
failed=false

# confirm QUESTION DEFAULT reads a yes or no answer, using DEFAULT (y or n)
# for an empty one.
confirm() {
    hint='[y/N]'
    [ "$2" = y ] && hint='[Y/n]'
    printf '%s %s ' "$1" "$hint"
    read -r answer || answer=
    case ${answer:-$2} in [Yy]*) return 0 ;; *) return 1 ;; esac
}

# want NAME ID INSTALLED decides whether to install or update an extension.
# Without --interactive, only installed extensions update.
want() {
    if $interactive; then
        default=n
        $3 && default=y
        confirm "Install $1?" "$default"
    elif $3; then
        return 0
    else
        skipped="$skipped  $1: run asanamate extensions $2
"
        return 1
    fi
}

# go_installed PACKAGE reports whether go install already wrote PACKAGE.
go_installed() {
    [ -e "$(go list -f '{{.Target}}' "$1")" ] && echo true || echo false
}

# sessions_managed reports whether install.mjs owns the default command path.
sessions_managed() {
    case $(readlink "$sessions" 2>/dev/null) in
    */lib/asana-sessions/*) echo true ;;
    *) echo false ;;
    esac
}

# install_sessions packs this checkout's asana-sessions and installs it with
# its own installer into ~/.local.
install_sessions() {
    if ! command -v node >/dev/null || ! command -v npm >/dev/null; then
        printf 'asana-sessions needs Node 22 or later and npm\n' >&2
        return 1
    fi
    tmp=$(mktemp -d) || return 1
    archive=$(cd packages/asana-sessions && npm pack --silent --pack-destination "$tmp") &&
        node packages/asana-sessions/install.mjs "$tmp/$archive"
    status=$?
    rm -rf "$tmp"
    return "$status"
}

want asanamate-hrvst harvest "$(go_installed ./cmd/asanamate-hrvst)" &&
    packages="$packages ./cmd/asanamate-hrvst"
want asanamate-ccmux ccmux "$(go_installed ./cmd/asanamate-ccmux)" &&
    packages="$packages ./cmd/asanamate-ccmux"
managed=$(sessions_managed)
other=$(command -v asana-sessions || true)
if ! $managed && [ -n "$other" ]; then
    printf 'Note: %s is not managed by this installer; remove it before\n' "$other"
    printf 'installing asana-sessions into %s.\n' "$sessions"
fi
want_sessions=false
want asana-sessions asana-sessions "$managed" && want_sessions=true

go install $packages
sh ./install-themes.sh

if $want_sessions; then
    if ! install_sessions; then
        printf 'asana-sessions was not installed\n' >&2
        failed=true
    elif $interactive; then
        "$sessions" setup || printf 'Run asana-sessions doctor to finish plugin setup.\n'
    else
        "$sessions" doctor || true
    fi
fi

printf 'Installed %s:\n' "$(git describe --match 'v*' --always --dirty 2>/dev/null || echo 'this checkout')"
go list -f '  {{.Target}}' $packages
if $want_sessions && ! $failed; then
    printf '  %s\n' "$sessions"
    case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) printf 'Add %s to PATH, before any other asana-sessions.\n' "$HOME/.local/bin" ;;
    esac
fi
if [ -n "$skipped" ]; then
    printf 'Not installed:\n%s' "$skipped"
fi
$interactive || printf 'Run sh ./install.sh --interactive to choose extensions.\n'
! $failed
