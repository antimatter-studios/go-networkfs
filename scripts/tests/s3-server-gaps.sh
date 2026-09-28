#!/usr/bin/env bash
#
# s3-server-gaps.sh — the driver must not start using an S3 operation the test
# server does not implement.
#
# stupid-simple-s3 covers everything s3/s3.go calls. Three things it does not
# implement — ListBuckets, UploadPartCopy and max-keys — and none of them is
# reached today, which is exactly the trap: a change that started using one
# would pass `chore test:s3` and `integration (containerised)` and still be
# broken against AWS or any other server, because the suite would be testing a
# stub of it. Issue #11.
#
# TWO HALVES, AND THIS IS THE ONE A TEST CANNOT MAKE.
# s3/s3_gaps_test.go asserts the gaps are still gaps, so the suite goes red the
# day the SERVER grows one of them. It cannot notice the other direction — the
# day the DRIVER reaches for one — because the call would simply fail against
# a server that does not answer it, and against AWS it would work, so neither
# run tells you which it was. A source check can, and needs no server to do it.
#
# WHY NAMES AND NOT BEHAVIOUR. ListBuckets and ComposeObject are methods on the
# minio-go client: their appearance in s3.go is the whole event. The third,
# CopyObject over 5 GiB, is NOT visible this way — minio-go switches to the
# multipart copy path inside the library, so Rename is already one large file
# away from it. That one is recorded in the Go test's comment and in #11; there
# is nothing here to grep for, and pretending otherwise would be worse than
# saying so.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DRIVER="$ROOT/s3/s3.go"
GAPS="$ROOT/s3/s3_gaps_test.go"

EXPECTED_CHECKS=10
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# --- The driver reaches for none of them. ----------------------------------
for call in ListBuckets ComposeObject UploadPartCopy; do
    found="$(grep -n "\.$call(" "$DRIVER" || true)"
    if [ -z "$found" ]; then ok
    else fail "s3.go does not call $call, which the test server does not implement" \
              "$found — a suite run would be testing a stub of it; see issue #11"; fi
done

# --- And the gaps are asserted somewhere a run can see them. ---------------
if [ -f "$GAPS" ]; then ok
else fail "s3/s3_gaps_test.go exists to assert the three gaps are still gaps"; fi

if head -1 "$GAPS" 2>/dev/null | grep -q 's3_integration'; then ok
else fail "the gap tests are behind the s3_integration tag, so they run against the server"; fi

for gap in ListBuckets UploadPartCopy MaxKeysIgnored; do
    if grep -q "func TestServerGap_$gap(" "$GAPS" 2>/dev/null; then ok
    else fail "TestServerGap_$gap pins that gap" \
              "the day the server implements it, nothing would go red"; fi
done

# A gap test that never talks to the server would pass whatever the server
# did. Each one builds the same minio-go client the driver builds.
if grep -q 'minio.New(' "$GAPS" 2>/dev/null; then ok
else fail "the gap tests use the same minio-go client the driver does"; fi

# And each asserts the gap is OPEN, so closing it is what goes red. A test
# that tolerated both answers would be documentation with a t.Run around it.
closing="$(grep -c 'err == nil\|events <= 1' "$GAPS" 2>/dev/null || true)"
if [ "${closing:-0}" -ge 3 ]; then ok
else fail "each gap test fails when the gap closes" \
          "$(grep -n 'err == nil\|events <= 1' "$GAPS" | tr '\n' '|')"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 's3-server-gaps: all checks passed\n'
