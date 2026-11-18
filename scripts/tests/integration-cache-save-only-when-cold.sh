#!/usr/bin/env bash
#
# integration-cache-save-only-when-cold.sh — the integration job saves its Go
# cache only when the restore found nothing to start from.
#
# THE WARM RUN TIMED OUT SAVING A CACHE IT DID NOT NEED. Run 44243388587: the
# tests passed in 2m45s, then actions/cache/save uploaded 230MB for 2m32s and
# the five-minute job budget was spent. Every run saves, because the key carries
# the run id. A run that restored a cache has nothing new worth that cost; a
# cold run, which the save step exists for, is the one that must keep it.
#
#   bash scripts/tests/integration-cache-save-only-when-cold.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/ci.yml"

# The lines of the integration job's steps: from `  integration:` to the next
# job at the same indent.
job="$(awk '/^  integration:/{on=1;next} on&&/^  [a-z][a-z-]*:/{exit} on{print}' "$WF")"
[ -n "$job" ] || { echo "  FAIL  no integration job found in $WF"; exit 1; }

fails=0
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; }

restore_id="$(printf '%s\n' "$job" | awk '/actions\/cache\/restore/{r=1;next} r&&/^ *id:/{print $2;exit} r&&/^      - /{exit}')"
save="$(printf '%s\n' "$job" | awk '/actions\/cache\/save/{s=1} s&&/^      - /&&!/cache\/save/{exit} s{print}')"

[ -n "$restore_id" ] || fail "the cache restore step has no id, so the save step cannot ask whether it hit"
[ -n "$save" ] || fail "the integration job has no cache save step"
if [ -n "$restore_id" ] && ! printf '%s\n' "$save" | grep -qE "^ *if:.*steps\.${restore_id}\.outputs\.cache-matched-key *== *''"; then
    fail "the cache save step runs even when the restore found a cache; its if: must test steps.${restore_id}.outputs.cache-matched-key == ''"
fi
printf '%s\n' "$save" | grep -qE '^ *if:.*(always\(\)|!cancelled\(\))' \
    || fail "the cache save step must still run after a failing or timed-out test, so a cold run keeps what it compiled"

# A CHANGED HASH MUST NOT START COLD. Queue run 44245171396 found no cache for
# its go.mod/go.sum/Dockerfile hash, had no broader key to fall back on, and
# spent 307s of a 300s budget compiling from nothing. The restore keys are tried
# in order, so the hash-specific one stays first and the os/arch prefix follows.
restore_keys="$(printf '%s\n' "$job" | awk '
    /actions\/cache\/restore/{r=1;next}
    r&&/^      - /{exit}
    r&&/^ *restore-keys:/{k=1;next}
    k&&(/^ *$/||/^ *[a-z-]+:/){exit}
    k{sub(/^ +/,"");print}')"
broad='${{ runner.os }}-${{ runner.arch }}-networkfs-integration-'
broad_line="$(printf '%s\n' "$restore_keys" | grep -nxF -- "$broad" | head -1 | cut -d: -f1)"
hashed_line="$(printf '%s\n' "$restore_keys" | grep -n 'hashFiles' | head -1 | cut -d: -f1)"
if [ -z "$broad_line" ]; then
    fail "the cache restore has no restore-keys entry of only the os/arch prefix, so a changed hash starts cold: want $broad"
elif [ -z "$hashed_line" ] || [ "$hashed_line" -gt "$broad_line" ]; then
    fail "the hash-specific restore key must come before the os/arch prefix, or an older cache beats an exact match"
fi

# A CACHE OF AN OLDER HASH MUST BE REPLACED. With the os/arch prefix any cache
# counts as a hit, so a save gated only on an empty match would never store the
# new hash's cache and every later run would recompile the difference.
printf '%s\n' "$save" | grep -qE "^ *if:.*contains\(steps\.${restore_id}\.outputs\.cache-matched-key, *hashFiles" \
    || fail "the cache save step must also run when the matched key is not of the current hash"

[ "$fails" -eq 0 ] && echo "integration cache save: only when cold, ok"
exit $((fails > 0))
