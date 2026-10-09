#!/usr/bin/env bash
#
# fuzzing.sh — the fuzzing setup is two tiers, and each has a way of going
# quietly missing.
#
# THE DETERMINISTIC TIER IS INVISIBLE, which is its whole appeal and its whole
# risk. Go replays every f.Add seed and every file in testdata/fuzz/<Target>/
# as an ordinary unit test, so the corpus gate the Rust siblings had to build
# by hand runs inside `chore test:unit` on every pull request with nothing
# naming it. A target with NO seeds still passes that run, having executed the
# property against nothing at all — which reads exactly like a target that
# passed. Issue #17.
#
# THE EXPLORER TIER CAN BE MADE UNREACHABLE by a one-line edit to its workflow,
# and the reverse — making it a required check — is worse: it runs on a
# schedule, so it can never report on a pull request, and a required check that
# never reports is a permanent block rather than a gate.
#
# THE FLOOR IS HERE FOR THE SAME REASON EVERY FLOOR IN THIS REPOSITORY IS. A
# glob that matched nothing runs its loop zero times and reports no failures.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW="$ROOT/.github/workflows/fuzz.yml"
RUNNER="$ROOT/scripts/fuzz.sh"
GUARD="$ROOT/.github-guard"

# Measured 2026-09-28: eight targets over pkg/fsutil, s3 and gdrive. A floor,
# not a target count — it moves UP when targets are added and never down.
MINIMUM_TARGETS=8

EXPECTED_CHECKS=13
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# --- There are targets, and enough of them. --------------------------------
targets="$(grep -rhoE '^func (Fuzz[A-Za-z0-9_]+)\(' --include='*_test.go' "$ROOT" \
           | sed -E 's/^func //; s/\($//' | sort -u)"
count="$(printf '%s\n' "$targets" | grep -c . || true)"

if [ "${count:-0}" -ge "$MINIMUM_TARGETS" ]; then ok
else fail "the module carries at least $MINIMUM_TARGETS fuzz targets" \
          "found ${count:-0}: $(echo $targets)"; fi

# --- Every target has a seed. ----------------------------------------------
#
# Without one the deterministic tier executes the property against nothing, and
# a plain `go test` reports that as a pass.
seedless=""
for target in $targets; do
    file="$(grep -rlE "^func $target\(" --include='*_test.go' "$ROOT" | head -1)"
    body="$(awk "/^func $target\(/,/^}/" "$file")"
    case "$body" in
        *f.Add*) ;;
        *) seedless="$seedless $target" ;;
    esac
done
if [ -z "$seedless" ]; then ok
else fail "every fuzz target seeds itself with f.Add" \
          "no seeds in:$seedless — the corpus replay would run them against nothing"; fi

# --- The explorer exists and is runnable. ----------------------------------
if [ -x "$RUNNER" ]; then ok
else fail "scripts/fuzz.sh exists and is executable"; fi

if [ -f "$WORKFLOW" ]; then ok
else fail ".github/workflows/fuzz.yml exists to run the explorer"; fi

if grep -q 'schedule:' "$WORKFLOW" 2>/dev/null; then ok
else fail "the fuzz workflow runs on a schedule" \
          "an explorer nothing triggers is an explorer that never explores"; fi

if grep -q 'workflow_dispatch:' "$WORKFLOW" 2>/dev/null; then ok
else fail "the fuzz workflow can be dispatched by hand"; fi

# It must NOT run per pull request: a forty-minute job on every push is how a
# schedule gets deleted rather than tuned.
if grep -q 'pull_request' "$WORKFLOW" 2>/dev/null; then
    fail "the fuzz workflow does not run on pull_request" \
         "forty minutes a push; the corpus replay is the per-PR tier"
else ok; fi

# --- The explorer runs on a Go without the fuzztime race. -----------------
#
# go1.26's fuzzer can report a clean run as a failure when -fuzztime expires:
# a worker error in the window between the deadline and the cancellation of
# its child context is not suppressed (go.dev/issue/75804). The nightly failed
# that way on 2026-10-09 with no input written (run 37917988928, #55). The fix,
# golang/go@5a957dc766, is in go1.27 and was never backported to 1.26. So while
# go.mod is below 1.27, fuzz.yml must install the explorer's Go itself, at 1.27
# or later; the corpus replay in `chore test:unit` stays on go.mod's.
go_minor() { # 1.27.1 -> 27; anything not 1.x -> 0
    case "$1" in 1.*) v="${1#1.}"; v="${v%%.*}"; [ -n "$v" ] && echo "$v" || echo 0 ;; *) echo 0 ;; esac
}
mod_go="$(sed -n 's/^go \([0-9][0-9.]*\)$/\1/p' "$ROOT/go.mod")"
explorer_go="$(sed -n "s/^ *go-version: *['\"]\{0,1\}\([0-9][0-9.]*\)['\"]\{0,1\} *$/\1/p" "$WORKFLOW" | head -1)"
if [ -z "$explorer_go" ] && grep -q 'go-version-file: *go.mod' "$WORKFLOW" 2>/dev/null; then
    explorer_go="$mod_go"
fi
if [ "$(go_minor "${explorer_go:-0}")" -ge 27 ]; then ok
else fail "the fuzz explorer runs on go1.27 or later" \
          "go.mod is ${mod_go:-?} and fuzz.yml installs ${explorer_go:-nothing it names}: go.dev/issue/75804 fails a clean run whose fuzztime expires"; fi

# --- And it is never a required check. -------------------------------------
#
# A schedule-driven workflow can never report on a pull request, so requiring
# it would block every merge forever.
if grep -qi 'fuzz' "$GUARD" 2>/dev/null; then
    fail ".github-guard does not require the fuzz job" \
         "$(grep -in fuzz "$GUARD" | tr '\n' '|') — it can never report on a PR"
else ok; fi

# --- The runner refuses rather than passing vacuously. ---------------------
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/scripts"
cp "$RUNNER" "$sandbox/scripts/fuzz.sh" 2>/dev/null

(cd "$sandbox" && bash scripts/fuzz.sh 1 >/dev/null 2>&1)
if [ $? -ne 0 ]; then ok
else fail "fuzz.sh fails in a tree with no targets rather than reporting a clean run"; fi

out="$(bash "$RUNNER" 1 FuzzThisDoesNotExist 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then ok
else fail "fuzz.sh fails when asked for a target that does not exist" "$out"; fi
case "$out" in
    *FuzzThisDoesNotExist*) ok ;;
    *) fail "and the refusal names what was asked for" "$out" ;;
esac

# --- Any committed regression seed is where go looks for it. ---------------
#
# go reads testdata/fuzz/<Target>/ relative to the PACKAGE. A seed filed under
# the repository root would be replayed by nothing.
stray="$(find "$ROOT" -path "$ROOT/tmp" -prune -o -type d -name fuzz -print 2>/dev/null \
         | grep -v '/testdata/fuzz$' || true)"
if [ -z "$stray" ]; then ok
else fail "every corpus directory is a package's testdata/fuzz" "$stray"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'fuzzing: all checks passed\n'
