#!/usr/bin/env bash
#
# merge-group-macos-legs.sh — a merge group runs no macOS leg, and when the
# gate fails, `ci-ok` names a job that never started apart from one that ran
# and failed.
#
# THE QUEUE DROPPED THREE GROUPS IN A ROW ON A RUNNER, NOT ON A TEST (#120).
# Runs 44332862841 and 44337598503: in each merge group both `macos-latest`
# legs of `test` and `build-archives` were created, never got a runner, and
# were cancelled with zero steps after 15-19 minutes. Every Ubuntu job passed,
# `ci-ok` failed the group, and the queue removed the pull request. Darwin is
# proven on the pull request head before it is queued, so a merge group asks
# for Ubuntu alone.
#
# `ci-ok` stays strict -- a cancelled job still fails it -- but its output
# says which gating jobs never ran a step, so a missing runner is not read as
# a broken change.
#
# This guard reads the job-level condition of each platform's jobs, and
# executes `ci-ok`'s own step with `gh` replaced by a stub that answers a
# run's jobs.
#
#   bash scripts/tests/merge-group-macos-legs.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WF="$ROOT/.github/workflows/ci.yml"
fails=0
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# --- 1. The platforms: Ubuntu alone for a merge group, both for the rest. ----
#
# Each platform is its own job with its own gate (a gate answers one runner
# class, so a matrix over os cannot share one). The macOS jobs and their gates
# carry a job-level `if:` that is false for a merge group; the Ubuntu jobs
# carry none beyond the changes job's answer.

# job_if <job>: the job-level `if:` of that job, or nothing.
job_if() {
    awk -v job="$1" '
        $0 ~ "^  " job ":[[:space:]]*$" { injob = 1; next }
        injob && /^  [^ ]/ { exit }
        injob && /^    if:/ { sub(/^    if:[[:space:]]*/, ""); print; exit }
    ' "$WF"
}

has_job() { grep -Eq "^  $1:[[:space:]]*\$" "$WF"; }

path_gate="needs.changes.outputs.code == 'true'"
for job in test-linux test-linux-gate archives-linux archives-linux-gate; do
    if ! has_job "$job"; then
        fail "$WF has no job $job"
        continue
    fi
    got="$(job_if "$job")"
    if [ "$got" != "$path_gate" ]; then
        fail "$job must run on every event its changes job asks for" "if: $got"
    fi
done
for job in test-macos test-macos-gate archives-macos archives-macos-gate; do
    if ! has_job "$job"; then
        fail "$WF has no job $job"
        continue
    fi
    got="$(job_if "$job")"
    if [ "$got" != "$path_gate && github.event_name != 'merge_group'" ]; then
        fail "$job must be skipped in a merge group, and otherwise run when the changes job asks" "if: $got"
    fi
done

# --- 2. ci-ok names a job that never started. ---------------------------------

gate_script="$(awk '
    /^[[:space:]]*- name: every gating job must have concluded success[[:space:]]*$/ { want = 1; next }
    want && /^[[:space:]]+run: \|[[:space:]]*$/ { block = 1; next }
    block {
        if ($0 ~ /^          /) { sub(/^          /, ""); print; next }
        if ($0 ~ /^[[:space:]]*$/) { print ""; next }
        exit
    }
' "$WF")"
[ -n "$gate_script" ] || { echo "merge-group-macos-legs.sh: no ci-ok gate script in $WF" >&2; exit 1; }

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin"

# A run's jobs: one leg that never got a runner, one that ran and failed, and
# ci-ok itself, still in progress.
cat > "$sandbox/jobs.json" <<'JSON'
{"total_count":4,"jobs":[
 {"name":"test (macos-latest)","status":"completed","conclusion":"cancelled","runner_name":"","steps":[]},
 {"name":"lint","status":"completed","conclusion":"failure","runner_name":"GitHub Actions 7","steps":[{"name":"Set up job","conclusion":"success"},{"name":"chore lint","conclusion":"failure"}]},
 {"name":"vulncheck","status":"completed","conclusion":"success","runner_name":"GitHub Actions 8","steps":[{"name":"Set up job","conclusion":"success"}]},
 {"name":"ci-ok","status":"in_progress","conclusion":null,"runner_name":"GitHub Actions 9","steps":[]}
]}
JSON

cat > "$sandbox/bin/gh" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$GH_CALLS"
[ "${GH_FAIL:-0}" = 1 ] && { echo "gh: HTTP 403" >&2; exit 1; }
cat "$JOBS_JSON"
SH
chmod +x "$sandbox/bin/gh"

# run_gate <needs-json> <gh-fails> [event]: the gate, with the variables GitHub
# sets.
run_gate() {
    : > "$sandbox/calls"
    env -i \
        PATH="$sandbox/bin:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
        HOME="$sandbox" \
        GH_CALLS="$sandbox/calls" \
        GH_FAIL="$2" \
        GH_TOKEN=stub \
        JOBS_JSON="$sandbox/jobs.json" \
        GITHUB_REPOSITORY=antimatter-studios/go-networkfs \
        GITHUB_RUN_ID=44332862841 \
        GITHUB_RUN_ATTEMPT=1 \
        NEEDS="$1" \
        CODE=true \
        EVENT="${3:-pull_request}" \
        bash -c "$gate_script" >"$sandbox/out" 2>&1
}

failing='{"test":{"result":"cancelled"},"lint":{"result":"failure"},"vulncheck":{"result":"success"}}'
passing='{"test":{"result":"success"},"lint":{"result":"success"},"vulncheck":{"result":"success"}}'

# 2a. A failed gate lists the never-started job apart from the failed one.
if run_gate "$failing" 0; then
    fail "ci-ok passed with a cancelled and a failed job" "$(tail -3 "$sandbox/out")"
fi
if ! grep -q "never started (no runner assigned): test (macos-latest)" "$sandbox/out"; then
    fail "ci-ok did not name test (macos-latest) as never started" "$(tail -3 "$sandbox/out")"
fi
if grep "never started" "$sandbox/out" | grep -q "lint"; then
    fail "ci-ok called lint, which ran steps, never started"
fi
if ! grep -Eq "failed: .*lint" "$sandbox/out"; then
    fail "ci-ok did not name lint as failed" "$(tail -3 "$sandbox/out")"
fi
if grep -Eq "(never started|failed): .*(vulncheck|ci-ok)" "$sandbox/out"; then
    fail "ci-ok named a job that succeeded or is still running"
fi
if ! grep -q "actions/runs/44332862841/attempts/1/jobs" "$sandbox/calls"; then
    fail "ci-ok did not ask for this run attempt's jobs" "$(head -1 "$sandbox/calls")"
fi

# 2b. A diagnostic that cannot be fetched changes nothing about the verdict.
if run_gate "$failing" 1; then
    fail "ci-ok passed a failed gate because the jobs could not be fetched"
fi
if ! run_gate "$passing" 1; then
    fail "ci-ok failed a passing gate" "$(tail -3 "$sandbox/out")"
fi

# 2c. A merge group skips the macOS jobs, and only those, only there.
merge_ok='{"test-linux":{"result":"success"},"test-macos-gate":{"result":"skipped"},"test-macos":{"result":"skipped"},"archives-macos-gate":{"result":"skipped"},"archives-macos":{"result":"skipped"}}'
if ! run_gate "$merge_ok" 1 merge_group; then
    fail "ci-ok failed a merge group that skipped only the macOS jobs" "$(tail -3 "$sandbox/out")"
fi
if run_gate "$merge_ok" 1 pull_request; then
    fail "ci-ok passed a pull request whose macOS jobs were skipped"
fi
linux_skipped='{"test-linux":{"result":"skipped"},"test-macos":{"result":"skipped"}}'
if run_gate "$linux_skipped" 1 merge_group; then
    fail "ci-ok passed a merge group whose Linux test job was skipped"
fi
macos_failed='{"test-linux":{"result":"success"},"test-macos":{"result":"failure"}}'
if run_gate "$macos_failed" 1 merge_group; then
    fail "ci-ok passed a merge group whose macOS job failed"
fi

exit "$fails"
