#!/usr/bin/env bash
#
# siblings-worktree.sh — a rust-fs-core sibling that is a git WORKTREE counts
# as checked out.
#
# Every working copy in this family is a worktree, the sibling at its pinned
# ref included. A worktree's `.git` is a FILE naming its gitdir, so a
# `[ -d "$dir/.git" ]` test reads it as missing, and `chore siblings` then
# runs `git init` + `remote add` over it and dies.
#
# This runs the real `siblings` task body out of chores.yml in a sandbox
# where rust-fs-core is a worktree of a scratch repository at a tag, and
# requires it to be reported present at the pinned ref and left alone. Then
# the same with an ordinary clone, and then with no checkout at all, which
# must still be fetched.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHORES="$ROOT/chores.yml"

EXPECTED_CHECKS=12
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

check_contains() { # DESCRIPTION NEEDLE HAYSTACK
    case "$3" in
        *"$2"*) ok ;;
        *)      fail "$1" "no '$2' in: $(printf '%s' "$3" | tr '\n' '|')" ;;
    esac
}

check_status() { # DESCRIPTION EXPECTED ACTUAL OUTPUT
    if [ "$2" = "$3" ]; then ok; else fail "$1" "exit $3: $(printf '%s' "$4" | tr '\n' '|')"; fi
}

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT

# Nothing from the caller's git configuration reaches the sandbox.
export GIT_CONFIG_GLOBAL="$sandbox/gitconfig" GIT_CONFIG_NOSYSTEM=1
git config --global user.name test
git config --global user.email test@example.invalid
git config --global init.defaultBranch main
git config --global commit.gpgsign false
git config --global tag.gpgsign false

origin="$sandbox/origin"

# --- The task body, as chores.yml has it, pointed at the sandbox. ----------
script="$(awk '
    $0 == "  siblings:" { task = 1; next }
    task && /^  [a-z][a-z:_-]*:$/ { exit }
    task && /^      - \|$/ { inside = 1; next }
    task && inside && /^      - / { exit }
    task && inside { sub(/^        /, ""); print }
' "$CHORES" | sed -E "s#\{\{\.FS_CORE_URL\}\}#$origin#g; s#\{\{\.FS_CORE_REF\}\}#v1#g")"
if [ -n "$script" ]; then ok; else fail "chores.yml has a siblings task with a script body"; fi
case "$script" in
    *'{{'*) fail "every template variable in the siblings task was substituted" ;;
    *)      ok ;;
esac

# --- A scratch upstream: v1 is tagged, main is one commit past it. ---------
git init -q "$origin"
git -C "$origin" commit -q --allow-empty -m one
git -C "$origin" tag v1
git -C "$origin" commit -q --allow-empty -m two
git -C "$origin" remote add origin "$origin"

# The checkout the task runs from; the sibling resolves beside its main tree.
root="$sandbox/root"
git init -q "$root/this"
git -C "$root/this" commit -q --allow-empty -m this
core="$root/rust-fs-core"

run() { (cd "$root/this" && bash -c "$script") 2>&1; }

# --- A worktree at the tag. ------------------------------------------------
git -C "$origin" worktree add -q --detach "$core" v1
if [ -f "$core/.git" ]; then ok; else fail "the fixture's rust-fs-core is a worktree (.git is a file)"; fi
out="$(run)"; rc=$?
check_status "siblings exits 0 when rust-fs-core is a worktree" 0 "$rc" "$out"
check_contains "siblings reports the worktree present at the pin" \
    "siblings: rust-fs-core at or ahead of v1" "$out"
case "$out" in
    *cloning*) fail "siblings did not clone over the worktree" "$out" ;;
    *)         ok ;;
esac
if [ "$(git -C "$core" rev-parse HEAD)" = "$(git -C "$origin" rev-parse v1)" ]; then ok
else fail "the worktree was left at v1"; fi
git -C "$origin" worktree remove --force "$core"

# --- An ordinary clone. ----------------------------------------------------
git clone -q "$origin" "$core"
out="$(run)"; rc=$?
check_status "siblings exits 0 when rust-fs-core is a clone" 0 "$rc" "$out"
check_contains "siblings reports the clone present at the pin" \
    "siblings: rust-fs-core at or ahead of v1" "$out"
rm -rf "$core"

# --- Nothing there. --------------------------------------------------------
out="$(run)"; rc=$?
check_status "siblings exits 0 fetching a missing rust-fs-core" 0 "$rc" "$out"
check_contains "siblings fetches a missing rust-fs-core" "cloning rust-fs-core at v1" "$out"

# --- No other guard in chores.yml asks for a .git DIRECTORY. ---------------
dir_tests="$(grep -nE -- '-d "[^"]*/\.git"' "$CHORES" || true)"
if [ -z "$dir_tests" ]; then ok
else fail "chores.yml asks for no .git directory, which a worktree does not have" "$dir_tests"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'siblings-worktree: all checks passed\n'
