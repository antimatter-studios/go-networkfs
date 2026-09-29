#!/usr/bin/env bash
#
# suite-env.sh — the tagged integration tests get their servers' addresses on
# every path that runs them, not only on the one that starts the servers.
#
# THE DEFECT THIS HOLDS SHUT (issue #33). The S3 and SMB integration tests read
# S3_ENDPOINT, SMB_HOST and friends. scripts/with-servers.sh exported them, so
# `chore test:s3` and `chore test:smb` had them. The containerised run does not
# pass through with-servers.sh — it runs `chore test:ci`, which runs
# scripts/suite.sh — and nothing there exported anything. Every tagged S3 and
# SMB test skipped itself in CI, and a skip reports as a pass, so the
# `integration (containerised)` job was green having run none of them.
#
# BEHAVIOURAL, NOT A GREP. suite.sh is run for real with GO pointed at a stub
# that records the environment `go test` would have seen and then fails, which
# stops suite.sh (set -e) before it reaches the C harnesses. No Go toolchain,
# no Docker, no server: this runs on the macOS runner as well as ubuntu.
#
# Quiet on success: this tier's output is budgeted.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

EXPECTED_CHECKS=20
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT

# The variables each tagged suite refuses to run without — the lists in the
# requireEnv of s3/s3_integration_test.go and smb/smb_integration_test.go.
REQUIRED="S3_ENDPOINT S3_BUCKET S3_ACCESS_KEY S3_SECRET_KEY SMB_HOST SMB_SHARE SMB_USER SMB_PASS"

# --- suite.sh exports them before `go test` runs. ---------------------------
cat > "$SANDBOX/go" <<'STUB'
#!/usr/bin/env bash
case "$1" in
    list) echo example.invalid/pkg ;;
    test) env > "$SUITE_ENV_DUMP"; exit 3 ;;
    *)    exit 0 ;;
esac
STUB
chmod +x "$SANDBOX/go"

# An environment with none of the variables in it, the way the runner container
# starts: only what a shell needs, plus the dump path for the stub.
env -i PATH="$PATH" HOME="${HOME:-/tmp}" SUITE_ENV_DUMP="$SANDBOX/env" \
    GO="$SANDBOX/go" COVERAGE="$SANDBOX/coverage.out" \
    bash "$ROOT/scripts/suite.sh" >"$SANDBOX/out" 2>&1
status=$?

if [ "$status" -eq 3 ]; then ok
else fail "suite.sh reached \`go test\` and stopped on its failure" \
          "exit $status: $(tr '\n' '|' < "$SANDBOX/out")"; fi

if [ -s "$SANDBOX/env" ]; then ok
else fail "the stub recorded the environment go test saw"; fi
touch "$SANDBOX/env"

for v in $REQUIRED; do
    if grep -q "^$v=." "$SANDBOX/env"; then ok
    else fail "suite.sh exports $v to the tagged tests" \
              "a tagged test run from \`chore test:ci\` has no $v"; fi
done

# --- servers.sh env prints what the suite gets, and nothing less. -----------
printed="$(env -i PATH="$PATH" HOME="${HOME:-/tmp}" bash "$ROOT/scripts/servers.sh" env 2>&1)"
for v in $REQUIRED; do
    want="$(grep "^$v=" "$SANDBOX/env")"
    case "$printed" in
        *"$want"*) ok ;;
        *) fail "servers.sh env prints $v as the suite sees it" \
                "suite: '$want'; printed: $(printf '%s' "$printed" | tr '\n' '|')" ;;
    esac
done

# --- with-servers.sh takes the same definition. -----------------------------
# A text check, because running it starts servers. The point is that there is
# ONE definition: a second, hand-written export list is how the two drifted.
if grep -q '^export_suite_env$' "$ROOT/scripts/with-servers.sh"; then ok
else fail "with-servers.sh calls export_suite_env"; fi
if grep -q '^export S3_\|^export SMB_' "$ROOT/scripts/with-servers.sh"; then
    fail "with-servers.sh carries no export list of its own" \
         "$(grep -n '^export ' "$ROOT/scripts/with-servers.sh" | tr '\n' '|')"
else ok; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'suite-env: all checks passed\n'
