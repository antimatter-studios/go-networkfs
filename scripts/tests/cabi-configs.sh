#!/usr/bin/env bash
#
# cabi-configs.sh — the C ABI build list and the C ABI config list cannot
# drift, and no harness can report success having run nothing.
#
# TWO LISTS THAT HAD TO AGREE AND NOTHING THAT CHECKED THEM. scripts/cabi.sh
# builds one harness per name in DRIVERS and mounts it with whatever
# config_for() hands back. That function's default arm used to hand back an
# empty string, and an empty CABI_CONFIG makes a harness print "skipping the
# mounted tests" and exit 0 — so a ninth driver added to DRIVERS and not to
# config_for() produced a PASSING job with that driver's entire success path
# untested: openfile, writefile, and the ByteSlice hand-back that is the
# boundary's whole contract. Issue #6.
#
# AND NO HARNESS COUNTED WHAT IT RAN. `main` returned `failures == 0 ? 0 : 1`,
# so one that executed forty checks and one that executed none exited
# identically. The floors are what tell those apart, and they are two numbers
# because a skip is right on a laptop with no servers and wrong in the job
# whose reason for existing is that the servers are up.
#
# THIS GUARD NEEDS NO COMPILER AND NO SERVER. `cabi.sh configs` is the config
# check with the building taken out precisely so it can be driven from here;
# the floors are read out of the harness sources. What the numbers themselves
# are worth was measured by running the s3 harness both ways — 17 without a
# config, 28 with — and that measurement is recorded beside them in the C.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CABI="$ROOT/scripts/cabi.sh"

EXPECTED_CHECKS=26
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

# --- The list as shipped has a config for every driver. --------------------
out="$(bash "$CABI" configs 2>&1)"; rc=$?
check_status "cabi.sh configs passes over the shipped DRIVERS" 0 "$rc" "$out"

# --- A driver with no config stops the run, by name. -----------------------
out="$(DRIVERS="ftp bogus" bash "$CABI" configs 2>&1)"; rc=$?
check_status "a driver with no config case fails the run" 1 "$rc" "$out"
check_contains "and the failure names the driver" "bogus" "$out"
check_contains "and says where to add it" "config_for" "$out"
check_contains "and where to take it out of" "DRIVERS" "$out"

# --- It refuses BEFORE anything is built. ----------------------------------
#
# Reaching config_for() through cabi_drivers costs a c-archive build per
# driver first, so a one-line omission would take minutes to report. The
# check-up-front is the difference, and `configs` is how it is reachable
# without a compiler at all.
if grep -q 'check_configs' "$CABI"; then ok
else fail "cabi.sh checks every config before it builds anything"; fi
body="$(awk '/^cabi_drivers\(\) \{/,/^\}/' "$CABI")"
case "$body" in
    *check_configs*) ok ;;
    *) fail "cabi_drivers checks the configs first" "$(printf '%s' "$body" | head -4 | tr '\n' '|')" ;;
esac

# The default arm must FAIL rather than hand back an empty string.
arm="$(grep -A1 -- '        \*)' "$CABI" | head -2)"
case "$arm" in
    *die*) ok ;;
    *) fail "config_for's default arm dies" "$arm" ;;
esac

# --- Every harness carries both floors and asserts on them. ----------------
harnesses=0
for h in "$ROOT"/test/cabi/test_*.c; do
    harnesses=$((harnesses + 1))
    name="$(basename "$h")"

    if grep -q '#define EXPECTED_CHECKS_OFFLINE' "$h" \
       && grep -q '#define EXPECTED_CHECKS_MOUNTED' "$h"; then :
    else fail "$name declares both check floors"; continue; fi

    if grep -q 'checks != expected' "$h"; then :
    else fail "$name fails a run whose check count is not the expected one"; continue; fi

    # The skip is a failure under CI, where the servers are the reason the job
    # exists.
    if grep -q 'GITHUB_ACTIONS' "$h"; then :
    else fail "$name refuses the skip when the runner is CI"; continue; fi

    ok
done

# A glob that matched nothing would have run the loop zero times and left
# every one of those checks unmade — the same absence this whole file is about.
if [ "$harnesses" -eq 9 ]; then ok
else fail "all nine harnesses were read" "found $harnesses"; fi

# --- The eight driver harnesses agree on their numbers. --------------------
#
# They are the same file with a prefix substituted, so a floor raised in one
# and not the others is a mistake rather than a decision.
offline=""; mounted=""
for h in "$ROOT"/test/cabi/test_*.c; do
    case "$(basename "$h")" in test_networkfs.c) continue ;; esac
    offline="$offline $(awk '/#define EXPECTED_CHECKS_OFFLINE/ {print $3}' "$h")"
    mounted="$mounted $(awk '/#define EXPECTED_CHECKS_MOUNTED/ {print $3}' "$h")"
done
if [ "$(printf '%s\n' $offline | sort -u | wc -l)" -eq 1 ]; then ok
else fail "the eight driver harnesses share one offline floor" "$offline"; fi
if [ "$(printf '%s\n' $mounted | sort -u | wc -l)" -eq 1 ]; then ok
else fail "the eight driver harnesses share one mounted floor" "$mounted"; fi

# The measured numbers, so a silent edit of either is a failure here and not
# only in a job that needs six containers to run.
if [ "$(printf '%s\n' $offline | sort -u)" = "17" ]; then ok
else fail "the driver harnesses' offline floor is the measured 17" "$offline"; fi
if [ "$(printf '%s\n' $mounted | sort -u)" = "28" ]; then ok
else fail "the driver harnesses' mounted floor is the measured 28" "$mounted"; fi

# The unified harness links all eight drivers and so has its own pair,
# measured the same way: 26 with SMB_HOST unset, 44 with Samba up.
u="$ROOT/test/cabi/test_networkfs.c"
if [ "$(awk '/#define EXPECTED_CHECKS_OFFLINE/ {print $3}' "$u")" = "26" ]; then ok
else fail "the unified harness's offline floor is the measured 26" \
          "$(grep EXPECTED_CHECKS_OFFLINE "$u")"; fi
if [ "$(awk '/#define EXPECTED_CHECKS_MOUNTED/ {print $3}' "$u")" = "44" ]; then ok
else fail "the unified harness's mounted floor is the measured 44" \
          "$(grep EXPECTED_CHECKS_MOUNTED "$u")"; fi

# --- CI reaches the harnesses at all. --------------------------------------
#
# They run inside the runner container, where nothing about the outer runner
# is visible unless it is passed in — so the refusal above would never fire in
# the one job it is for.
if grep -q -- '-e CI=' "$ROOT/scripts/docker-suite.sh"; then ok
else fail "docker-suite.sh forwards CI into the runner container" \
          "the harnesses' CI refusal cannot fire in the job it exists for"; fi
if grep -q -- '-e GITHUB_ACTIONS=' "$ROOT/scripts/docker-suite.sh"; then ok
else fail "docker-suite.sh forwards GITHUB_ACTIONS into the runner container"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'cabi-configs: all checks passed\n'
