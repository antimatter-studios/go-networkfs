#!/usr/bin/env bash
#
# runner-gate-jobs.sh — every job takes its runner from its own gate job on
# the antimatter-vps runner gate, and nothing asks the replaced picker.
#
# THE PICK JOBS QUEUED FOR A HOSTED RUNNER JUST TO ASK (#133). Each gated job
# had a `runner-*` job before it that ran on GitHub's ubuntu-24.04 for a few
# seconds of curl. With the organisation's hosted runners saturated, those
# pick jobs waited 368-2539 s for a runner before asking anything, so a run
# took 40-75 minutes for a few minutes of work. A gate job runs on the gate's
# own runner, `antimatter-vps-gate`, and takes no hosted runner at all.
#
# scripts/ci-runner-gates checks every workflow against the gate's shape; its
# own tests check that it refuses each departure from it.
#
#   bash scripts/tests/runner-gate-jobs.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
go test ./scripts/ci-runner-gates >/dev/null 2>&1 || {
    go test ./scripts/ci-runner-gates 2>&1 | tail -20
    echo "runner-gate-jobs.sh: go test ./scripts/ci-runner-gates failed" >&2
    exit 1
}
go run ./scripts/ci-runner-gates
