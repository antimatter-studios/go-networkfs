#!/usr/bin/env bash
#
# run-isolation.sh — two runs on one Docker host share no name and no port.
#
# scripts/test-env.sh gives a run that has an id (RUN_TAG, or CI's
# GITHUB_RUN_ID) its own container names, network, runner image and port range.
# Without that, `servers.sh up` removes another run's containers by name and a
# published port the other holds fails the start, which is what a shared
# antimatter-vps runner would do to two integration jobs at once.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="$ROOT/scripts/test-env.sh"

EXPECTED_CHECKS=9
checks=0
fails=0
ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# Every name and port the file assigns, under the given environment, as
# NAME=value lines.
dump() { env -i PATH="$PATH" "$@" bash -c ". '$ENV_FILE'; set | grep -E '^(TEST_NETWORK|RUNNER_IMAGE|[A-Z0-9_]*_CONTAINER|[A-Z0-9_]*_PORT|FTP_PASV_(LO|HI))='"; }

plain="$(dump)"
a="$(dump GITHUB_RUN_ID=1001 GITHUB_RUN_ATTEMPT=1)"
b="$(dump GITHUB_RUN_ID=1002 GITHUB_RUN_ATTEMPT=1)"
again="$(dump GITHUB_RUN_ID=1001 GITHUB_RUN_ATTEMPT=2)"

# Nothing moves for a run with no id.
if [ -n "$plain" ] && ! printf '%s\n' "$plain" | grep -q -- '-[0-9]*-[0-9]*$'; then ok
else fail "a run with no id keeps the fixed names" "$plain"; fi

# Two runs share no container, network or port value.
names() { printf '%s\n' "$1" | grep -E '^(TEST_NETWORK|[A-Z0-9_]*_CONTAINER)=' | cut -d= -f2 | sort; }
ports() { printf '%s\n' "$1" | grep -E '_PORT=|PASV' | cut -d= -f2 | sort; }
if [ -z "$(comm -12 <(names "$a") <(names "$b"))" ]; then ok
else fail "two runs share no container or network name" "$(comm -12 <(names "$a") <(names "$b"))"; fi
if [ -n "$(names "$a")" ] && [ "$(names "$a")" != "$(names "$plain")" ]; then ok
else fail "a run with an id has names of its own"; fi
off_a="$(env -i PATH="$PATH" GITHUB_RUN_ID=1001 bash -c ". '$ENV_FILE'; echo \$PORT_OFFSET")"
off_b="$(env -i PATH="$PATH" GITHUB_RUN_ID=1002 bash -c ". '$ENV_FILE'; echo \$PORT_OFFSET")"
if [ "$off_a" != "$off_b" ]; then
    if [ -z "$(comm -12 <(ports "$a") <(ports "$b"))" ]; then ok
    else fail "two runs with different offsets share no port" "$(comm -12 <(ports "$a") <(ports "$b"))"; fi
else fail "the two test run ids draw different offsets" "both drew $off_a; choose other ids"; fi

# A re-run (another attempt) is not the run it replaces.
if [ "$(names "$a")" != "$(names "$again")" ]; then ok
else fail "a second attempt has names of its own"; fi

# Within one run, no two services share a port, and the FTP passive range stays
# below the ephemeral range (#15), whatever the offset. One shell loops over
# the ids, each in a subshell of its own.
sweep="$(env -i PATH="$PATH" bash -c '
    for id in $(seq 1 200); do
        (
            GITHUB_RUN_ID=$id; . "'"$ENV_FILE"'"
            dup=$(set | grep -E "_PORT=|FTP_PASV_" | cut -d= -f2 | sort | uniq -d | head -1)
            echo "$id ${dup:-none} $FTP_PASV_HI"
        )
    done' 2>/dev/null)"
dups="$(printf '%s\n' "$sweep" | awk '$2 != "none" {print "run " $1 " repeats port " $2}' | head -3)"
if [ -n "$sweep" ] && [ -z "$dups" ]; then ok; else fail "within a run, no two services share a port" "$dups"; fi
high="$(printf '%s\n' "$sweep" | awk '{print $3}' | sort -n | tail -1)"
if [ -n "$high" ] && [ "$high" -lt 32768 ]; then ok; else fail "the FTP passive range stays below 32768" "highest '$high'"; fi

# What the caller set is left alone.
mine="$(env -i PATH="$PATH" GITHUB_RUN_ID=1001 DAV_PORT=19999 DAV_CONTAINER=mine bash -c ". '$ENV_FILE'; echo \$DAV_PORT \$DAV_CONTAINER")"
if [ "$mine" = "19999 mine" ]; then ok; else fail "a port and a name the caller set are left alone" "$mine"; fi

# docker-suite.sh and servers.sh take every name from the environment file.
if ! grep -nE 'go-networkfs-(samba|sss3|ftp|sftp|webdav|mockapi|test)\b' "$ROOT/scripts/servers.sh" "$ROOT/scripts/docker-suite.sh" | grep -vE '^\S+:[0-9]+:\s*#' | grep -q .; then ok
else fail "servers.sh and docker-suite.sh name no container themselves" \
          "$(grep -nE 'go-networkfs-(samba|sss3|ftp|sftp|webdav|mockapi|test)\b' "$ROOT/scripts/servers.sh" "$ROOT/scripts/docker-suite.sh" | grep -vE '^\S+:[0-9]+:\s*#')"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'run-isolation: all checks passed\n'
