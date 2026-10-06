#!/usr/bin/env bash
#
# run-all.sh — every shell guard in this directory, and a refusal when there
# are none.
#
# THE COUNT IS THE POINT. A glob that matches nothing expands to nothing, the
# loop body never runs, and a task that executed no guard at all exits 0 —
# which reads exactly like a task whose guards all passed. That is the failure
# mode this repository keeps meeting in other shapes (issue #6: a driver with
# no case in the build's switch was silently built with nothing), so the floor
# is written down rather than assumed.
#
# `chore test:scripts` runs this under ../rust-fs-core/scripts/tier.sh, so its output is
# budgeted like any other tier.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ran=0
failed=0
for guard in "$HERE"/*.sh; do
    case "$guard" in
        */run-all.sh) continue ;;
    esac
    [ -f "$guard" ] || continue
    ran=$((ran + 1))
    bash "$guard" || failed=$((failed + 1))
done

if [ "$ran" -eq 0 ]; then
    echo "run-all.sh: scripts/tests/ holds no guards — this task proved nothing." >&2
    exit 1
fi

echo "run-all.sh: $ran guards, $failed failed"
[ "$failed" -eq 0 ]
