#!/usr/bin/env bash
#
# resolve-output-budget.sh — print the path of rust-fs-core's canonical
# scripts/output-budget.sh, or refuse loudly and say how to fix it.
#
# THIS REPOSITORY NO LONGER CARRIES A COPY OF THAT SCRIPT. It used to:
# scripts/output-budget.sh, vendored from fs-linux-test-harness, with a header
# asking whoever changed one copy to remember the other. Nothing checked, and a
# copy that nothing checks is a copy that drifts — measured across this family
# on 2026-09-22, three copies reached four different ways, each repository
# internally consistent and none of them the same file.
#
# So the wrapper is resolved at RUN TIME from a pinned sibling checkout of
# rust-fs-core, and there is NO FALLBACK to a local copy. A missing sibling
# fails this script, which fails the tier, which fails the task. That is the
# point: a fallback is how a repository ends up running last month's wrapper
# for a month without anybody noticing.
#
# WHY A SIBLING CHECKOUT and not the package manager. The Rust drivers ask
# cargo where am-fs-core is, because cargo has already resolved it. There is no
# such answer here: this is a Go module, it has no dependency on core, and
# nothing in the Go toolchain will unpack a Rust crate for it. A pinned sibling
# is the only runtime source available, so `chore siblings` clones one and the
# CI jobs that run a tier call that task.
#
# THE RESOLUTION ORDER IS TWO ENTRIES LONG, and deliberately:
#
#   1. $FS_CORE_ROOT, when set. AUTHORITATIVE — if it is set and does not hold
#      a working wrapper, this fails. It never falls through to the sibling.
#      Two callers need it. scripts/docker-suite.sh bind-mounts the resolved
#      core into the runner container, where ../rust-fs-core does not exist and
#      cannot; and scripts/tests/output-budget-resolver.sh points it at an
#      empty directory and at a broken script to prove that both refusals
#      actually happen.
#   2. <siblings root>/rust-fs-core.
#
# WHERE THE SIBLINGS ROOT IS: beside the MAIN checkout, which is not the same
# place as beside THIS checkout. A worktree lives under whatever directory its
# author chose, so `$REPO/..` resolves somewhere else entirely there, and the
# copy `chore siblings` maintains would not be the copy a tier read.
# --git-common-dir names the main tree from a worktree and from the main
# checkout alike, so both answer the same. `--path-format=absolute` is required
# rather than tidy: without it a main checkout reports a bare `.git` and this
# resolves to `//rust-fs-core`.
#
# THERE IS NO SHA-256 PIN HERE, on purpose. A digest repeated in a dozen
# consumers is exactly the lockstep this migration removes: core could not
# change one line of its own script without a commit in every repository that
# had memorised it, and a repository that forgot would fail with a checksum
# mismatch that says nothing about what changed. The CONTRACT is `--version`.
# It names the API the callers use, it moves when that API moves, and it is
# core's to declare.
set -uo pipefail

# The API string core's wrapper answers `--version` with. A copy that answers
# something else is a DIFFERENT script with the same name — fatal, and not a
# reason to go looking for another one.
EXPECTED_API="rust-fs-core-output-budget 1"

# The first core release that ships scripts/output-budget.sh with --version.
# Named in the refusal so the fix is a version, not a guess. The pin itself is
# FS_CORE_REF in chores.yml; this is the floor the code needs.
CORE_MIN_REF="v0.2.13"

SCRIPT_REL="scripts/output-budget.sh"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

refuse() {
    echo "resolve-output-budget.sh: $1" >&2
    echo "  looked at:    $BUDGET" >&2
    echo "  resolved by:  $ORIGIN" >&2
    echo "  expected:     $EXPECTED_API  (from '$SCRIPT_REL --version')" >&2
    echo "  needs:        rust-fs-core $CORE_MIN_REF or later" >&2
    echo "  fix it with:  chore siblings" >&2
    echo "" >&2
    echo "  The wrapper belongs to rust-fs-core and is deliberately not" >&2
    echo "  committed here. There is no local copy to fall back to." >&2
    exit 1
}

siblings_root() {
    local common
    common="$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
    if [ -n "$common" ] && [ -d "$common" ]; then
        (cd "$common/../.." && pwd)
        return
    fi
    # No git: an exported tarball, or a bind-mount whose .git is a worktree
    # pointer to a directory that was not mounted. Beside this checkout is the
    # only answer left, and it is the right one whenever the two agree.
    (cd "$REPO/.." && pwd)
}

if [ -n "${FS_CORE_ROOT:-}" ]; then
    CORE_ROOT="$FS_CORE_ROOT"
    ORIGIN="FS_CORE_ROOT=$FS_CORE_ROOT"
else
    # `${root%/}` because `pwd` in `/` prints `/`, and the path would then be
    # `//rust-fs-core`. Two leading slashes are implementation-defined in
    # POSIX rather than merely ugly, and the refusal message is the one thing
    # a reader has to be able to paste.
    root="$(siblings_root)"
    CORE_ROOT="${root%/}/rust-fs-core"
    ORIGIN="the pinned sibling beside the main checkout"
fi

BUDGET="$CORE_ROOT/$SCRIPT_REL"

[ -f "$BUDGET" ] || refuse "rust-fs-core has no $SCRIPT_REL there."

version="$(bash "$BUDGET" --version 2>/dev/null)"
[ "$version" = "$EXPECTED_API" ] \
    || refuse "that script answers --version with '${version:-nothing}', not the API this repository calls."

printf '%s\n' "$BUDGET"
