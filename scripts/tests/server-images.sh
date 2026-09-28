#!/usr/bin/env bash
#
# server-images.sh — every test server image is pinned, and every one built
# here has a Dockerfile to build it from.
#
# TWO THINGS THIS FILE IS ABOUT, and they are the same mistake twice.
#
# `:latest` IS NOT A VERSION. What the suite runs against can then change
# between two runs of the same commit, and a green run is not evidence about
# the commit at all. Issue #7 was exactly this: an unpinned image that stopped
# existing. sss3 came out of that pinned; three others did not.
#
# AN IMAGE HAS TO EXIST FOR THE HOST. atmoz/sftp and bytemark/webdav publish
# linux/amd64 manifests alone, so on arm64 both exited with "exec format
# error" and `servers.sh up` stopped at the first of them — and the consumer
# of these archives is a macOS application, so an Apple Silicon workstation is
# exactly where a developer runs this. Issues #16 and #20. Both are now built
# from source here, the way samba and mockapi already were, which is multi-arch
# by construction.
#
# WHY THE ARCHITECTURE ITSELF IS NOT CHECKED HERE. Asking the registry needs
# Docker and a network, and `chore test:scripts` runs on the macOS job where
# there is no daemon. What this file can check without either is that nothing
# names the two images that had the problem and that nothing floats — and a
# pull that does turn out to be wrong for the host now stops at servers.sh's
# liveness check, naming the container and dumping its log, rather than ninety
# seconds later inside a C harness (#13, scripts/tests/servers-liveness.sh).
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="$ROOT/scripts/test-env.sh"

EXPECTED_CHECKS=18
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# The five image references as test-env.sh resolves them, with nothing from
# the caller's environment getting a vote.
image_of() { # $1 = variable name
    env -i bash -c ". '$ENV_FILE'; printf '%s' \"\$$1\""
}

# --- Nothing floats. -------------------------------------------------------
for var in SMB_IMAGE S3_IMAGE FTP_IMAGE SFTP_IMAGE DAV_IMAGE MOCK_IMAGE; do
    ref="$(image_of "$var")"
    case "$ref" in
        '')       fail "$var names an image" ;;
        *:latest) fail "$var is pinned, not :latest" \
                       "'$ref' — what the suite runs against can change between two runs of the same commit" ;;
        *:*)      ok ;;
        *)        fail "$var carries a tag" "'$ref' has none, so it means :latest" ;;
    esac
done

# --- The two amd64-only images are gone, by name. --------------------------
#
# By name rather than by architecture because the name is what a future edit
# would reach for: "put the old one back, it was simpler" is the regression
# this catches, and it needs no daemon to catch it.
# COMMENTS ARE EXEMPT, and deliberately: the Dockerfiles and test-env.sh say
# which image each replaced and why, which is the whole record of the decision.
# What must not come back is a line that RUNS one.
for image in atmoz/sftp bytemark/webdav; do
    found="$(grep -rn "$image" "$ROOT/scripts" "$ROOT/.github" "$ROOT/chores.yml" 2>/dev/null \
             | grep -v 'scripts/tests/server-images.sh' \
             | grep -vE ':[0-9]+:[[:space:]]*#' || true)"
    if [ -z "$found" ]; then ok
    else fail "nothing runs $image, which publishes no arm64 manifest" "$found"; fi
done

# --- Every image built here has something to build it from. ----------------
for var in SMB_IMAGE SFTP_IMAGE DAV_IMAGE MOCK_IMAGE; do
    ref="$(image_of "$var")"
    case "$ref" in
        go-networkfs-*) ok ;;
        *) fail "$var is one of the images this repository builds" "'$ref' is pulled" ;;
    esac
done

for dir in samba sftp webdav mockapi; do
    if [ -f "$ROOT/.github/docker/$dir/Dockerfile" ]; then ok
    else fail ".github/docker/$dir/Dockerfile exists to build that server from"; fi
done

# The two new ones are built, not pulled: build_image, not ensure_image.
for server in sftp webdav; do
    body="$(awk "/^up_$server\\(\\) \\{/,/^\\}/" "$ROOT/scripts/servers.sh")"
    case "$body" in
        *build_image*) ok ;;
        *) fail "servers.sh builds the $server image rather than pulling it" \
                "$(printf '%s' "$body" | tr '\n' '|')" ;;
    esac
done

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'server-images: all checks passed\n'
