#!/usr/bin/env bash
#
# fuzz.sh — the explorer tier: every fuzz target in the module, each on a
# bounded budget.
#
#   fuzz.sh              every target, 60s each
#   fuzz.sh 300          every target, 300s each
#   fuzz.sh 300 FuzzNormPath   just that one
#
# WHY THIS IS A SEPARATE TIER AND NOT A TEST TASK. `go test -fuzz` can only
# fuzz ONE target per package per invocation, and it runs until its budget
# expires rather than until it is done — so it is a loop over targets with a
# clock, which is not something `go test ./...` expresses.
#
# THE DETERMINISTIC HALF IS NOT HERE, AND THAT IS THE POINT. Go replays every
# f.Add seed and every file in testdata/fuzz/<Target>/ as an ordinary unit test
# during a plain `go test`, so the corpus gate the Rust siblings had to build
# by hand is already running in `chore test:unit` on every pull request. This
# task is only the explorer. Issue #17.
#
# ANYTHING IT FINDS IS WRITTEN INTO testdata/fuzz/<Target>/ by go itself, as a
# regression seed. Commit it: from then on the plain test run reproduces the
# failure and no explorer is needed to see it again.
#
# QUIET. `go test -fuzz` prints a progress line every three seconds, which is
# 20 lines a minute a target and nothing worth reading on a pass. The run goes
# to tmp/logs/fuzz.log and each target prints one verdict; a failure prints the
# tail. OUTPUT_BUDGET_VERBOSE=1 streams it.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

SECONDS_PER_TARGET="${1:-60}"
ONLY="${2:-}"

LOG_DIR="tmp/logs"
LOG="$LOG_DIR/fuzz.log"
mkdir -p "$LOG_DIR"
: > "$LOG"

VERBOSE="${OUTPUT_BUDGET_VERBOSE:-0}"

# THE TARGETS ARE DISCOVERED, NOT LISTED. A hand-kept list is a list that goes
# stale the first time somebody adds a target and forgets — and the failure
# would be a fuzz run that quietly explored less than it claimed, which is the
# absence this repository keeps meeting in other shapes.
targets="$(grep -rhoE '^func (Fuzz[A-Za-z0-9_]+)\(' --include='*_test.go' . \
           | sed -E 's/^func //; s/\($//' | sort -u)"

found=0
failed=0
ran=0

for target in $targets; do
    found=$((found + 1))
    [ -n "$ONLY" ] && [ "$target" != "$ONLY" ] && continue

    pkg="$(grep -rlE "^func $target\(" --include='*_test.go' . | head -1)"
    pkg="./$(dirname "${pkg#./}")"

    printf 'fuzz: %s in %s for %ss\n' "$target" "$pkg" "$SECONDS_PER_TARGET" >> "$LOG"
    if [ "$VERBOSE" = 1 ]; then
        go test "$pkg" -run '^$' -fuzz "^$target\$" -fuzztime "${SECONDS_PER_TARGET}s" 2>&1 | tee -a "$LOG"
        status=${PIPESTATUS[0]}
    else
        go test "$pkg" -run '^$' -fuzz "^$target\$" -fuzztime "${SECONDS_PER_TARGET}s" >> "$LOG" 2>&1
        status=$?
    fi
    ran=$((ran + 1))

    if [ "$status" -eq 0 ]; then
        printf '  %-32s ok\n' "$target"
    else
        printf '  %-32s FAILED — see %s\n' "$target" "$LOG"
        failed=$((failed + 1))
    fi
done

# A GLOB THAT MATCHED NOTHING IS A RUN THAT PROVED NOTHING. With no targets the
# loop never executes and this task would exit 0 having fuzzed nothing at all,
# which reads exactly like a clean run.
if [ "$found" -eq 0 ]; then
    echo "fuzz.sh: no Fuzz* targets found — this task explored nothing." >&2
    exit 1
fi
if [ -n "$ONLY" ] && [ "$ran" -eq 0 ]; then
    echo "fuzz.sh: no target named '$ONLY' (found: $(echo $targets | tr '\n' ' '))" >&2
    exit 1
fi

if [ "$failed" -gt 0 ]; then
    echo "fuzz: $ran targets, $failed failed — $LOG"
    echo "      go wrote the failing input into testdata/fuzz/<target>/; commit it."
    [ "$VERBOSE" = 1 ] || tail -30 "$LOG"
    exit 1
fi

echo "fuzz: $ran targets, ${SECONDS_PER_TARGET}s each, no failures — $LOG"
