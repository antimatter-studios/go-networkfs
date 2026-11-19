#!/usr/bin/env bash
#
# runner-pick-min-free.sh — a gated pick keeps two pool slots spare, so a job
# the gate places on antimatter-vps is one a runner there can actually claim.
#
# A PICK THAT COUNTS THE CAP STRANDS A JOB FOREVER. The antimatter-vps pool
# runs two runners, but the gate counts the organisation's cap, which it
# reports as four. Three picks made together all saw room and all answered
# antimatter-vps-linux-x64; two jobs were dispatched to the two runners, and
# the third's two-minute reservation lapsed before anything could claim it.
# Run 44497936802: `integration` was queued at 09:36:50Z, was never claimed by
# any runner, and was still queued eleven minutes later, holding the whole run
# open with it.
#
# The gate answers the preferred label only when at least `min_free` slots
# would still be free after the pick takes one (measured: free=4,min_free=3
# prefers; free=4,min_free=4 falls back; free=2,min_free=1 prefers;
# free=1,min_free=2 falls back). With a cap of four and a pool of two,
# min_free=2 admits a pick only while the pool itself has a runner free.
#
# This guard holds the contract in three parts:
#   1. runner-pick.yml sends min_free=2 to the gate when given min_free: 2;
#   2. it sends no min_free at all when the input is empty or "0", so other
#      repositories calling this reusable workflow are unaffected, and it
#      falls back without asking when the input is not an integer;
#   3. ci.yml passes min_free: 2 to every job that calls runner-pick.yml.
# The pick's own script runs, with `curl` replaced by a stub that records
# every argument it is given.
#
#   bash scripts/tests/runner-pick-min-free.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/runner-pick.yml"
CI="$ROOT/.github/workflows/ci.yml"
fails=0
checks=0
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
[ -n "$pick_script" ] || { echo "runner-pick-min-free.sh: no Pick runners script in $WF" >&2; exit 1; }

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin"

# A gate that always has room, and records every call made to it.
cat > "$sandbox/bin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_CALLS"
echo "antimatter-vps-linux-x64"
SH
chmod +x "$sandbox/bin/curl"

# run_pick <min_free>: the pick of one pull request job by the owner, with the
# files GitHub gives a step. Leaves the output in $runners.
run_pick() {
    local min_free="$1"
    : > "$sandbox/calls"
    : > "$sandbox/summary"
    : > "$sandbox/output"
    env -i \
        PATH="$sandbox/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
        HOME="$sandbox" \
        CURL_CALLS="$sandbox/calls" \
        ORG=antimatter-studios \
        ACTOR=christhomas \
        AUTHOR=christhomas \
        SENDER=christhomas \
        GH_ACTOR=christhomas \
        RUN_ID=1 \
        RUN_ATTEMPT=1 \
        JOBS="integration" \
        OWNER=christhomas \
        PREFER=antimatter-vps-linux-x64 \
        FALLBACK=ubuntu-24.04 \
        MIN_FREE="$min_free" \
        EVENT=pull_request \
        GITHUB_STEP_SUMMARY="$sandbox/summary" \
        GITHUB_OUTPUT="$sandbox/output" \
        bash -c "$pick_script" >/dev/null 2>"$sandbox/stderr"
    local status=$?
    if [ "$status" -ne 0 ]; then
        fail "the pick with min_free='$min_free' exited $status" "$(tail -3 "$sandbox/stderr")"
        return 1
    fi
    runners="$(sed -n 's/^runners=//p' "$sandbox/output" | tail -1)"
    [ -n "$runners" ] || { fail "the pick with min_free='$min_free' wrote no runners output"; return 1; }
    return 0
}

# --- 1. min_free: 2 reaches the gate as min_free=2. ------------------------
checks=$((checks + 1))
if run_pick 2; then
    if ! grep -q -- 'min_free=2' "$sandbox/calls"; then
        fail "a pick given min_free 2 did not send min_free=2 to the gate" \
            "gate call: $(head -1 "$sandbox/calls")"
    fi
fi

# --- 2. An empty or zero min_free sends no parameter at all. --------------
for value in "" 0; do
    checks=$((checks + 1))
    if run_pick "$value"; then
        if [ ! -s "$sandbox/calls" ]; then
            fail "a pick given min_free '$value' did not ask the gate"
        elif grep -q -- 'min_free' "$sandbox/calls"; then
            fail "a pick given min_free '$value' sent a min_free parameter" \
                "gate call: $(head -1 "$sandbox/calls")"
        fi
    fi
done

# --- 2b. A min_free that is not an integer falls back without asking. -----
checks=$((checks + 1))
if run_pick "two"; then
    got="$(echo "$runners" | jq -r '.integration')"
    if [ "$got" != "ubuntu-24.04" ]; then
        fail "a pick given min_free 'two' answered '$got', not the fallback"
    fi
    if [ -s "$sandbox/calls" ]; then
        fail "a pick given min_free 'two' asked the gate" "gate call: $(head -1 "$sandbox/calls")"
    fi
fi

# --- 3. ci.yml passes min_free: 2 to every runner-pick.yml caller. ---------
# A job is a two-space-indented key under `jobs:`; its block runs to the next.
callers="$(awk '
    /^jobs:/ { injobs = 1; next }
    injobs && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ {
        if (job != "" && uses) print job, (mf ? "yes" : "no")
        job = $1; sub(/:$/, "", job); uses = 0; mf = 0; next
    }
    injobs && /uses:[[:space:]]*\.\/\.github\/workflows\/runner-pick\.yml/ { uses = 1 }
    injobs && /^[[:space:]]+min_free:[[:space:]]*["\x27]?2["\x27]?[[:space:]]*$/ { mf = 1 }
    END { if (job != "" && uses) print job, (mf ? "yes" : "no") }
' "$CI")"
checks=$((checks + 1))
if [ -z "$callers" ]; then
    fail "ci.yml has no job that calls runner-pick.yml"
fi
for want in runner-changes runner-test runner-integration runner-lint runner-vulncheck; do
    if ! echo "$callers" | grep -q "^$want "; then
        fail "ci.yml has no $want job calling runner-pick.yml"
    fi
done
while read -r job has; do
    [ -n "$job" ] || continue
    checks=$((checks + 1))
    [ "$has" = yes ] || fail "ci.yml job $job calls runner-pick.yml without min_free: 2"
done <<<"$callers"

if [ "$fails" -eq 0 ]; then
    echo "runner-pick-min-free.sh: PASS ($checks checks)"
fi
exit "$fails"
