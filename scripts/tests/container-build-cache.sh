#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/repo/scripts" "$sandbox/rust-fs-core" "$sandbox/bin"
cp "$ROOT/scripts/docker-suite.sh" "$ROOT/scripts/test-env.sh" "$sandbox/repo/scripts/"
cat > "$sandbox/repo/scripts/servers.sh" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$1" >> "$FAKE_SERVERS_LOG"
if [ "$1" = up ]; then
    printf '%s\n' "${DOCKER_BUILDKIT:-unset}" > "$FAKE_BUILDER_ENV"
fi
SH
cat > "$sandbox/bin/docker" <<'SH'
#!/usr/bin/env bash
if [ "$1" = build ]; then
    printf '%s\n' "${DOCKER_BUILDKIT:-unset}" > "$FAKE_BUILDER_ENV"
    exit "${FAKE_BUILD_EXIT:-0}"
fi
printf '%s\n' "$@" > "$FAKE_DOCKER_LOG"
SH
chmod +x "$sandbox/repo/scripts/servers.sh" "$sandbox/bin/docker"
export PATH="$sandbox/bin:$PATH"
export FAKE_SERVERS_LOG="$sandbox/servers" FAKE_BUILDER_ENV="$sandbox/builder"
export FAKE_DOCKER_LOG="$sandbox/run"
failures=0
fail() { printf '  FAIL  %s\n' "$1"; failures=$((failures + 1)); }

for verbose in 0 1; do
    : > "$FAKE_SERVERS_LOG"
    : > "$FAKE_BUILDER_ENV"
    DOCKER_BUILDKIT=0 OUTPUT_BUDGET_VERBOSE="$verbose" \
        bash "$sandbox/repo/scripts/docker-suite.sh"
    [ "$(cat "$FAKE_BUILDER_ENV")" = 0 ] \
        || fail "a container build keeps the builder the runner selected (verbose=$verbose)"
    grep -qx 'go-networkfs-gobuild:/go/build-cache' "$FAKE_DOCKER_LOG" \
        || fail "the runner retains its compilation cache between suites"
    grep -qx 'GOCACHE=/go/build-cache' "$FAKE_DOCKER_LOG" \
        || fail "Go uses the retained compilation cache"
    [ "$(cat "$FAKE_SERVERS_LOG")" = "$(printf 'up\ndown')" ] \
        || fail "the successful suite tears its servers down"

    : > "$FAKE_BUILDER_ENV"
    env -u DOCKER_BUILDKIT OUTPUT_BUDGET_VERBOSE="$verbose" \
        bash "$sandbox/repo/scripts/docker-suite.sh"
    [ "$(cat "$FAKE_BUILDER_ENV")" = unset ] \
        || fail "a container build selects no builder when the runner selects none (verbose=$verbose)"
done

GO_BUILD_CACHE="$sandbox/ci cache/build" GO_MODULE_CACHE="$sandbox/ci cache/modules" \
    bash "$sandbox/repo/scripts/docker-suite.sh"
grep -qx "$sandbox/ci cache/build:/go/build-cache" "$FAKE_DOCKER_LOG" \
    || fail "CI can restore the compilation cache outside an ephemeral Docker volume"
grep -qx "$sandbox/ci cache/modules:/go/pkg/mod" "$FAKE_DOCKER_LOG" \
    || fail "CI can restore the module cache outside an ephemeral Docker volume"
[ -d "$sandbox/ci cache/build" ] && [ -d "$sandbox/ci cache/modules" ] \
    || fail "host cache directories exist before Docker mounts them"

: > "$FAKE_SERVERS_LOG"
status=0
FAKE_BUILD_EXIT=27 bash "$sandbox/repo/scripts/docker-suite.sh" || status=$?
[ "$status" -eq 27 ] || fail "a failed build preserves its exit status"
[ "$(cat "$FAKE_SERVERS_LOG")" = "$(printf 'up\ndown')" ] \
    || fail "a failed build tears its servers down"
grep -qx 'COPY test/mockapi ./test/mockapi' "$ROOT/.github/docker/mockapi/Dockerfile" \
    || fail "the mock server image copies only its own source"
if grep -qE '^COPY \. \.$|^RUN go mod download$' "$ROOT/.github/docker/mockapi/Dockerfile"; then
    fail "the standard-library-only mock server neither downloads unrelated dependencies nor rebuilds for unrelated source"
fi

[ "$failures" -eq 0 ] || exit 1
printf 'container-build-cache: all checks passed\n'
