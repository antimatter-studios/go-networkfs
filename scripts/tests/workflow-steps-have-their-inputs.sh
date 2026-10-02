#!/usr/bin/env bash
#
# workflow-steps-have-their-inputs.sh — a step in any workflow gets what it
# needs to do its job, rather than failing (or uploading nothing) at run time.
#
# THE NIGHTLY FUZZ RUN NEVER FUZZED. fuzz.yml ran scripts/ci-install-chore.sh
# without CHORE_VERSION, which ci.yml declares and fuzz.yml did not, so every
# scheduled run since the explorer landed (#32) stopped at
#   scripts/ci-install-chore.sh: line 19: CHORE_VERSION: set CHORE_VERSION
# A scheduled workflow reports to nobody, and no pull request runs it, so the
# failure was four nights old before anyone looked (#38).
#
# AND ITS FINDINGS WOULD NOT HAVE BEEN KEPT. Its upload listed
# '**/testdata/fuzz/**' inside a `path: |` block, where quotes are not YAML
# syntax but part of the text: the path searched for began with a quote and
# matched nothing, so a crashing input -- the one file the job exists to hand
# back -- was never uploaded.
#
# Checked for every workflow, not only fuzz.yml.
#
#   bash scripts/tests/workflow-steps-have-their-inputs.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0
ok()   { :; }
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# scan <workflow>...: one line per problem. Text, not a YAML parser, so it
# runs on every leg of the matrix with nothing installed:
#   * a workflow that runs ci-install-chore.sh declares CHORE_VERSION as a
#     key somewhere -- workflow, job or step env, all inherited by the step;
#   * a line that is ONLY a quoted string can only be inside a block scalar
#     (a mapping value has its key, a list item its dash), and there the
#     quotes are text, so the path it names can never match.
scan() {
    local wf
    for wf in "$@"; do
        if grep -q 'ci-install-chore\.sh' "$wf" && ! grep -qE '^[[:space:]]*CHORE_VERSION:' "$wf"; then
            echo "$wf: runs ci-install-chore.sh with no CHORE_VERSION in the workflow, job or step env"
        fi
        grep -nE "^[[:space:]]+(\"[^\"]*\"|'[^']*')[[:space:]]*$" "$wf" | while IFS= read -r hit; do
            echo "$wf:${hit%%:*}: ${hit#*:} is a quoted line inside a block, so the quotes are part of the path and it matches nothing"
        done
    done
}

# --- 1. The scan sees each defect it exists for. --------------------------
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
cat > "$sandbox/bad.yml" <<'YML'
on: push
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - run: scripts/ci-install-chore.sh
      - uses: actions/upload-artifact@v4
        with:
          path: |
            tmp/logs/
            '**/testdata/fuzz/**'
YML
cat > "$sandbox/good.yml" <<'YML'
on: push
env:
  CHORE_VERSION: "0.11.0"
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - run: scripts/ci-install-chore.sh
      - uses: actions/upload-artifact@v4
        with:
          path: |
            tmp/logs/
            **/testdata/fuzz/**
YML
bad="$(scan "$sandbox/bad.yml")"
case "$bad" in *"no CHORE_VERSION"*) ok ;; *) fail "the scan finds an install with no CHORE_VERSION" "$bad" ;; esac
case "$bad" in *"quoted line inside a block"*) ok ;; *) fail "the scan finds a quoted upload path" "$bad" ;; esac
good="$(scan "$sandbox/good.yml")"
[ -z "$good" ] && ok || fail "the scan passes a workflow with neither defect" "$good"

# --- 2. This repository's workflows. --------------------------------------
shopt -s nullglob
workflows=("$ROOT"/.github/workflows/*.yml "$ROOT"/.github/workflows/*.yaml)
[ "${#workflows[@]}" -gt 0 ] || fail "found no workflows to check"
found="$(scan "${workflows[@]}")"
[ -z "$found" ] && ok || fail "every workflow step has what it needs" "${found//$ROOT\//}"

[ "$fails" -eq 0 ] || { printf '  %s check(s) failed\n' "$fails"; exit 1; }
printf 'workflow-steps-have-their-inputs: all checks passed\n'
