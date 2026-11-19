#!/usr/bin/env bash
#
# test-env.sh — every address, port, credential and image the test rig uses,
# in ONE place. Sourced, never executed.
#
# WHY IT IS ONE FILE. These values were the Makefile's variable block, and they
# are read by four things that must agree: the script that starts the servers,
# the script that runs the suite against them, the script that builds and runs
# the C harnesses, and the runner container's environment. Four copies of
# "the SMB port is 4445" is three chances to be wrong, and the failure is a
# connection refused in a driver test — which reads as a driver bug.
#
# EVERY VALUE TAKES THE ENVIRONMENT FIRST. On a developer machine the servers
# are published on localhost, which is what these defaults say. Inside the
# containerised runner they are reached by container NAME on the shared Docker
# network and on the INTERNAL port, so scripts/docker-suite.sh passes
# SMB_ADDR=samba SMB_PORT=445 and so on, and nothing here has to know.
#
# THE NAMES ARE THE MAKEFILE'S, unchanged. They are what .github/workflows,
# the runner container and anyone's shell history already say.

# WHAT THE CALLER EXPORTED, taken before anything below is defaulted. The
# per-run isolation at the end of this file must leave alone every value the
# caller chose, and a value an earlier script already exported (export_suite_env
# exports ports) is one of them: it has been isolated once and must not be again.
_CALLER_ENV=" $(compgen -e | tr '\n' ' ') "

# Where the servers are reachable.
SMB_ADDR="${SMB_ADDR:-127.0.0.1}"
S3_ADDR="${S3_ADDR:-127.0.0.1}"
FTP_ADDR="${FTP_ADDR:-127.0.0.1}"
SFTP_ADDR="${SFTP_ADDR:-127.0.0.1}"
DAV_ADDR="${DAV_ADDR:-127.0.0.1}"
DAV_SABRE_ADDR="${DAV_SABRE_ADDR:-127.0.0.1}"
DAV_LIGHTTPD_ADDR="${DAV_LIGHTTPD_ADDR:-127.0.0.1}"
DAV_LIGHTTPD_SESS_ADDR="${DAV_LIGHTTPD_SESS_ADDR:-127.0.0.1}"
DAV_LIGHTTPD_SHA256_ADDR="${DAV_LIGHTTPD_SHA256_ADDR:-127.0.0.1}"
PROXY_ADDR="${PROXY_ADDR:-127.0.0.1}"
MOCK_ADDR="${MOCK_ADDR:-127.0.0.1}"

# The servers join this network so the runner container can reach them by name.
TEST_NETWORK="${TEST_NETWORK:-go-networkfs-test}"
RUNNER_IMAGE="${RUNNER_IMAGE:-go-networkfs-test:latest}"

# The SMB driver cannot be tested without a real server: there is no in-process
# Go SMB server to stand in for one.
SMB_PORT="${SMB_PORT:-4445}"
SMB_IMAGE="${SMB_IMAGE:-go-networkfs-samba:test}"
SMB_CONTAINER="${SMB_CONTAINER:-go-networkfs-samba}"
SMB_SHARE="${SMB_SHARE:-tmp}"
SMB_USER="${SMB_USER:-smbuser}"
SMB_PASS="${SMB_PASS:-Smbpasswd12345}"

# stupid-simple-s3 speaks the S3 protocol, so the S3 driver is tested against
# the real thing rather than a hand-written stub of it. It ships both as an
# image and as a single static binary, so unlike Samba it does not need a
# container: `chore test:s3` runs the binary (scripts/sss3-server.sh) and needs
# no Docker at all. The container is for `chore servers:up` and `chore test`,
# where every server has to sit on one network for the runner to reach by name.
#
# S3_PORT is the PUBLISHED port on the host; the server listens on 5553 inside
# the container, which is what the runner reaches it on.
S3_PORT="${S3_PORT:-9000}"
S3_VERSION="${S3_VERSION:-1.0.7}"
S3_IMAGE="${S3_IMAGE:-ghcr.io/espebra/stupid-simple-s3:$S3_VERSION}"
S3_CONTAINER="${S3_CONTAINER:-go-networkfs-sss3}"
S3_BUCKET="${S3_BUCKET:-testbucket}"
S3_KEY="${S3_KEY:-sss3admin}"
S3_SECRET="${S3_SECRET:-sss3admin123}"
# The image is distroless and runs as `nonroot`, which owns exactly one
# directory: the one its own Dockerfile prepares and chowns. Pointing the
# server anywhere else — /data, say — leaves it unable to create the storage
# directory, and it exits 1 on the spot.
S3_DATA="${S3_DATA:-/var/lib/stupid-simple-s3/data}"
S3_TMP="${S3_TMP:-/var/lib/stupid-simple-s3/tmp}"

FTP_PORT="${FTP_PORT:-2121}"
# BELOW 32768 ON PURPOSE, and the next person reaching for a nice round 40000
# should read this first. Linux hands out ephemeral SOURCE ports from
# net.ipv4.ip_local_port_range, which is 32768-60999 on the GitHub runners and
# on most distributions. A passive range inside it can already be held by any
# outbound connection the machine is making when ftp comes up — a registry
# pull, an apt fetch, one of the other five `docker run`s — and the bind then
# fails with "address already in use", aborting the whole servers chain on a
# pull request whose diff does not touch FTP. Seen once, on exactly that kind
# of pull request; issue #15. Anything under 32768 is never handed out as a
# source port, so the collision cannot happen.
#
# vsftpd's own range is baked into the image's /etc/vsftpd.conf, so moving
# these two moves the PUBLISH alone unless the server is told as well —
# scripts/servers.sh passes them as -opasv_min_port/-opasv_max_port.
FTP_PASV_LO="${FTP_PASV_LO:-30000}"
FTP_PASV_HI="${FTP_PASV_HI:-30009}"
# PINNED, because :latest is not a version. Issue #7 was an unpinned image
# that stopped existing; this one is multi-arch (386, amd64, arm/v6, arm/v7,
# arm64, ppc64le, s390x — `docker manifest inspect`, 2026-09-28), so unlike
# the WebDAV and SFTP servers it did not also have to be replaced.
FTP_VERSION="${FTP_VERSION:-0.9.2}"
FTP_IMAGE="${FTP_IMAGE:-garethflowers/ftp-server:$FTP_VERSION}"
FTP_CONTAINER="${FTP_CONTAINER:-go-networkfs-ftp}"
FTP_USER="${FTP_USER:-testuser}"
FTP_PASS="${FTP_PASS:-Ftppasswd12345}"

# BUILT FROM .github/docker/sftp, NOT PULLED. atmoz/sftp:latest is published
# for linux/amd64 alone: on an arm64 host it exited with "exec format error"
# before answering on port 22, and `servers.sh up` stopped there, so the one
# task that exercises every driver's tagged integration tests and the C ABI
# did not run at all on the architecture a macOS developer has. Issues #16 and
# #20. Built here it is multi-arch by construction and pinned by this
# repository's own commit.
SFTP_PORT="${SFTP_PORT:-2222}"
SFTP_IMAGE="${SFTP_IMAGE:-go-networkfs-sftp:test}"
SFTP_CONTAINER="${SFTP_CONTAINER:-go-networkfs-sftp}"
SFTP_USER="${SFTP_USER:-testuser}"
SFTP_PASS="${SFTP_PASS:-testpass}"

# BUILT FROM .github/docker/webdav, NOT PULLED, for the same reason as sftp:
# bytemark/webdav:latest is linux/amd64 only. The server is Apache httpd's
# mod_dav on a pinned, multi-arch httpd base: the reference implementation of
# RFC 4918, and one that enforces entity tags in an If header, which the
# driver's conditional mutations rely on (#70). test/webdavd, over
# golang.org/x/net/webdav, served here before it and could not.
DAV_PORT="${DAV_PORT:-8080}"
# The same server's second listener, behind HTTP Digest auth rather than Basic
# (#95). A port of its own, not a path under the first: the driver checks a
# mount by asking the server's root, and one root cannot offer both schemes.
DAV_DIGEST_PORT="${DAV_DIGEST_PORT:-8083}"
DAV_IMAGE="${DAV_IMAGE:-go-networkfs-webdav:test}"
DAV_CONTAINER="${DAV_CONTAINER:-go-networkfs-webdav}"
DAV_USER="${DAV_USER:-testuser}"
DAV_PASS="${DAV_PASS:-testpass}"

# A SECOND WebDAV server, sabre/dav, BUILT FROM .github/docker/sabredav, with
# the same account. One server is one reading of RFC 4918: Apache answers a
# MOVE into a missing parent 500 where the RFC asks for 409, so the driver's
# 409 path had no independent oracle until this one, which answers 409 (#91).
# The webdav integration tests run against both. It listens on 8080 inside the
# container because it runs unprivileged.
DAV_SABRE_PORT="${DAV_SABRE_PORT:-8082}"
DAV_SABRE_IMAGE="${DAV_SABRE_IMAGE:-go-networkfs-sabredav:test}"
DAV_SABRE_CONTAINER="${DAV_SABRE_CONTAINER:-go-networkfs-sabredav}"

# A THIRD WebDAV server, lighttpd's mod_webdav, BUILT FROM .github/docker/lighttpd,
# with the same account. sabre/dav reports no entity tag for a collection, so
# conditional operations on a directory had Apache as their one oracle (#102);
# lighttpd reports one. The webdav integration tests run against all three.
DAV_LIGHTTPD_PORT="${DAV_LIGHTTPD_PORT:-8084}"
DAV_LIGHTTPD_IMAGE="${DAV_LIGHTTPD_IMAGE:-go-networkfs-lighttpd:test}"
DAV_LIGHTTPD_CONTAINER="${DAV_LIGHTTPD_CONTAINER:-go-networkfs-lighttpd}"

# lighttpd AGAIN, BUILT FROM .github/docker/lighttpd-sess, with the same
# account behind HTTP Digest auth that offers algorithm=MD5-sess. Apache
# refuses to offer MD5-sess at all, so the driver's MD5-sess answers had no
# independent server to check them until this one (#100). The Digest tests
# alone use it. It listens on 8080 inside the container because it runs
# unprivileged.
DAV_LIGHTTPD_SESS_PORT="${DAV_LIGHTTPD_SESS_PORT:-8085}"
DAV_LIGHTTPD_SESS_IMAGE="${DAV_LIGHTTPD_SESS_IMAGE:-go-networkfs-lighttpd-sess:test}"
DAV_LIGHTTPD_SESS_CONTAINER="${DAV_LIGHTTPD_SESS_CONTAINER:-go-networkfs-lighttpd-sess}"

# lighttpd ONCE MORE, BUILT FROM .github/docker/lighttpd-sha256, with the same
# account behind HTTP Digest auth that offers algorithm=SHA-256: on one
# listener alone, on the other before MD5, as two challenges (#108). Apache
# offers MD5 alone. The Digest tests alone use it. It listens on 8080 and 8081
# inside the container because it runs unprivileged.
DAV_LIGHTTPD_SHA256_PORT="${DAV_LIGHTTPD_SHA256_PORT:-8086}"
DAV_LIGHTTPD_SHA256_MD5_PORT="${DAV_LIGHTTPD_SHA256_MD5_PORT:-8087}"
DAV_LIGHTTPD_SHA256_IMAGE="${DAV_LIGHTTPD_SHA256_IMAGE:-go-networkfs-lighttpd-sha256:test}"
DAV_LIGHTTPD_SHA256_CONTAINER="${DAV_LIGHTTPD_SHA256_CONTAINER:-go-networkfs-lighttpd-sha256}"

# A FORWARD PROXY, Squid, BUILT FROM .github/docker/squid. Go sends a plain
# http:// request through a proxy with the absolute-form request-target, and
# the driver signs each Digest answer for the origin form, so the Digest tests
# go through a real proxy to the webdav and lighttpd-sess servers to show the
# answer is still accepted (#110). Those servers are reached through it BY
# THEIR NAMES on the test network and on their internal ports, which is the
# same address whether the tests run on the host or in the runner: from the
# host, only the proxy can reach them there, so nothing goes around it. It
# listens on 3128 inside the container because it runs unprivileged.
PROXY_PORT="${PROXY_PORT:-8088}"
PROXY_IMAGE="${PROXY_IMAGE:-go-networkfs-squid:test}"
PROXY_CONTAINER="${PROXY_CONTAINER:-go-networkfs-squid}"

# Dropbox, Google Drive and OneDrive cannot be run locally the way Samba can.
# One mock API stands in for all three, built from test/mockapi in this repo.
MOCK_PORT="${MOCK_PORT:-8081}"
MOCK_IMAGE="${MOCK_IMAGE:-go-networkfs-mockapi:test}"
MOCK_CONTAINER="${MOCK_CONTAINER:-go-networkfs-mockapi}"
# It compiles in the runner image's base, so the large pull is paid once, and
# against the Go build cache the runner uses, which CI restores between runs.
MOCK_BUILD_IMAGE="${MOCK_BUILD_IMAGE:-golang:1.26-bookworm}"
GO_BUILD_CACHE="${GO_BUILD_CACHE:-go-networkfs-gobuild}"

# WHAT A TAGGED GO TEST AND THE C HARNESSES READ, exported under the names the
# tests use rather than the ones this file uses. One function, called by
# scripts/with-servers.sh AND scripts/suite.sh, because it used to be three
# lines in with-servers.sh alone: `chore test:smb` and `chore test:s3` had the
# environment, and the containerised run — which reaches suite.sh through
# `chore test:ci`, never through with-servers.sh — did not, so every tagged S3
# and SMB test in CI skipped itself and the job went green. Issue #33.
#
# SUITE_ENV_VARS is the list, so `servers.sh env` prints exactly this and a
# guard (scripts/tests/suite-env.sh) can hold the two to each other.
# shellcheck disable=SC2034  # read by servers.sh, which sources this file
SUITE_ENV_VARS="SMB_HOST SMB_PORT SMB_SHARE SMB_USER SMB_PASS S3_ENDPOINT S3_BUCKET S3_SECURE S3_ACCESS_KEY S3_SECRET_KEY FTP_HOST FTP_PORT FTP_USER FTP_PASS WEBDAV_URL WEBDAV_DIGEST_URL WEBDAV_SABRE_URL WEBDAV_LIGHTTPD_URL WEBDAV_LIGHTTPD_SESS_URL WEBDAV_LIGHTTPD_SHA256_URL WEBDAV_LIGHTTPD_SHA256_MD5_URL WEBDAV_PROXY_URL WEBDAV_PROXIED_DIGEST_URL WEBDAV_PROXIED_LIGHTTPD_SESS_URL WEBDAV_USER WEBDAV_PASS"
export_suite_env() {
    export SMB_HOST="$SMB_ADDR" SMB_PORT SMB_SHARE SMB_USER SMB_PASS
    export S3_ENDPOINT="$S3_ADDR:$S3_PORT" S3_BUCKET S3_SECURE=false
    export S3_ACCESS_KEY="$S3_KEY" S3_SECRET_KEY="$S3_SECRET"
    export FTP_HOST="$FTP_ADDR" FTP_PORT FTP_USER FTP_PASS
    export WEBDAV_URL="http://$DAV_ADDR:$DAV_PORT" WEBDAV_USER="$DAV_USER" WEBDAV_PASS="$DAV_PASS"
    export WEBDAV_DIGEST_URL="http://$DAV_ADDR:$DAV_DIGEST_PORT"
    export WEBDAV_SABRE_URL="http://$DAV_SABRE_ADDR:$DAV_SABRE_PORT"
    export WEBDAV_LIGHTTPD_URL="http://$DAV_LIGHTTPD_ADDR:$DAV_LIGHTTPD_PORT"
    export WEBDAV_LIGHTTPD_SESS_URL="http://$DAV_LIGHTTPD_SESS_ADDR:$DAV_LIGHTTPD_SESS_PORT"
    export WEBDAV_LIGHTTPD_SHA256_URL="http://$DAV_LIGHTTPD_SHA256_ADDR:$DAV_LIGHTTPD_SHA256_PORT"
    export WEBDAV_LIGHTTPD_SHA256_MD5_URL="http://$DAV_LIGHTTPD_SHA256_ADDR:$DAV_LIGHTTPD_SHA256_MD5_PORT"
    export WEBDAV_PROXY_URL="http://$PROXY_ADDR:$PROXY_PORT"
    # The network aliases and internal ports servers.sh gives those two.
    export WEBDAV_PROXIED_DIGEST_URL="http://webdav:81"
    export WEBDAV_PROXIED_LIGHTTPD_SESS_URL="http://lighttpd-sess:8080"
}

# Every driver whose integration tests are behind a build tag and need a
# server. Without these tags most of each driver is never executed and the
# default coverage number reports it as untested.
INTEGRATION_TAGS="${INTEGRATION_TAGS:-smb_integration,s3_integration,ftp_integration,webdav_integration}"

# The drivers that build as a c-archive. THIS LIST AND chores.yml's DRIVERS
# MUST AGREE — scripts/cabi.sh builds one harness per name here, and the
# archives task checks that every name in its own list produced an archive.
DRIVERS="${DRIVERS:-ftp sftp smb dropbox webdav gdrive s3 onedrive}"

COVERAGE="${COVERAGE:-coverage.out}"

# The C ABI harness. It links the shipped archive and calls the exported
# functions the way a consumer does, which is the only way to reach the three
# entry points taking a ByteSlice or a size_t: a Go test file may not import
# "C", so it cannot name either type.
CABI_DIR="${CABI_DIR:-build/cabi}"
CABI_COVER="${CABI_COVER:-build/cabi-cover}"
CC="${CC:-cc}"
if [ -z "${CABI_LDLIBS:-}" ]; then
    case "$(uname -s)" in
        Darwin) CABI_LDLIBS="-framework CoreFoundation -framework Security" ;;
        *)      CABI_LDLIBS="-lpthread -ldl -lresolv" ;;
    esac
fi

# PER-RUN ISOLATION, for a host that runs more than one of these at once.
#
# The vps runners are shared: two integration runs, or one of ours and another
# repository's, can be on the same Docker daemon together. Every name here is
# fixed, and `servers.sh up` removes a container by name before it starts one,
# so the second run would delete the first run's servers, and a published port
# the first holds fails the second's `docker run`. A run that has an id
# therefore gets a name and a port range of its own:
#
#   RUN_TAG   the run's id. CI's GITHUB_RUN_ID and attempt by default, so two
#             runs never share one and a re-run is not the run it replaces; set
#             it by hand to run two copies on one machine.
#   containers and the network get "-$RUN_TAG" appended, and the runner image
#             is tagged with it instead of :latest.
#   ports     every *_PORT and the FTP passive range move up by one offset,
#             PORT_OFFSET, 0 to 780 in steps of 20 chosen from RUN_TAG. Twenty
#             keeps one service's moved port off another's (the defaults differ
#             by 1, 101 and 1000 or more, none a multiple of 20), and 780 keeps
#             the passive range below 32768, which is where Linux hands out
#             source ports (#15). Two runs can still draw the same offset, one
#             in forty; servers.sh then fails on the bound port instead of
#             deleting anything.
#
# Server images keep their tags: two builds of one Dockerfile are the same
# image. Nothing changes when RUN_TAG is empty, which is every local run, and
# any variable the caller set is left as the caller set it.
RUN_TAG="${RUN_TAG:-${GITHUB_RUN_ID:+$GITHUB_RUN_ID-${GITHUB_RUN_ATTEMPT:-1}}}"
if [ -n "$RUN_TAG" ]; then
    PORT_OFFSET=$(( $(printf %s "$RUN_TAG" | cksum | cut -d' ' -f1) % 40 * 20 ))
    for _v in $(compgen -v); do
        case "$_CALLER_ENV" in *" $_v "*) continue ;; esac
        case "$_v" in
            *_PORT|FTP_PASV_LO|FTP_PASV_HI) printf -v "$_v" %s "$(( ${!_v} + PORT_OFFSET ))" ;;
            *_CONTAINER|TEST_NETWORK)       printf -v "$_v" %s "${!_v}-$RUN_TAG" ;;
            RUNNER_IMAGE)                   printf -v "$_v" %s "${!_v%:*}:$RUN_TAG" ;;
        esac
    done
    unset _v
fi
