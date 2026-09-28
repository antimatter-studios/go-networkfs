#!/usr/bin/env bash
#
# servers-liveness.sh — a published port that accepts is not a server that
# works, and servers.sh must say so.
#
# `docker run -p` starts a proxy that binds the published port on the HOST as
# soon as the container is created. It accepts connections whether or not the
# process inside is still alive, so a container that exited on startup passes
# a bare port probe and the suite proceeds against a corpse. That cost ninety
# seconds and six passing harnesses once (issue #13): the failure landed in a
# C harness, one driver deep, looking like a driver bug, and the container log
# that said exactly what was wrong was never printed.
#
# THE STUB ANSWERS `build` AS WELL AS `run`, because half the servers are built
# from a Dockerfile in this repository rather than pulled. Which half is not
# this guard's business — it is asserting what servers.sh does with a container
# once it exists — so the stub covers both and the guard does not have to move
# every time a server changes where its image comes from.
#
# THE FIXTURE IS A FAKE DOCKER, NOT A REAL ONE. `chore test:scripts` runs on
# macOS as well as ubuntu and the macOS runner has no daemon, so a guard that
# needed one would be a guard that never ran on half the machines this
# repository is developed on. A stub on PATH answers the six subcommands
# servers.sh calls and is told, per case, whether its container is running.
#
# THE PORT IS REALLY BOUND, because that is the whole defect: anything can
# detect a dead container when nothing is listening. A listener is opened on
# the published port exactly as the docker proxy would leave one, and only
# then is the container reported dead.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

EXPECTED_CHECKS=11
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

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

check_status() { # DESCRIPTION EXPECTED ACTUAL OUTPUT
    if [ "$2" = "$3" ]; then ok; else fail "$1" "exit $3: $(printf '%s' "$4" | tr '\n' '|')"; fi
}

# A LISTENER IS NOT OPTIONAL, so a missing python3 fails rather than skips:
# without one the fake docker's published port refuses connections and the
# guard would prove only that a dead container with nothing listening is
# caught, which is not the bug. Both CI runners ship python3.
if ! command -v python3 >/dev/null 2>&1; then
    printf '  FAIL  python3 is needed to bind the published port this guard probes.\n'
    printf '        Install python3 — there is no version of this check that does not listen.\n'
    exit 1
fi

sandbox="$(mktemp -d)"
listener_pid=""
cleanup() {
    [ -n "$listener_pid" ] && kill "$listener_pid" 2>/dev/null
    rm -rf "$sandbox"
}
trap cleanup EXIT

# --- A port nothing else on this machine is using. -------------------------
port="$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()')"
if [ -n "$port" ]; then ok; else fail "a free port was chosen for the fixture"; fi

# --- The docker proxy, as it survives a container that exited. -------------
python3 -c "import socket, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', $port))
s.listen(16)
time.sleep(300)" &
listener_pid=$!

for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
        exec 3<&- 3>&-
        break
    fi
    sleep 0.2
done
if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    exec 3<&- 3>&-
    ok
else
    fail "the fixture's published port accepts connections"
fi

# --- The fake docker. ------------------------------------------------------
#
# Every subcommand servers.sh reaches for when it brings one pulled server up,
# and nothing else. Whether the container is alive is read from a file, so one
# stub serves both the dead case and the control.
mkdir -p "$sandbox/bin"
cat > "$sandbox/bin/docker" <<'STUB'
#!/usr/bin/env bash
running="$(cat "$FAKE_DOCKER_STATE" 2>/dev/null || echo false)"
case "$1" in
    info)    exit 0 ;;
    network) exit 0 ;;
    image)   exit 0 ;;            # inspect: the image is already local
    pull)    exit 0 ;;
    build)   echo "0123456789ab"; exit 0 ;;
    rm)      exit 0 ;;
    run)     echo "0123456789ab"; exit 0 ;;
    logs)    echo "exec /entrypoint: exec format error" >&2; exit 0 ;;
    inspect)
        # servers.sh asks exactly one question of a container.
        echo "$running"
        exit 0
        ;;
    *) echo "fake docker: unexpected subcommand '$1'" >&2; exit 97 ;;
esac
STUB
chmod +x "$sandbox/bin/docker"

run_up() { # $1 = what .State.Running answers
    echo "$1" > "$sandbox/state"
    (
        cd "$ROOT" || exit 98
        PATH="$sandbox/bin:$PATH" \
        FAKE_DOCKER_STATE="$sandbox/state" \
        SFTP_PORT="$port" \
            scripts/servers.sh up sftp
    ) 2>&1
}

# --- The defect: the port accepts, the container is gone. ------------------
start=$(date +%s)
out="$(run_up false)"; rc=$?
elapsed=$(( $(date +%s) - start ))

check_status "servers.sh up fails when the container exited" 1 "$rc" "$out"
check_contains "it names the container and the port it was waiting on" \
    "go-networkfs-sftp exited before it answered on port $port" "$out"
check_contains "it dumps the container's log, which says what went wrong" \
    "exec format error" "$out"
check_lacks "it does not report a dead server ready" \
    "sftp     127.0.0.1:$port" "$out"

# The pre-fix code probed the port alone, so it either returned ready at once
# or spent the full forty seconds. Neither is this.
if [ "$elapsed" -lt 15 ]; then ok
else fail "it fails on the first pass rather than waiting out the timeout" "took ${elapsed}s"; fi

# --- The control: the same fixture, alive. ---------------------------------
#
# Without it this file would pass against a wait_for_port that failed
# unconditionally, which is a check that cannot tell the two apart.
out="$(run_up true)"; rc=$?
check_status "servers.sh up succeeds when the container is running" 0 "$rc" "$out"
check_contains "it reports where the server is listening" "127.0.0.1:$port" "$out"
check_lacks "it says nothing about the container having exited" "exited before" "$out"

# --- The check is in the script, not only in this guard. -------------------
liveness="$(grep -c 'State.Running' "$ROOT/scripts/servers.sh")"
if [ "$liveness" -ge 1 ]; then ok
else fail "scripts/servers.sh asks docker whether the container is still running"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'servers-liveness: all checks passed\n'
