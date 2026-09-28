#!/usr/bin/env bash
#
# install-hooks.sh — installing this repository's hook must not switch another
# repository guard off.
#
# `core.hooksPath` REPLACES .git/hooks: git looks in one place or the other,
# never both. github-guard installs into .git/hooks deliberately — outside the
# working tree, so no branch can rewrite them — and its own installer clears
# `core.hooksPath` for exactly this reason. Two installers competing for one
# pointer means the loser is whichever ran first, and nothing prints either
# way: a contributor who ran both scripts believes they have both sets of
# hooks and has one. Issue #14.
#
# So scripts/install-hooks.sh must put its checks in the hooks DIRECTORY, and
# must never set the pointer. github-guard's dispatcher runs every executable
# in <hooks>/<hook>.d/ in lexical order, which is the chain point to use when
# it is there; a clone without github-guard gets the hook itself.
#
# EVERY CASE IS RUN IN A SCRATCH REPOSITORY. Nothing here touches the caller's
# clone, its config or its hooks.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

EXPECTED_CHECKS=17
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

export GIT_CONFIG_GLOBAL="$sandbox/gitconfig" GIT_CONFIG_NOSYSTEM=1
git config --global user.name test
git config --global user.email test@example.invalid
git config --global init.defaultBranch main

# --- A scratch clone carrying the two files the installer reads. -----------
make_clone() { # $1 = path
    rm -rf "$1"
    mkdir -p "$1/scripts" "$1/.githooks"
    git init -q "$1"
    cp "$ROOT/scripts/install-hooks.sh" "$1/scripts/install-hooks.sh"
    cp "$ROOT/.githooks/pre-commit" "$1/.githooks/pre-commit"
    chmod +x "$1/scripts/install-hooks.sh" "$1/.githooks/pre-commit"
    git -C "$1" add -A
    git -C "$1" commit -q -m init
}

# github-guard's dispatcher, in the shape its installer leaves behind: a stub
# that names itself and execs lib/run-guards.sh over <hook>.d/.
plant_github_guard() { # $1 = clone path
    mkdir -p "$1/.git/hooks/lib" "$1/.git/hooks/pre-commit.d"
    cat > "$1/.git/hooks/pre-commit" <<'STUB'
#!/usr/bin/env bash
# github-guard hook: pre-commit
d=$(cd "$(dirname "$0")" && pwd)
exec "$d/lib/run-guards.sh" "$(basename "$0")" "$@"
STUB
    cat > "$1/.git/hooks/lib/run-guards.sh" <<'RUN'
#!/usr/bin/env bash
set -u
hook=${1:-}; shift
dir=$(cd "$(dirname "$0")/.." && pwd)
for g in "$dir/$hook.d"/*; do
    [ -f "$g" ] && [ -x "$g" ] || continue
    "$g" "$@" || exit 1
done
RUN
    chmod +x "$1/.git/hooks/pre-commit" "$1/.git/hooks/lib/run-guards.sh"
}

run_install() { (cd "$1" && ./scripts/install-hooks.sh) 2>&1; }

# --- THE DEFECT: the pointer is never set, in any case. --------------------
clone="$sandbox/plain"
make_clone "$clone"
out="$(run_install "$clone")"; rc=$?
check_status "install-hooks.sh succeeds in a fresh clone" 0 "$rc" "$out"

hooks_path="$(git -C "$clone" config --get core.hooksPath || true)"
if [ -z "$hooks_path" ]; then ok
else fail "install-hooks.sh leaves core.hooksPath unset" \
          "set to '$hooks_path', which replaces .git/hooks wholesale"; fi

# --- Without github-guard, the hook itself is installed. -------------------
if [ -x "$clone/.git/hooks/pre-commit" ]; then ok
else fail "a clone with no dispatcher gets .git/hooks/pre-commit"; fi
if grep -q 'gofmt -l -s' "$clone/.git/hooks/pre-commit" 2>/dev/null; then ok
else fail "the installed hook is the tracked one's body"; fi

# It has to actually run, so a real commit is the check rather than the file
# being present: an installed hook that git will not execute is not installed.
printf 'package main\n\nfunc  main ()  {}\n' > "$clone/bad.go"
git -C "$clone" add bad.go
out="$(git -C "$clone" commit -m "unformatted" 2>&1)"; rc=$?
check_status "the installed hook blocks an unformatted commit" 1 "$rc" "$out"
check_contains "and says which file it objected to" "bad.go" "$out"
git -C "$clone" reset -q

# --- An existing core.hooksPath is CLEARED, not left to win. ---------------
clone="$sandbox/pointed"
make_clone "$clone"
git -C "$clone" config core.hooksPath .githooks
out="$(run_install "$clone")"; rc=$?
check_status "install-hooks.sh succeeds over an existing core.hooksPath" 0 "$rc" "$out"
hooks_path="$(git -C "$clone" config --get core.hooksPath || true)"
if [ -z "$hooks_path" ]; then ok
else fail "an existing core.hooksPath is cleared" "still '$hooks_path'"; fi
check_contains "and it says so rather than doing it silently" "core.hooksPath" "$out"

# --- With github-guard present, its dispatcher is not overwritten. ---------
clone="$sandbox/guarded"
make_clone "$clone"
plant_github_guard "$clone"
out="$(run_install "$clone")"; rc=$?
check_status "install-hooks.sh succeeds alongside github-guard" 0 "$rc" "$out"

if grep -q 'github-guard' "$clone/.git/hooks/pre-commit"; then ok
else fail "github-guard's pre-commit stub is left in place" \
          "$(head -3 "$clone/.git/hooks/pre-commit" | tr '\n' '|')"; fi

installed="$(ls "$clone/.git/hooks/pre-commit.d" 2>/dev/null | tr '\n' ' ')"
case "$installed" in
    *go*) ok ;;
    *)    fail "the fast checks are installed into pre-commit.d/" "holds: $installed" ;;
esac

# Both sets run: github-guard's chain reaches this repository's check.
printf 'package main\n\nfunc  main ()  {}\n' > "$clone/bad.go"
git -C "$clone" add bad.go
out="$(git -C "$clone" commit -m "unformatted" 2>&1)"; rc=$?
check_status "the dispatcher runs this repository's check too" 1 "$rc" "$out"
check_contains "and it is the gofmt one that objected" "bad.go" "$out"
git -C "$clone" reset -q

# --- Running it twice changes nothing. -------------------------------------
before="$(ls "$clone/.git/hooks/pre-commit.d")"
out="$(run_install "$clone")"; rc=$?
check_status "install-hooks.sh is idempotent" 0 "$rc" "$out"
if [ "$before" = "$(ls "$clone/.git/hooks/pre-commit.d")" ]; then ok
else fail "a second run installs nothing new"; fi

# --- Nothing tracked still tells anybody to set the pointer. ---------------
# An INSTRUCTION, not a mention: `git config core.hooksPath <value>` is the
# thing that breaks a clone. Prose explaining why it is not set has to stay
# sayable, and `--unset`/`--get` are what this script and a diagnosis use.
told="$(grep -rnE 'git config +core\.hooksPath +[^ ]' \
        "$ROOT/README.md" "$ROOT/.githooks" "$ROOT/docs" "$ROOT/AGENTS.md" 2>/dev/null \
        | grep -v -- '--unset' | grep -v -- '--get' || true)"
if [ -z "$told" ]; then ok
else fail "no tracked file tells a contributor to set core.hooksPath" "$told"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'install-hooks: all checks passed\n'
