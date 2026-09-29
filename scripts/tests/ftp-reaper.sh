#!/usr/bin/env bash
#
# ftp-reaper.sh — the FTP test server is started so that vsftpd is never the
# parent of a process it did not fork.
#
# WHY (issue #27). vsftpd's standalone listener reaps children in a SIGCHLD
# handler that treats every reaped pid as one of its sessions and dereferences
# the lookup unchecked, so any other child is a SIGSEGV and the container exits
# 139. Run as the image ships it, the listener is PID 1 — it inherits every
# orphaned session process — and the image entrypoint backgrounds two log
# pipelines and then execs it, so those are its children as well. The server
# died after a handful of client sessions and the next test found nothing.
#
# ftp/ftp_integration_test.go is the behavioural proof, and it needs the
# server. This guard is the part that runs everywhere, with no Docker: it holds
# servers.sh to the three things the fix is made of.
#
# Quiet on success: this tier's output is budgeted.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SERVERS="$ROOT/scripts/servers.sh"

EXPECTED_CHECKS=3
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# The body of up_ftp, and nothing else in the file.
body="$(awk '/^up_ftp\(\) \{/{f=1} f{print} f&&/^\}/{exit}' "$SERVERS")"

# 1. An init at PID 1 takes the orphans.
case "$body" in
    *"docker run -d --init "*) ok ;;
    *) fail "up_ftp runs the container with --init" \
            "without it vsftpd is PID 1 and reaps every orphaned session process" ;;
esac

# 2. The image's entrypoint, which leaves its log pipelines as vsftpd's
#    children, is replaced.
case "$body" in
    *"--entrypoint "*) ok ;;
    *) fail "up_ftp replaces the image entrypoint" \
            "the image's backgrounds two tail|tee pipelines, then execs vsftpd over itself" ;;
esac

# 3. Anything the replacement backgrounds is orphaned at once, from a subshell,
#    so it belongs to the init and not to the process the shell becomes.
bare="$(printf '%s\n' "$body" | grep -E '&[[:space:]]*(\)|$)' | grep -vE '\([^()]*&[[:space:]]*\)' || true)"
if [ -z "$bare" ]; then ok
else fail "up_ftp's entrypoint backgrounds nothing vsftpd would inherit" \
          "$(printf '%s' "$bare" | tr '\n' '|')"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'ftp-reaper: all checks passed\n'
