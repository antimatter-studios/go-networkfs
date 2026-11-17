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
    DOCKER_BUILDKIT=0 OUTPUT_BUDGET_VERBOSE="$verbose" \
        bash "$sandbox/repo/scripts/docker-suite.sh"
    [ "$(cat "$FAKE_BUILDER_ENV")" = 1 ] || fail "container builds use BuildKit, including when the runner disables it"
    grep -qx 'go-networkfs-gobuild:/go/build-cache' "$FAKE_DOCKER_LOG" \
        || fail "the runner retains its compilation cache between suites"
    grep -qx 'GOCACHE=/go/build-cache' "$FAKE_DOCKER_LOG" \
        || fail "Go uses the retained compilation cache"
    [ "$(cat "$FAKE_SERVERS_LOG")" = "$(printf 'up\ndown')" ] \
        || fail "the successful suite tears its servers down"
done

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
