#!/usr/bin/env bash
#
# integration-startup-and-phase-timing.sh — the integration job starts every
# server at once, and the suite says which of its phases used the time.
#
# THE QUEUE RUN TIMED OUT WITH NOTHING TO SAY WHERE. Run 44247455688: servers up
# took 72s, the runner image 14s, and the suite phase was still running when the
# five-minute job budget ended; a passing pull request run of the same commit
# finished it in 71s. Eleven servers started four at a time is three waves of
# image builds and port waits on a shared runner, so every server now starts
# at once in CI. And scripts/suite.sh prints the seconds of its Go test and
# C ABI phases, because the next timeout must name the phase that ate the budget.
#
#   bash scripts/tests/integration-startup-and-phase-timing.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/ci.yml"
fails=0
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; }

job="$(awk '/^  integration:/{on=1;next} on&&/^  [a-z][a-z-]*:/{exit} on{print}' "$WF")"
[ -n "$job" ] || { echo "  FAIL  no integration job found in $WF"; exit 1; }

servers="$(sed -n 's/^ALL_SERVERS="\(.*\)"/\1/p' "$ROOT/scripts/servers.sh" | wc -w | tr -d ' ')"
jobs="$(printf '%s\n' "$job" | sed -n 's/^ *SERVERS_UP_JOBS: *\([0-9][0-9]*\).*/\1/p' | head -1)"
if [ -z "$jobs" ]; then
    fail "the integration job does not set SERVERS_UP_JOBS, so its $servers servers start four at a time"
elif [ "$jobs" -lt "$servers" ]; then
    fail "SERVERS_UP_JOBS is $jobs but the integration job has $servers servers, so some wait for another to finish"
fi

for phase in "go test" "c abi"; do
    grep -qE "phase_done +\"${phase}\"" "$ROOT/scripts/suite.sh" \
        || fail "scripts/suite.sh does not report the seconds of its '${phase}' phase"
done

[ "$fails" -eq 0 ] && echo "  ok    the integration job starts every server at once and the suite times its phases"
exit "$fails"
