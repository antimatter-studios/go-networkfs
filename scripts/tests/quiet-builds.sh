#!/usr/bin/env bash
#
# quiet-builds.sh — a quiet image build says nothing, and says everything when it fails.
#
# `docker build -q` still prints the legacy builder's deprecation notice, four
# lines on stderr, for every image. Eleven servers and the runner made a
# passing suite print more than its output budget, and the tier failed.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin" "$sandbox/repo/scripts" "$sandbox/rust-fs-core"
cat > "$sandbox/bin/docker" <<'SH'
#!/usr/bin/env bash
if [ "$1" = build ]; then
    printf 'DEPRECATED: The legacy builder is deprecated\n' >&2
    [ "${FAKE_BUILD_EXIT:-0}" -eq 0 ] || echo "build step failed" >&2
    exit "${FAKE_BUILD_EXIT:-0}"
fi
exit 0
SH
chmod +x "$sandbox/bin/docker"
export PATH="$sandbox/bin:$PATH"

failures=0
fail() { printf '  FAIL  %s\n' "$1"; failures=$((failures + 1)); }

fn="$(sed -n '/^build_image() {/,/^}/p' "$ROOT/scripts/servers.sh")"
[ -n "$fn" ] || fail "servers.sh defines build_image"

# build_image, run as servers.sh runs it.
run_build_image() {
    ( VERBOSE=0; eval "$fn"; build_image tag some/dir ) 2>&1
}
out="$(run_build_image)" || fail "a good build succeeds"
[ -z "$out" ] || fail "a good server image build prints nothing, not: $out"
status=0
out="$(FAKE_BUILD_EXIT=9 run_build_image)" || status=$?
[ "$status" -eq 9 ] || fail "a failed server image build keeps its exit status ($status)"
case "$out" in *"build step failed"*) ;; *) fail "a failed server image build shows why" ;; esac

# The runner image, built by docker-suite.sh.
cp "$ROOT/scripts/docker-suite.sh" "$ROOT/scripts/test-env.sh" "$sandbox/repo/scripts/"
printf '#!/usr/bin/env bash\nexit 0\n' > "$sandbox/repo/scripts/servers.sh"
chmod +x "$sandbox/repo/scripts/servers.sh"
out="$(bash "$sandbox/repo/scripts/docker-suite.sh" 2>&1)" || fail "a good suite run succeeds"
case "$out" in *DEPRECATED*) fail "a good runner image build prints no builder notice" ;; esac
status=0
out="$(FAKE_BUILD_EXIT=9 bash "$sandbox/repo/scripts/docker-suite.sh" 2>&1)" || status=$?
[ "$status" -eq 9 ] || fail "a failed runner image build keeps its exit status ($status)"
case "$out" in *"build step failed"*) ;; *) fail "a failed runner image build shows why" ;; esac

[ "$failures" -eq 0 ] || exit 1
printf 'quiet-builds: all checks passed\n'
