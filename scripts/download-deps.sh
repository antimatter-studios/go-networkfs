#!/usr/bin/env bash
set -euo pipefail

attempt=1
while [ "$attempt" -le 3 ]; do
    if go mod download; then
        exit 0
    else
        status=$?
    fi
    if [ "$attempt" -eq 3 ]; then
        exit "$status"
    fi
    printf 'download-deps: attempt %s/3 failed (exit %s); retrying in %ss\n' \
        "$attempt" "$status" "$((attempt * 2))" >&2
    sleep "$((attempt * 2))"
    attempt=$((attempt + 1))
done
