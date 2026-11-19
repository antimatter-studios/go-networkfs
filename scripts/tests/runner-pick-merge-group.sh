#!/usr/bin/env bash
#
# runner-pick-merge-group.sh — a merge group is never placed on a runner that
# holds a reservation, because a merge group is the one run that must not lose
# its place in the queue.
#
# THE QUEUE DROPPED #119 ON A RESERVATION, NOT ON A TEST. Run 44494822694, a
# merge group: the gate placed `test`, `lint` and `vulncheck` on
# antimatter-vps, the host had two runners, and `vulncheck` was dispatched
# 195 seconds after its pick. The runner's start hook refused it -- "started
# 76 s after its runner reservation ran out (reservations last 120 s)" -- and
# `ci-ok` failed the group on a job that never ran a step. A pull request can
# be re-run; a queue entry is removed and has to be enqueued again.
#
# So the pick answers the fallback for every job when the event is
# `merge_group`, and asks the gate for every other event. This guard executes
# the pick's own script, with `curl` replaced by a stub that always offers the
# vps runner, and asserts both halves.
#
#   bash scripts/tests/runner-pick-merge-group.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/runner-pick.yml"
fails=0
ok()   { :; }
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# The pick's step is the only `run: |` block in the workflow. Take its lines
# at their own indentation rather than teaching this guard YAML.
pick_script="$(awk '
    /^[[:space:]]*- name: Pick runners[[:space:]]*$/ { want = 1; next }
    want && /^[[:space:]]+run: \|[[:space:]]*$/ { block = 1; next }
    block {
        if ($0 ~ /^          /) { sub(/^          /, ""); print; next }
        if ($0 ~ /^[[:space:]]*$/) { print ""; next }
        exit
    }
' "$WF")"
[ -n "$pick_script" ] || { echo "runner-pick-merge-group.sh: no Pick runners script in $WF" >&2; exit 1; }

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin"

# A gate that always has room. Every call is recorded, so a pick that should
# not have asked can be told from one that asked and was refused.
cat > "$sandbox/bin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_CALLS"
echo "antimatter-vps-linux-x64"
SH
chmod +x "$sandbox/bin/curl"

# run_pick <event> <actor>: the pick, with the files GitHub gives a step.
run_pick() {
    local event="$1" actor="$2"
    : > "$sandbox/calls"
    : > "$sandbox/summary"
    : > "$sandbox/output"
    env -i \
        PATH="$sandbox/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
        HOME="$sandbox" \
        CURL_CALLS="$sandbox/calls" \
        ORG=antimatter-studios \
        ACTOR="$actor" \
        AUTHOR="$actor" \
        SENDER="$actor" \
        GH_ACTOR="$actor" \
        RUN_ID=1 \
        RUN_ATTEMPT=1 \
        JOBS="lint vulncheck test-ubuntu" \
        OWNER=christhomas \
        PREFER=antimatter-vps-linux-x64 \
        FALLBACK=ubuntu-24.04 \
        EVENT="$event" \
        GITHUB_STEP_SUMMARY="$sandbox/summary" \
        GITHUB_OUTPUT="$sandbox/output" \
        bash -c "$pick_script" >/dev/null 2>"$sandbox/stderr"
    local status=$?
    if [ "$status" -ne 0 ]; then
        fail "$event pick exited $status" "$(tail -3 "$sandbox/stderr")"
        return 1
    fi
    runners="$(sed -n 's/^runners=//p' "$sandbox/output" | tail -1)"
    [ -n "$runners" ] || { fail "$event pick wrote no runners output"; return 1; }
    return 0
}

# collisions: every job in the output, as `job=runner` lines.
collisions() { echo "$1" | jq -r 'to_entries[] | "\(.key)=\(.value)"' | sort; }

# --- 1. A merge group is never placed. ------------------------------------
if run_pick merge_group christhomas; then
    got="$(collisions "$runners")"
    want="$(printf 'lint=ubuntu-24.04\ntest-ubuntu=ubuntu-24.04\nvulncheck=ubuntu-24.04\n')"
    if [ "$got" = "$want" ]; then
        ok
    else
        fail "a merge group was placed on the gate's runner" "got: $(echo "$got" | tr '\n' ' ')"
    fi
    if [ -s "$sandbox/calls" ]; then
        fail "a merge group asked the gate" "$(head -1 "$sandbox/calls")"
    fi
fi

# --- 2. A pull request still asks, and still gets the vps runner. ---------
if run_pick pull_request christhomas; then
    got="$(collisions "$runners")"
    want="$(printf 'lint=antimatter-vps-linux-x64\ntest-ubuntu=antimatter-vps-linux-x64\nvulncheck=antimatter-vps-linux-x64\n')"
    if [ "$got" = "$want" ]; then
        ok
    else
        fail "a pull request no longer uses the gate" "got: $(echo "$got" | tr '\n' ' ')"
    fi
    if [ ! -s "$sandbox/calls" ]; then
        fail "a pull request did not ask the gate"
    fi
fi

# --- 3. Someone else's run is never placed, whatever the event. -----------
if run_pick pull_request someone-else; then
    got="$(collisions "$runners")"
    want="$(printf 'lint=ubuntu-24.04\ntest-ubuntu=ubuntu-24.04\nvulncheck=ubuntu-24.04\n')"
    if [ "$got" = "$want" ]; then
        ok
    else
        fail "another actor's run was placed" "got: $(echo "$got" | tr '\n' ' ')"
    fi
fi

exit "$fails"
