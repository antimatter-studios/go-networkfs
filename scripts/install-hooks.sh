#!/usr/bin/env bash
#
# install-hooks.sh — put this repository's fast pre-commit checks where git
# will find them, without switching anything else off. Idempotent; run once
# per fresh clone.
#
# WHY IT DOES NOT SET core.hooksPath ANY MORE. That value REPLACES .git/hooks:
# git looks in one directory or the other, never both. github-guard installs
# into .git/hooks deliberately — outside the working tree, so no branch can
# rewrite them — and its own installer clears core.hooksPath for exactly this
# reason. Two installers competing for one pointer means the loser is whichever
# ran first, silently: run github-guard then this script and the guard that
# refuses a commit on main stops running, with nothing to show for it but
# `git config --get core.hooksPath`. Run them the other way round and these
# checks stop running instead. Issue #14.
#
# So there is one hooks directory and this script writes into it.
#
#   github-guard present   ->  <hooks>/pre-commit.d/20-go-fast-checks
#                              Its stub runs every executable in that
#                              directory in lexical order; dropping a file in
#                              is how you add to the chain, and both sets run.
#   no dispatcher          ->  <hooks>/pre-commit
#                              A plain clone still gets the checks.
#
# THE HOOKS DIRECTORY IS THE COMMON ONE. `git rev-parse --git-common-dir`
# names the main checkout's .git from inside a worktree as well as from the
# main tree, and hooks are shared by every worktree of a checkout. --git-dir
# would install into .git/worktrees/<name>/, where git never looks for a hook.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

common="$(git rev-parse --path-format=absolute --git-common-dir)"
hooks="$common/hooks"
mkdir -p "$hooks"

# NEVER LEFT SET, and cleared when something else set it, because a clone
# where it is set is a clone where half the hooks are not running and nothing
# says which half.
if existing="$(git config --get core.hooksPath 2>/dev/null)" && [ -n "$existing" ]; then
    git config --unset core.hooksPath
    echo "Cleared core.hooksPath ('$existing'): it replaces $hooks wholesale,"
    echo "so it would switch off every hook installed there — github-guard's included."
fi

chmod +x .githooks/* 2>/dev/null || true

# github-guard's stub names itself on its second line. Grepping the whole file
# is enough and does not care how the stub is spelled.
if [ -f "$hooks/pre-commit" ] && grep -q 'github-guard' "$hooks/pre-commit"; then
    mkdir -p "$hooks/pre-commit.d"
    target="$hooks/pre-commit.d/20-go-fast-checks"
    cp .githooks/pre-commit "$target"
    chmod +x "$target"
    echo "Installed $target — github-guard's dispatcher runs it."
    echo "pre-commit will run gofmt -s + go vet, alongside github-guard's own guards."
else
    target="$hooks/pre-commit"
    if [ -f "$target" ] && ! grep -q 'gofmt -l -s' "$target"; then
        # Somebody else's hook, and not one this script wrote. Overwriting it
        # is the mistake this whole file is about.
        echo "$target exists and is not this repository's hook — refusing to overwrite it." >&2
        echo "Move it aside, or add the body of .githooks/pre-commit to it by hand." >&2
        exit 1
    fi
    cp .githooks/pre-commit "$target"
    chmod +x "$target"
    echo "Installed $target. pre-commit will run gofmt -s + go vet."
fi

echo "Bypass a single commit with: git commit --no-verify"
