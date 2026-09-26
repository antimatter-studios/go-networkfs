#!/usr/bin/env bash
#
# output-budget-resolver.sh — the contract between this repository and
# rust-fs-core's output-budget wrapper, executed rather than asserted in prose.
#
# WHAT THIS IS FOR. The wrapper is no longer committed here; scripts/tier.sh
# resolves it from a pinned sibling checkout at run time and refuses when it
# cannot. A refusal path nobody executes has never been shown to happen — it
# is a branch that has only ever been read — and the two that matter here are
# exactly the two a developer meets: no sibling at all, and a sibling holding
# something that is not the wrapper. Both are driven below through
# FS_CORE_ROOT, which exists so they can be.
#
# IT DOES NOT SKIP. If the sibling is missing, this FAILS and names
# `chore siblings`. A guard that quietly declines to run reads exactly like a
# guard that passed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TIER="$ROOT/scripts/tier.sh"
RESOLVER="$ROOT/scripts/resolve-output-budget.sh"
EXPECTED_API="rust-fs-core-output-budget 1"

# Every check below increments this, and the count is asserted at the end. An
# `exit 0` reached early — or a `return` from a helper that was supposed to
# run four more assertions — gives status 0 having proved a prefix of the
# contract, with nothing failing and nothing saying so.
EXPECTED_CHECKS=13
checks=0
fails=0

ok()   { checks=$((checks + 1)); printf '  ok    %s\n' "$1"; }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

check_eq() { # DESCRIPTION EXPECTED ACTUAL
    if [ "$2" = "$3" ]; then ok "$1"; else fail "$1" "expected '$2', got '$3'"; fi
}

check_contains() { # DESCRIPTION NEEDLE HAYSTACK
    case "$3" in
        *"$2"*) ok "$1" ;;
        *)      fail "$1" "no '$2' in: $(printf '%s' "$3" | tr '\n' '|')" ;;
    esac
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

printf 'output-budget-resolver\n'

# ---------------------------------------------------------------------------
# The sibling itself. Everything below depends on it, so it is checked first
# and its absence is a failure with an instruction, not a reason to stop.
# ---------------------------------------------------------------------------
resolved="$(bash "$RESOLVER" 2>"$tmp/resolve.err")"
if [ -n "$resolved" ] && [ -f "$resolved" ]; then
    ok "the pinned sibling supplies scripts/output-budget.sh"
else
    fail "the pinned sibling supplies scripts/output-budget.sh" \
         "run 'chore siblings' — $(tr '\n' ' ' < "$tmp/resolve.err")"
fi

check_eq "the resolved script answers the API this repository calls" \
    "$EXPECTED_API" "$(bash "$resolved" --version 2>/dev/null)"

# The copy is transient. Whatever is lying in tmp/ now — `chore test` runs one
# tier inside another, so a live copy here is legitimate — must be what is
# lying there when this file is done.
copies_before="$(find "$ROOT/tmp" -maxdepth 1 -name 'output-budget.*' 2>/dev/null | sort)"

# ---------------------------------------------------------------------------
# Refusal 1: no core at all.
# ---------------------------------------------------------------------------
mkdir -p "$tmp/empty-core"
out="$(FS_CORE_ROOT="$tmp/empty-core" bash "$TIER" probe probe 40 3000 -- true 2>&1)"
rc=$?
check_eq "an absent core fails the tier" 1 "$rc"
check_contains "and names the path it looked at" "$tmp/empty-core" "$out"
check_contains "and names the minimum core version" "v0.2.13" "$out"
check_contains "and names the command that fixes it" "chore siblings" "$out"

# ---------------------------------------------------------------------------
# Refusal 2: core is there, and what is in it is not the wrapper.
#
# THIS IS THE ONE THAT MUST NOT FALL BACK. The real sibling is present on this
# machine — the first check proved it — so a resolver that treated a bad
# FS_CORE_ROOT as "look elsewhere" would pass every other check in this file
# while silently running a wrapper nobody asked for.
# ---------------------------------------------------------------------------
mkdir -p "$tmp/wrong-core/scripts"
printf '#!/usr/bin/env bash\necho "some-other-budget 9"\n' \
    > "$tmp/wrong-core/scripts/output-budget.sh"
chmod +x "$tmp/wrong-core/scripts/output-budget.sh"

out="$(FS_CORE_ROOT="$tmp/wrong-core" bash "$TIER" probe probe 40 3000 -- true 2>&1)"
rc=$?
check_eq "a present-but-wrong core fails the tier" 1 "$rc"
check_contains "and says what it answered instead" "some-other-budget 9" "$out"

# ---------------------------------------------------------------------------
# The tier contract, against the real wrapper.
# ---------------------------------------------------------------------------

# A pass is ONE line, on stdout, naming the log.
out="$(bash "$TIER" probe probe 40 3000 -- sh -c 'echo hello' 2>&1)"
rc=$?
if [ "$rc" -eq 0 ] && [ "$(printf '%s\n' "$out" | wc -l | tr -d ' ')" = 1 ] \
   && [ "${out#probe: ok (1 lines, 6 bytes) — }" != "$out" ] \
   && [ "${out%/tmp/logs/probe.log}" != "$out" ]; then
    ok "a passing tier prints one verdict naming its log"
else
    fail "a passing tier prints one verdict naming its log" "exit $rc: $out"
fi

# The COMMAND's status, not the wrapper's and not tee's. 42 is chosen because
# nothing else in this pipeline produces it.
out="$(bash "$TIER" probe probe 40 3000 -- sh -c 'echo noise; exit 42' 2>&1)"
rc=$?
check_eq "a failing tier hands on the command's own status" 42 "$rc"

# ...and it does not read the log aloud. Core's --tail defaults to 0 as of
# v0.2.13; the vendored copy this replaced printed forty lines here.
if [ "${out#*noise}" != "$out" ]; then
    fail "a failing tier names the log instead of quoting it" "the log's contents were printed: $out"
else
    ok "a failing tier names the log instead of quoting it"
fi

# A run that passed but printed more than it may: 65, told apart from a
# failing suite by that status alone.
out="$(bash "$TIER" probe probe 2 3000 -- sh -c 'echo 1; echo 2; echo 3' 2>&1)"
rc=$?
check_eq "a breached budget exits 65" 65 "$rc"

# ---------------------------------------------------------------------------
# The copy is deleted, however a tier ended — and three of the runs above
# ended badly.
# ---------------------------------------------------------------------------
copies_after="$(find "$ROOT/tmp" -maxdepth 1 -name 'output-budget.*' 2>/dev/null | sort)"
check_eq "no transient copy of the wrapper is left behind" "$copies_before" "$copies_after"

rm -f "$ROOT/tmp/logs/probe.log"

printf '  %s checks, %s failed\n' "$checks" "$fails"
if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || exit 1
printf 'output-budget-resolver: all checks passed\n'
