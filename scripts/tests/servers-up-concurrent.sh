#!/usr/bin/env bash
#
# servers-up-concurrent.sh — `servers.sh up` starts independent servers side by
# side, a bounded number at a time, and a run still reads and fails as one.
#
# Eleven servers one after another took 162s and 185s of the integration job's
# five minutes on a self-hosted runner, so the suite was cancelled while every
# test it had reached was passing. Each server has its own image, container and
# port; nothing but the network has to exist before them.
#
# THE PROOF IS A RENDEZVOUS, NOT A STOPWATCH. The fake `docker build` holds until
# the expected number of builds have begun. Servers started one at a time can
# never satisfy it, so the check does not depend on how fast this machine is.
#
# THE FIXTURE IS A FAKE DOCKER for the reason servers-liveness.sh gives: the
# macOS runner has no daemon. The published ports are really bound, because
# servers.sh really probes them.
#
# Quiet on success: this tier's output is budgeted.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

EXPECTED_CHECKS=14
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

check_equal() { # DESCRIPTION EXPECTED ACTUAL
    if [ "$2" = "$3" ]; then ok; else fail "$1" "expected '$2', got '$3'"; fi
}

check_contains() { # DESCRIPTION NEEDLE HAYSTACK
    case "$3" in
        *"$2"*) ok ;;
        *)      fail "$1" "no '$2' in: $(printf '%s' "$3" | tr '\n' '|')" ;;
    esac
}

check_lacks() { # DESCRIPTION NEEDLE HAYSTACK
    case "$3" in
        *"$2"*) fail "$1" "found '$2' in: $(printf '%s' "$3" | tr '\n' '|')" ;;
        *)      ok ;;
    esac
}

if ! command -v python3 >/dev/null 2>&1; then
    printf '  FAIL  python3 is needed to bind the published ports this guard probes.\n'
    printf '        Install python3 — servers.sh waits on those ports.\n'
    exit 1
fi

sandbox="$(mktemp -d)"
listener_pid=""
cleanup() {
    if [ -n "$listener_pid" ]; then
        kill "$listener_pid" 2>/dev/null
        wait "$listener_pid" 2>/dev/null   # bash 3.2 reports the kill otherwise
    fi
    rm -rf "$sandbox"
}
trap cleanup EXIT

# --- Six published ports, held open for the whole file. ---------------------
python3 -c "import os, socket, sys, time
held = []
for _ in range(6):
    s = socket.socket()
    s.bind(('127.0.0.1', 0))
    s.listen(64)
    held.append(s)
tmp = sys.argv[1] + '.tmp'
with open(tmp, 'w') as f:
    f.write(' '.join(str(s.getsockname()[1]) for s in held))
os.rename(tmp, sys.argv[1])
time.sleep(120)" "$sandbox/ports" &
listener_pid=$!

for _ in $(seq 1 50); do
    [ -f "$sandbox/ports" ] && break
    sleep 0.1
done
read -r port_sftp port_squid port_samba port_mockapi port_sabredav port_lighttpd \
    < "$sandbox/ports" || true
if [ -n "${port_lighttpd:-}" ]; then ok; else
    fail "the fixture's published ports are bound"
    exit 1
fi

# --- The fake docker. -------------------------------------------------------
#
# A server is in flight from its `docker build` until its `docker run` is done.
# Every build records how many were in flight as it began, then waits at the
# rendezvous and lingers long enough for an unbounded start to show itself.
mkdir -p "$sandbox/bin"
cat > "$sandbox/bin/docker" <<'STUB'
#!/usr/bin/env bash
previous=""
count() { ls "$1" | wc -l | tr -d ' '; }
call() { printf '%s\n' "$*" >> "$FAKE_DIR/calls"; }
case "$1" in
    info|image|rm|logs) exit 0 ;;
    inspect) echo true; exit 0 ;;
    network)
        call "network $2"
        [ "$2" = create ]
        exit
        ;;
    build)
        call build
        tag=""
        while [ $# -gt 0 ]; do
            [ "$1" = -t ] && { tag="$2"; break; }
            shift
        done
        : > "$FAKE_DIR/started/${tag//[\/:]/_}"
        echo $(( $(count "$FAKE_DIR/started") - $(count "$FAKE_DIR/finished") )) \
            >> "$FAKE_DIR/in-flight"
        for _ in $(seq 1 50); do
            [ "$(count "$FAKE_DIR/started")" -ge "${FAKE_RENDEZVOUS:-0}" ] && break
            sleep 0.1
        done
        if [ "$(count "$FAKE_DIR/started")" -lt "${FAKE_RENDEZVOUS:-0}" ]; then
            : > "$FAKE_DIR/rendezvous-missed"
            echo "fake docker: nothing else started beside $tag" >&2
            exit 1
        fi
        sleep 0.3
        exit 0
        ;;
    run)
        call "run $*"
        name=""
        for arg in "$@"; do
            [ "$previous" = --name ] && name="$arg"
            previous="$arg"
            image="$arg"
        done
        if [ "$name" = "${FAKE_FAIL:-}" ]; then
            echo "fake docker: $name would not start" >&2
            exit 1
        fi
        [ "$name" = "${FAKE_SLOW:-}" ] && sleep 1
        : > "$FAKE_DIR/finished/${image//[\/:]/_}"
        echo "$name" >> "$FAKE_DIR/completed"
        exit 0
        ;;
    *) echo "fake docker: unexpected subcommand '$1'" >&2; exit 97 ;;
esac
STUB
chmod +x "$sandbox/bin/docker"

NAMES="sftp squid samba mockapi sabredav lighttpd"

# Fills $rc, $out and $err. Extra VAR=value arguments reach servers.sh.
run_up() {
    rm -rf "$sandbox/state"
    mkdir -p "$sandbox/state/started" "$sandbox/state/finished"
    # shellcheck disable=SC2086  # the words are the point
    (
        cd "$ROOT" || exit 98
        env PATH="$sandbox/bin:$PATH" FAKE_DIR="$sandbox/state" \
            SFTP_PORT="$port_sftp" PROXY_PORT="$port_squid" \
            SMB_PORT="$port_samba" MOCK_PORT="$port_mockapi" \
            DAV_SABRE_PORT="$port_sabredav" DAV_LIGHTTPD_PORT="$port_lighttpd" \
            "$@" scripts/servers.sh up $NAMES
    ) > "$sandbox/stdout" 2> "$sandbox/stderr"
    rc=$?
    out="$(cat "$sandbox/stdout")"
    err="$(cat "$sandbox/stderr")"
}

peak_in_flight() { sort -n "$sandbox/state/in-flight" 2>/dev/null | tail -1; }

# --- Servers start together, as many as the limit allows and no more. -------
run_up SERVERS_UP_JOBS=2 FAKE_RENDEZVOUS=2
check_equal "two servers are in flight at once when the limit is two" 0 "$rc"
if [ -e "$sandbox/state/rendezvous-missed" ]; then
    fail "a second server starts while the first is still building" "$err"
else ok; fi
check_equal "the limit of two is never exceeded" 2 "$(peak_in_flight)"

# --- The default is a bound as well. ----------------------------------------
run_up FAKE_RENDEZVOUS=4
check_equal "four servers start together by default" 0 "$rc"
check_equal "no more than four are in flight by default" 4 "$(peak_in_flight)"

# --- The network exists before anything needs it. ---------------------------
check_equal "the network is created before the first server" \
    "network create" "$(grep -v 'network inspect' "$sandbox/state/calls" | head -1)"
check_equal "every container joins the test network" 6 \
    "$(grep -c -- '--network go-networkfs-test ' "$sandbox/state/calls")"

# --- The report keeps the order asked for, whatever finishes first. ---------
run_up SERVERS_UP_JOBS=6 FAKE_SLOW=go-networkfs-sftp
check_equal "the first server named is the last to finish here" \
    go-networkfs-sftp "$(tail -1 "$sandbox/state/completed" 2>/dev/null)"
check_equal "ready lines follow the order the servers were named in" \
    "$NAMES" "$(printf '%s\n' "$out" | awk '{print $1}' | tr '\n' ' ' | sed 's/ $//')"

# --- A failure anywhere is fatal, not only one in the last server. ----------
run_up SERVERS_UP_JOBS=6 FAKE_FAIL=go-networkfs-sftp
check_equal "one server failing fails the whole command" 1 "$rc"
check_contains "the failing server's own error is printed" \
    "go-networkfs-sftp would not start" "$err"
check_lacks "the failed server is not reported ready" "sftp     127.0.0.1" "$out"

# --- A limit that could never start anything is refused. --------------------
run_up SERVERS_UP_JOBS=0
if [ "$rc" -ne 0 ] && [ ! -e "$sandbox/state/calls" ]; then ok
else fail "SERVERS_UP_JOBS=0 is refused before docker is used" "exit $rc: $err"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'servers-up-concurrent: all checks passed\n'
