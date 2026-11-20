#!/usr/bin/env bash
#
# ci-go-toolcache.sh — on Linux, Go is placed in the runner's tool cache before
# actions/setup-go looks for it, so setup-go finds it instead of copying it in.
#
# THE VPS RUNNERS SPENT A MINUTE COPYING GO (issue #121). The antimatter-vps
# image has no Go in its tool cache, so every setup-go step downloaded the
# archive, extracted it to a temporary directory and then spent 57-79s on
# "Adding to the cache ..." — a recursive copy into _tool/go/<ver>/x64 (run
# 44506209693, jobs 135498693923 and 135498690559). scripts/ci-go-toolcache.sh
# extracts the archive straight into the tool cache and writes the
# `x64.complete` marker @actions/tool-cache requires, so setup-go reports
# "Found in cache" and copies nothing.
#
#   bash scripts/tests/ci-go-toolcache.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/ci.yml"
SCRIPT="$ROOT/scripts/ci-go-toolcache.sh"

fails=0
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; }

for tool in curl tar gzip awk; do
    command -v "$tool" >/dev/null || { echo "  FAIL  $tool is not installed; this guard needs it"; exit 1; }
done

# (a) Every setup-go step is immediately preceded by a Linux step that runs the
# script. A step starts at a `      - ` line; the text of the previous step is
# what is checked when a step using actions/setup-go begins.
seen="$(awk '
    /^      - /{
        if (index($0, "actions/setup-go")) {
            n++
            ok = index(prev, "scripts/ci-go-toolcache.sh") && index(prev, "runner.os == '\''Linux'\''")
            print (ok ? "ok" : "missing") " " NR
        }
        prev = $0; next
    }
    /^  [a-z][a-z-]*:/{prev = ""; next}
    {prev = prev "\n" $0}
    END{print "count " n+0}' "$WF")"

count="$(printf '%s\n' "$seen" | awk '$1=="count"{print $2}')"
[ "$count" -gt 0 ] || fail "no actions/setup-go step found in $WF — this guard checked nothing"
while read -r verdict line; do
    [ "$verdict" = missing ] && fail "ci.yml:$line: actions/setup-go is not immediately preceded by a Linux step running scripts/ci-go-toolcache.sh"
done <<<"$(printf '%s\n' "$seen" | grep -v '^count ')"

# (b) Functionally: a fake Go archive served from a file:// base lands in the
# tool cache with its marker, and a second run with the marker present does not
# download at all.
if [ ! -f "$SCRIPT" ]; then
    fail "scripts/ci-go-toolcache.sh does not exist"
else
    ver="$(awk '$1=="toolchain"{sub(/^go/,"",$2);print $2;f=1;exit} $1=="go"{g=$2} END{if(!f)print g}' "$ROOT/go.mod")"
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT
    mkdir -p "$work/src/go/bin" "$work/dl" "$work/cache"
    printf '#!/bin/sh\necho "go version go%s linux/amd64"\n' "$ver" >"$work/src/go/bin/go"
    chmod +x "$work/src/go/bin/go"
    tar -czf "$work/dl/go$ver.linux-amd64.tar.gz" -C "$work/src" go

    run() { RUNNER_TOOL_CACHE="$work/cache" GO_TOOLCACHE_DL_BASE="file://$work/dl" bash "$SCRIPT" >"$work/out" 2>&1; }

    if ! run; then
        fail "the first run failed: $(tr '\n' ' ' <"$work/out")"
    else
        [ -x "$work/cache/go/$ver/x64/bin/go" ] || fail "the first run did not place go/$ver/x64/bin/go in the tool cache"
        [ -f "$work/cache/go/$ver/x64.complete" ] || fail "the first run did not write go/$ver/x64.complete"
        [ -s "$work/out" ] && fail "the first run was not quiet: $(tr '\n' ' ' <"$work/out")"
        rm -f "$work/dl/go$ver.linux-amd64.tar.gz"
        run || fail "a second run with the marker present tried to download: $(tr '\n' ' ' <"$work/out")"
    fi
fi

[ "$fails" -eq 0 ] || exit 1
