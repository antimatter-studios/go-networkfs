#!/usr/bin/env bash
#
# ci-go-toolcache.sh — put the requested Go into the runner's tool cache so
# actions/setup-go finds it there instead of copying it in.
#
# setup-go, finding no Go in the tool cache, downloads the archive, extracts it
# to a temporary directory and then copies the whole tree into
# $RUNNER_TOOL_CACHE/go/<ver>/x64 — a copy that cost the antimatter-vps runners
# 57-79s per job (issue #121). Extracting the archive straight into that
# directory and writing the `x64.complete` marker @actions/tool-cache's `find`
# requires makes setup-go report "Found in cache" and skip both the download and
# the copy. Its module and build cache handling is unchanged.
#
# The optional version matches an explicit setup-go pin. By default, use the
# `toolchain` directive from go.mod when present, otherwise the `go` directive.
# The archive is linux-amd64, the architecture used by these Linux jobs.
#
#   GO_TOOLCACHE_DL_BASE  where the archive is fetched from (default https://go.dev/dl)
#
#   scripts/ci-go-toolcache.sh [version]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${RUNNER_TOOL_CACHE:?RUNNER_TOOL_CACHE is not set; this runs on a GitHub Actions runner}"
base="${GO_TOOLCACHE_DL_BASE:-https://go.dev/dl}"

ver="${1:-}"
if [ -z "$ver" ]; then
    ver="$(awk '$1=="toolchain"{sub(/^go/,"",$2);print $2;f=1;exit} $1=="go"{g=$2} END{if(!f)print g}' "$ROOT/go.mod")"
fi
if ! [[ "$ver" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?((beta|rc)[0-9]+)?$ ]]; then
    echo "ci-go-toolcache.sh: invalid Go version: '$ver'" >&2
    exit 1
fi

dir="$RUNNER_TOOL_CACHE/go/$ver/x64"
[ -f "$dir.complete" ] && exit 0

rm -rf "$dir"
mkdir -p "$dir"
curl -fsSL --retry 3 "$base/go$ver.linux-amd64.tar.gz" | tar -xz --strip-components=1 -C "$dir"

got="$(GOTOOLCHAIN=local "$dir/bin/go" version)"
case "$got " in
    *"go$ver "*) ;;
    *) echo "ci-go-toolcache.sh: $dir/bin/go reports \"$got\", not go$ver" >&2; rm -rf "$dir"; exit 1 ;;
esac

touch "$dir.complete"
