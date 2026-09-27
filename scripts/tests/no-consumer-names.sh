#!/usr/bin/env bash
#
# no-consumer-names.sh — no tracked file names an application built on this
# module.
#
# This module is a standalone project (AGENTS.md: "Never mention a consuming
# application in the README, the source, or CLI help"). A name slips in the
# easy way — a header recalling where a driver was migrated from, a label in
# a test server's config — and nothing else here would notice (#23). So this
# reads EVERY tracked file: Go source, the C ABI tests, scripts, docs,
# workflows, test-server fixtures, the changelog. Comments included: a name
# in a comment is still a name in the repository.
#
# THE NAME LIST LIVES HERE AND NOWHERE ELSE. Each entry is a case-insensitive
# extended regex, so one entry covers the spellings of one name. This file is
# the one place allowed to spell them, and the only file the scan does not
# read.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
#
#   bash scripts/tests/no-consumer-names.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SELF="$(basename "${BASH_SOURCE[0]}")"

NAMES=(
    'disk[-_ ]?jockey'
    'in[-_]?pace'
)

PATTERN="$(IFS='|'; echo "${NAMES[*]}")"

EXPECTED_CHECKS=5
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '%s\n' "$2"; }

# Every tracked line under <root> (a git work tree) that names one of them.
scan() {
    git -C "$1" grep -nIiE -e "$PATTERN" -- . ":(exclude)scripts/tests/$SELF" || true
}

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT

# --- 1. The scan recognises every name it refuses. -------------------------
#
# Without this a pattern that matched nothing — a typo, a grep that reads the
# flags differently — would pass the real tree having checked nothing.
git -C "$sandbox" init -q
mkdir -p "$sandbox/scripts/tests" "$sandbox/smb" "$sandbox/docs"
cat > "$sandbox/smb/smb.go" <<'EOF'
// Written for the DiskJockey app.
package smb
EOF
cat > "$sandbox/docs/notes.md" <<'EOF'
Seen on an inpace.service unit.
disk-jockey and Disk Jockey and INPACE_HOME too.
Nothing to see on this line: a disk, a jockey, in place.
EOF
# An untracked file is not the repository's, and this file's own name is the
# one allowed mention.
printf 'DiskJockey\n' > "$sandbox/untracked.txt"
printf 'DiskJockey\n' > "$sandbox/scripts/tests/$SELF"
git -C "$sandbox" add smb/smb.go docs/notes.md "scripts/tests/$SELF"

found="$(scan "$sandbox")"
expect=(
    "smb/smb.go:1:"
    "docs/notes.md:1:"
    "docs/notes.md:2:"
)
for e in "${expect[@]}"; do
    if grep -qF "$e" <<<"$found"; then ok
    else fail "the scan finds $e" "$found"; fi
done
count="$(grep -c . <<<"$found")"
if [ "$count" -eq ${#expect[@]} ]; then ok
else fail "the scan finds exactly ${#expect[@]} lines, not $count" "$found"; fi

# --- 2. The repository names none of them. ---------------------------------
found="$(scan "$ROOT")"
if [ -z "$found" ]; then ok
else fail "these name an application built on this module; describe the scenario instead:" "$found"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'no-consumer-names: all checks passed\n'
