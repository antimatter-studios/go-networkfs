#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir "$sandbox/bin"

cat > "$sandbox/bin/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[ "$*" = 'mod download' ] || exit 99
attempt=0
[ ! -f "$DOWNLOAD_ATTEMPTS" ] || attempt="$(cat "$DOWNLOAD_ATTEMPTS")"
attempt=$((attempt + 1))
printf '%s\n' "$attempt" > "$DOWNLOAD_ATTEMPTS"
if [ "$attempt" -le "$DOWNLOAD_FAILURES" ]; then
    echo 'go: proxy download: stream error: INTERNAL_ERROR; received from peer' >&2
    exit 27
fi
STUB
cat > "$sandbox/bin/sleep" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOWNLOAD_DELAYS"
STUB
chmod +x "$sandbox/bin/go" "$sandbox/bin/sleep"

run_download() {
    local name="$1" failures="$2" expected_status="$3" expected_attempts="$4"
    local status=0
    (
        cd "$ROOT"
        PATH="$sandbox/bin:$PATH" DOWNLOAD_FAILURES="$failures" \
            DOWNLOAD_ATTEMPTS="$sandbox/$name.attempts" \
            DOWNLOAD_DELAYS="$sandbox/$name.delays" chore deps
    ) > "$sandbox/$name.log" 2>&1 || status=$?
    if [ "$status" -ne "$expected_status" ] || \
        [ "$(cat "$sandbox/$name.attempts")" -ne "$expected_attempts" ]; then
        printf 'dependency-download-retries: %s failed (exit %s, attempts %s)\n' \
            "$name" "$status" "$(cat "$sandbox/$name.attempts")" >&2
        cat "$sandbox/$name.log" >&2
        return 1
    fi
}

run_download immediate 0 0 1
run_download transient 2 0 3
run_download persistent 9 27 3
[ ! -f "$sandbox/immediate.delays" ]
[ "$(cat "$sandbox/transient.delays")" = $'2\n4' ]
[ "$(cat "$sandbox/persistent.delays")" = $'2\n4' ]
printf 'dependency-download-retries: all checks passed\n'
