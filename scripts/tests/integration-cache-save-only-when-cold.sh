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

[ "$fails" -eq 0 ] && echo "integration cache save: only when cold, ok"
exit $((fails > 0))
