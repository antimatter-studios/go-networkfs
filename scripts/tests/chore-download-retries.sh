#!/usr/bin/env bash
#
# chore-download-retries.sh — the chore download retries a transient server
# error.
#
# GitHub's release download answers an occasional HTTP 500, and a `curl` with
# no retry turns that one answer into a red job before anything under test has
# run. It failed CI jobs elsewhere in the family (rust-fs-ext4#494,
# rust-fs-btrfs#286). Every chore download -- in scripts/ci-install-chore.sh
# and in any workflow that fetches a chore release itself -- must retry at
# least three times and on any error. The checksum check that follows the
# download still guards what was fetched, so retrying is safe.
#
#   bash scripts/tests/chore-download-retries.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0
ok()   { :; }
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# scan <installer> <workflow>...: one line per chore download that does not
# retry. Each `curl` command is read with comments dropped and `\`
# continuations joined, so a flag on the next line still counts. Every curl in
# the installer is a chore download; in a workflow, only one that names a
# chore release is.
scan() {
    local installer="$1" f
    shift
    for f in "$installer" "$@"; do
        [ -f "$f" ] || continue
        awk -v all="$([ "$f" = "$installer" ] && echo 1 || echo 0)" -v file="$f" '
            { sub(/^[ \t]+/, "") }
            /^#/ { next }
            {
                continued = sub(/\\$/, "")
                joined = joined $0 " "
                if (continued) next
                if (joined ~ /(^|[ \t(|])curl[ \t]/ && (all || joined ~ /chore\/releases\/download/)) {
                    print "DOWNLOAD"
                    retries = 0
                    if (match(joined, /--retry [0-9]+/)) retries = substr(joined, RSTART + 8, RLENGTH - 8) + 0
                    if (retries < 3 || joined !~ /--retry-all-errors/)
                        print file ": fails the job on one transient HTTP 5xx: " joined
                }
                joined = ""
            }
        ' "$f"
    done
}

# --- 1. The scan sees the defect it exists for. ---------------------------
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
cat > "$sandbox/bare.sh" <<'SH'
curl -fsSL -o "$dir/$tarball" "$base/$tarball"
SH
cat > "$sandbox/retried.sh" <<'SH'
# curl -fsSL with no retry, in a comment, is not a download
curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 \
    -o "$dir/$tarball" "$base/$tarball"
SH
cat > "$sandbox/too-few.sh" <<'SH'
curl -fsSL --retry 1 --retry-all-errors -o "$dir/$tarball" "$base/$tarball"
SH
cat > "$sandbox/workflow.yml" <<'YML'
      - run: |
          curl -fsSL "https://github.com/antimatter-studios/chore/releases/download/v0.11.0/$asset" \
            | tar xz
          curl -fsSL https://example.com/not-chore.tar.gz | tar xz
YML
case "$(scan "$sandbox/bare.sh")" in *"transient HTTP 5xx"*) ok ;; *) fail "the scan finds a download with no retry" ;; esac
case "$(scan "$sandbox/too-few.sh")" in *"transient HTTP 5xx"*) ok ;; *) fail "the scan finds a download that retries fewer than three times" ;; esac
found="$(scan "$sandbox/retried.sh")"
case "$found" in *"transient HTTP 5xx"*) fail "the scan passes a download that retries, its flags on a continuation line" "$found" ;; *) ok ;; esac
found="$(scan "$sandbox/none.sh" "$sandbox/workflow.yml")"
if [ "$(printf '%s\n' "$found" | grep -c 'transient HTTP 5xx')" -eq 1 ]; then ok
else fail "in a workflow, the scan checks the chore download and only it" "$found"; fi

# --- 2. This repository's installer and workflows. ------------------------
shopt -s nullglob
workflows=("$ROOT"/.github/workflows/*.yml "$ROOT"/.github/workflows/*.yaml)
found="$(scan "$ROOT/scripts/ci-install-chore.sh" "${workflows[@]}")"
grep -q '^DOWNLOAD$' <<<"$found" || fail "found no chore download to check; the guard is looking in the wrong place"
bare="$(grep -v '^DOWNLOAD$' <<<"$found")"
if [ -z "$bare" ]; then ok
else fail "every chore download retries; add --retry 5 --retry-all-errors --retry-delay 2" "${bare//$ROOT\//}"; fi

[ "$fails" -eq 0 ] || { printf '  %s check(s) failed\n' "$fails"; exit 1; }
printf 'chore-download-retries: all checks passed\n'
