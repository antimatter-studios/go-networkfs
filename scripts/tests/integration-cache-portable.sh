#!/usr/bin/env bash
#
# integration-cache-portable.sh — the integration job's Go cache is one that
# every runner kind can restore, whichever kind saved it.
#
# A VPS RUN NEVER FOUND A CACHE A GITHUB RUNNER SAVED (issue #123). Run
# 44506209693 reported "Cache not found" while main held a cache of exactly its
# hash. actions/cache matches a key only within a cache version, and the version
# is a hash of the literal `path:` strings and the compression method:
#
#   - `${{ runner.temp }}/...` expands to /home/runner/work/_temp on GitHub's
#     runners and /home/runner/_work/_temp on the antimatter-vps ones, so the
#     path must be a relative string that reads the same on both;
#   - the vps image has no zstd, so the action fell back to gzip while GitHub's
#     image used zstd, so zstd must be made present before the restore.
#
#   bash scripts/tests/integration-cache-portable.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/ci.yml"

job="$(awk '/^  integration:/{on=1;next} on&&/^  [a-z][a-z-]*:/{exit} on{print}' "$WF")"
[ -n "$job" ] || { echo "  FAIL  no integration job found in $WF"; exit 1; }

fails=0
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; }

# The `path:` of the first step using the named action, joined into one value
# (a block scalar's lines included).
cache_path() {
    printf '%s\n' "$job" | awk -v action="$1" '
        index($0, action){s=1;next}
        s&&/^      - /{exit}
        s&&/^ *path: *[|>]/{p=1;next}
        s&&/^ *path:/{sub(/^ *path: */,"");print;exit}
        p&&(/^ *$/||/^ *[a-z-]+:/){exit}
        p{sub(/^ +/,"");print}'
}

runner_dependent() {
    printf '%s\n' "$1" | grep -qE 'runner\.temp|runner\.workspace|github\.workspace|RUNNER_TEMP|RUNNER_WORKSPACE|GITHUB_WORKSPACE|HOME|^ *[/~]'
}

for step in restore save; do
    p="$(cache_path "actions/cache/$step")"
    if [ -z "$p" ]; then
        fail "the integration job's actions/cache/$step step has no path:"
    elif runner_dependent "$p"; then
        fail "the actions/cache/$step path depends on the runner, so its cache version differs between runner kinds: $(printf '%s' "$p" | tr '\n' ' ')"
    fi
done
restore_path="$(cache_path actions/cache/restore)"
save_path="$(cache_path actions/cache/save)"
[ "$restore_path" = "$save_path" ] \
    || fail "the cache restore and save paths differ, so a saved cache is of another version than the one restored"

# Some step before the restore must make zstd present on a Linux runner: the
# command that checks for it and the install that follows when it is missing.
before_restore="$(printf '%s\n' "$job" | awk '/actions\/cache\/restore/{exit} {print}')"
printf '%s\n' "$before_restore" | grep -qE 'command -v zstd.*apt-get install[^#]*zstd' \
    || fail "no step before the cache restore installs zstd when it is missing, so a runner without it saves and restores gzip, another cache version"

[ "$fails" -eq 0 ] && echo "integration cache: portable across runner kinds, ok"
exit $((fails > 0))
