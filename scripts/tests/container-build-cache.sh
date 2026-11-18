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
        bash "$sandbox/repo/scripts/docker-suite.sh" >/dev/null
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
        bash "$sandbox/repo/scripts/docker-suite.sh" >/dev/null
    [ "$(cat "$FAKE_BUILDER_ENV")" = unset ] \
        || fail "a container build selects no builder when the runner selects none (verbose=$verbose)"
done

GO_BUILD_CACHE="$sandbox/ci cache/build" GO_MODULE_CACHE="$sandbox/ci cache/modules" \
    bash "$sandbox/repo/scripts/docker-suite.sh" >/dev/null
grep -qx "$sandbox/ci cache/build:/go/build-cache" "$FAKE_DOCKER_LOG" \
    || fail "CI can restore the compilation cache outside an ephemeral Docker volume"
grep -qx "$sandbox/ci cache/modules:/go/pkg/mod" "$FAKE_DOCKER_LOG" \
    || fail "CI can restore the module cache outside an ephemeral Docker volume"
[ -d "$sandbox/ci cache/build" ] && [ -d "$sandbox/ci cache/modules" ] \
    || fail "host cache directories exist before Docker mounts them"

: > "$FAKE_SERVERS_LOG"
status=0
FAKE_BUILD_EXIT=27 bash "$sandbox/repo/scripts/docker-suite.sh" >/dev/null || status=$?
[ "$status" -eq 27 ] || fail "a failed build preserves its exit status"
[ "$(cat "$FAKE_SERVERS_LOG")" = "$(printf 'up\ndown')" ] \
    || fail "a failed build tears its servers down"
# --- The mock server compiles against the Go cache CI restores. --------------
#
# A Dockerfile stage starts with an empty cache on every runner, so each run
# compiled the standard library again, on the slowest path of `servers.sh up`.
dockerfile="$ROOT/.github/docker/mockapi/Dockerfile"
if grep -qiE '^FROM golang|go build' "$dockerfile"; then
    fail "the mock server image holds no compile stage, which could not reach the Go cache"
fi
runner_base="$(sed -n 's/^FROM //p' "$ROOT/.github/docker/testrunner/Dockerfile" | head -1)"
compiler="$(env -i bash -c ". '$ROOT/scripts/test-env.sh'; printf '%s' \"\${MOCK_BUILD_IMAGE:-}\"" 2>/dev/null)"
[ "$compiler" = "$runner_base" ] \
    || fail "the mock server compiles in the runner's base image, pulled once ('$compiler', '$runner_base')"
first_two="$(env -i bash -c "eval \"\$(grep '^ALL_SERVERS=' '$ROOT/scripts/servers.sh')\"; set -- \$ALL_SERVERS; printf '%s %s' \"\$1\" \"\$2\"" 2>/dev/null)"
[ "$first_two" = "mockapi sabredav" ] \
    || fail "the two slowest servers take the first slots, not '$first_two'"

mkdir -p "$sandbox/mock-bin"
cat > "$sandbox/mock-bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_MOCK_LOG"
case "$1" in
    image) exit 0 ;;
    build)
        for last; do :; done
        cp "$last/mockapi" "$FAKE_MOCK_CONTEXT" 2>/dev/null
        ls -l "$last/mockapi" | cut -c4 > "$FAKE_MOCK_CONTEXT.mode"
        exit 0
        ;;
    run)
        case "$*" in
            *"go build"*)
                if [ -n "${FAKE_COMPILE_FAILS:-}" ]; then
                    echo "compile step failed" >&2
                    exit 7
                fi
                printf 'compiled binary'
                ;;
        esac
        exit 0
        ;;
esac
SH
chmod +x "$sandbox/mock-bin/docker"
export FAKE_MOCK_LOG="$sandbox/mock-log" FAKE_MOCK_CONTEXT="$sandbox/mock-context"

# up_mockapi and what it calls, as servers.sh defines them; only the container
# lifecycle around the build is stubbed.
run_up_mockapi() {
    : > "$FAKE_MOCK_LOG"
    rm -f "$FAKE_MOCK_CONTEXT" "$FAKE_MOCK_CONTEXT.mode"
    (
        set -e
        PATH="$sandbox/mock-bin:$PATH"
        REPO="$ROOT" VERBOSE=0
        . "$ROOT/scripts/test-env.sh"
        eval "$(sed -n '/^build_image() {/,/^}/p; /^ensure_image() {/,/^}/p; /^compile_mockapi() {/,/^}/p; /^up_mockapi() {/,/^}/p' "$ROOT/scripts/servers.sh")"
        rm_container() { :; }
        wait_for_port() { :; }
        up_mockapi
    ) 2>&1
}

out="$(GO_BUILD_CACHE="$sandbox/ci cache/build" run_up_mockapi)" \
    || fail "the mock server comes up from a compiled binary: $out"
compile="$(grep -F 'go build' "$FAKE_MOCK_LOG" || true)"
case "$compile" in
    *"$sandbox/ci cache/build:/go/build-cache"*) ;;
    *) fail "the mock server compiles against the restored build cache: $compile" ;;
esac
case "$compile" in *GOCACHE=/go/build-cache*) ;; *) fail "Go uses that cache for the mock server" ;; esac
case "$compile" in *CGO_ENABLED=0*) ;; *) fail "the mock server stays a static binary" ;; esac
case "$compile" in *./test/mockapi*) ;; *) fail "only the mock server is compiled" ;; esac
[ "$(cat "$FAKE_MOCK_CONTEXT" 2>/dev/null)" = "compiled binary" ] \
    || fail "the image is built from the binary that was compiled"
[ "$(cat "$FAKE_MOCK_CONTEXT.mode" 2>/dev/null)" = x ] \
    || fail "the compiled binary is executable in the image"

status=0
out="$(FAKE_COMPILE_FAILS=1 run_up_mockapi)" || status=$?
[ "$status" -ne 0 ] || fail "a failed compile fails the mock server"
case "$out" in *"compile step failed"*) ;; *) fail "a failed compile shows why" ;; esac
if grep -q '^build ' "$FAKE_MOCK_LOG"; then
    fail "a failed compile builds no image"
fi

[ "$failures" -eq 0 ] || exit 1
printf 'container-build-cache: all checks passed\n'
