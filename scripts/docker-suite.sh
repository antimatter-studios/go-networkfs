#!/usr/bin/env bash
#
# docker-suite.sh — the whole suite inside a container, so the only thing this
# machine needs is Docker: no Go toolchain, no C compiler, no chore.
#
# The servers are started on the host and joined to a shared network; the
# runner joins the same network and reaches them BY NAME on their internal
# ports rather than through published ones. That is also what makes the
# pipeline reproducible, since CI runs this same image — what a contributor
# runs with `chore test` is what the `integration (containerised)` job runs.
#
# THE SERVERS COME DOWN HOWEVER THIS ENDS. A trap, not a sequence: an
# interrupted run used to leave six containers and a network behind, and the
# next run then failed on a port that was already bound.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

# THE SIBLING IS MOUNTED, BECAUSE ../rust-fs-core DOES NOT EXIST IN THERE.
#
# The container runs `chore test:ci`, which runs scripts/tier.sh, which needs
# rust-fs-core's output-budget.sh. Only this checkout is mounted, so the
# sibling beside it is not there and the relative path resolves to /rust-fs-core
# — a directory that has never existed in any of these images. Resolve it out
# here, where it does exist, and hand the container the answer: FS_CORE_ROOT is
# authoritative, so the inner tier looks exactly where it is told and nowhere
# else. Read-only, because the suite has no business writing to a checkout
# every other project on the machine shares.
CORE_BUDGET="$("$REPO/scripts/resolve-output-budget.sh")"
CORE_ROOT="$(cd "$(dirname "$CORE_BUDGET")/.." && pwd)"

# shellcheck source=scripts/test-env.sh
. "$REPO/scripts/test-env.sh"

command -v docker >/dev/null 2>&1 \
    || { echo "docker-suite.sh: docker is not installed — this task IS the container." >&2; exit 1; }

cleanup() { scripts/servers.sh down >/dev/null 2>&1 || true; }
trap cleanup EXIT

scripts/servers.sh up

# Built from the repository root, not from the Dockerfile's directory: the
# image copies scripts/ci-install-chore.sh so that how chore is pinned and
# checksummed is written down once. .dockerignore keeps the context small.
if [ "${OUTPUT_BUDGET_VERBOSE:-0}" = 1 ]; then
    docker build -t "$RUNNER_IMAGE" -f .github/docker/testrunner/Dockerfile .
else
    docker build -q -t "$RUNNER_IMAGE" -f .github/docker/testrunner/Dockerfile . >/dev/null
fi

# The module cache is a named volume so a second run does not re-download the
# world. /src is this checkout: the suite writes coverage.out and tmp/logs/
# where the host can read them, which is what CI uploads.
#
# `chore test:ci` inside, not a script: the container runs the same task name a
# developer runs when the servers are already up, and the runner image carries
# chore for exactly that reason.
#
# CI AND GITHUB_ACTIONS ARE FORWARDED because the C harnesses read them. A
# missing mount config is a skip on a laptop and a FAILURE in the job whose
# reason for existing is that the servers are up (issue #6) — and the harnesses
# run in here, where nothing about the outer runner is visible unless it is
# passed in. Empty on a developer machine, which is the laptop case.
docker run --rm --network "$TEST_NETWORK" \
    -v "$REPO":/src -v go-networkfs-gomod:/go/pkg/mod \
    -e SMB_ADDR=samba    -e SMB_PORT=445 \
    -e S3_ADDR=sss3      -e S3_PORT=5553 \
    -e FTP_ADDR=ftp      -e FTP_PORT=21 \
    -e SFTP_ADDR=sftp    -e SFTP_PORT=22 \
    -e DAV_ADDR=webdav   -e DAV_PORT=80 \
    -e MOCK_ADDR=mockapi -e MOCK_PORT=8081 \
    -e OUTPUT_BUDGET_VERBOSE="${OUTPUT_BUDGET_VERBOSE:-0}" \
    -e CI="${CI:-}" -e GITHUB_ACTIONS="${GITHUB_ACTIONS:-}" \
    -v "$CORE_ROOT":/fs-core:ro -e FS_CORE_ROOT=/fs-core \
    "$RUNNER_IMAGE" chore test:ci
