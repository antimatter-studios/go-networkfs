#!/usr/bin/env bash
# tier.sh LABEL LOG-NAME MAX-LINES MAX-BYTES -- COMMAND [ARG...]
#
# One tier of the suite, run QUIETLY and under a budget. The whole run goes to
# tmp/logs/<LOG-NAME>.log, a pass prints one verdict line naming the log, a
# failure prints one line naming the log and the command's own exit status, and
# a run that passed but printed more than its budget fails with status 65.
#
# WHY THE BUDGET IS PART OF THE TASK and not a CI-only check: the reader who
# pays most for a noisy suite is the one running it locally, and a rule that
# only CI enforces is a rule the tree drifts away from between pull requests.
#
# The budgets themselves are in chores.yml, next to the command each one
# bounds, and every one of them was MEASURED — see the table there. Raise one
# deliberately when a tier grows; a budget nobody can breach measures nothing.
#
# THE WORK IS DONE BY rust-fs-core's scripts/output-budget.sh, which is NOT
# committed here. scripts/resolve-output-budget.sh finds it in the pinned
# sibling checkout (`chore siblings`) and refuses loudly when it cannot; this
# file copies it into tmp/ for the run, deletes the copy on the way out, and
# owns only the part that is ours — which tiers exist and what each may print.
# The copy is what makes a run hermetic: a `git checkout` in the sibling
# halfway through a suite cannot change the wrapper underneath it.
#
# VERBOSE. `OUTPUT_BUDGET_VERBOSE=1`, or `--verbose`/`-v` in the chore
# invocation's CLI_ARGS (`chore test:unit -- --verbose`), streams the run as it
# happens as well as logging it. It does NOT lift the budget: the log is the
# same size either way, and a tier that has outgrown its budget should say so
# whether or not anybody was watching.
#
# THE VARIABLES WERE FLTH_* UNTIL THE WRAPPER MOVED TO rust-fs-core, and this
# is where somebody grepping for the old names should land:
#
#   FLTH_VERBOSE   -> OUTPUT_BUDGET_VERBOSE
#   FLTH_FAIL_TAIL -> OUTPUT_BUDGET_FAIL_TAIL
#
# That rename fails SILENTLY where it is missed: nothing errors, `--verbose`
# simply stops working and the run stays quiet. Core prints a line when it sees
# an FLTH_* variable set, which catches a shell that still exports one; nothing
# catches a script that still reads one, so they were renamed everywhere in
# this repository at once (scripts/servers.sh and scripts/docker-suite.sh too).
#
# A FAILING TIER NO LONGER READS ITS LOG ALOUD. Core defaults `--tail` to 0:
# the tail is usually not where the assertion is, and an agent re-reading its
# transcript pays for those lines on every later step. The verdict names the
# log and its length; `OUTPUT_BUDGET_FAIL_TAIL=40 chore test:unit` brings the
# old behaviour back for a person at a terminal.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

[ $# -ge 5 ] || { echo "tier.sh: usage: tier.sh LABEL LOG MAX-LINES MAX-BYTES -- CMD..." >&2; exit 2; }
LABEL="$1"; LOG_NAME="$2"; MAX_LINES="$3"; MAX_BYTES="$4"; shift 4
[ "${1:-}" = "--" ] && shift
[ $# -gt 0 ] || { echo "tier.sh: no command" >&2; exit 2; }

# The resolver prints the path and explains itself on stderr when it cannot.
# Its refusal is the whole error message; adding one here would bury it.
CANONICAL="$("$REPO/scripts/resolve-output-budget.sh")"

# tmp/ is gitignored and is where the tier logs already live. mktemp rather
# than $$, because `chore test` runs one tier INSIDE another: the outer one is
# on this host and the inner one is in the runner container, which has its own
# PID namespace and its own low PIDs, and both write into this same mounted
# directory. Two tiers that picked the same name would have each other's copy
# deleted out from under them by whichever trap fired first.
mkdir -p "$REPO/tmp"
BUDGET="$(mktemp "$REPO/tmp/output-budget.XXXXXXXX")"
trap 'rm -f "$BUDGET"' EXIT
cp "$CANONICAL" "$BUDGET"

# `chore test:unit -- --verbose` arrives as CLI_ARGS. output-budget.sh reads
# OUTPUT_BUDGET_VERBOSE itself, so mapping the flag onto it is all that is
# needed — and it means the environment variable and the flag cannot disagree.
case " ${CLI_ARGS:-} " in
    *" --verbose "*|*" -v "*) export OUTPUT_BUDGET_VERBOSE=1 ;;
esac

# NOT `exec`: the trap has to run, and exec replaces this shell. The command's
# own status is passed on unchanged, which is what the tiers and the tests
# depend on.
bash "$BUDGET" \
    --log "$REPO/tmp/logs/$LOG_NAME.log" \
    --max-lines "$MAX_LINES" \
    --max-bytes "$MAX_BYTES" \
    --label "$LABEL" \
    -- "$@"
