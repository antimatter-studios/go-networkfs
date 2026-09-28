#!/usr/bin/env bash
#
# ftp-passive-range.sh — FTP's passive ports must sit below the ephemeral
# range, and the server must be told the same range that is published.
#
# Linux hands out ephemeral SOURCE ports from net.ipv4.ip_local_port_range,
# which is 32768-60999 on the GitHub runners and on most distributions. A
# passive range inside it can be held by any outbound connection the machine
# happens to be making — a registry pull, an apt fetch, one of the other five
# `docker run`s — at the moment ftp comes up, and then the bind fails:
#
#   failed to bind host port for 0.0.0.0:40008: address already in use
#
# `servers.sh up` is a chain, so that aborts the whole job before a single
# test runs, on a pull request whose diff does not touch FTP. Issue #15.
#
# THE SECOND CHECK IS THE ONE THAT IS EASY TO GET WRONG. vsftpd's passive
# range lives in the IMAGE (/etc/vsftpd.conf: pasv_min_port=40000), not in an
# environment variable, so moving FTP_PASV_LO alone would publish one range
# and leave the server advertising another. A passive transfer would then be
# told to connect to a port nothing forwards, and the failure would arrive as
# an FTP driver hang rather than as a configuration mistake.
#
# Quiet on success: this tier's output is budgeted, so a check that passes
# prints nothing and only the last line says the file finished.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="$ROOT/scripts/test-env.sh"
SERVERS="$ROOT/scripts/servers.sh"

EXPECTED_CHECKS=10
checks=0
fails=0

ok()   { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"
         [ $# -gt 1 ] && printf '        %s\n' "$2"; }

# The defaults as test-env.sh states them, read WITHOUT the environment
# getting a vote: a value that is only correct because this shell exported it
# is not the value a CI runner will use.
# shellcheck source=../test-env.sh
lo="$(env -i bash -c ". '$ENV_FILE'; printf '%s' \"\$FTP_PASV_LO\"")"
hi="$(env -i bash -c ". '$ENV_FILE'; printf '%s' \"\$FTP_PASV_HI\"")"

case "$lo" in ''|*[!0-9]*) fail "FTP_PASV_LO is a number" "got '$lo'" ;; *) ok ;; esac
case "$hi" in ''|*[!0-9]*) fail "FTP_PASV_HI is a number" "got '$hi'" ;; *) ok ;; esac
[ -n "$lo" ] || lo=0
[ -n "$hi" ] || hi=0

if [ "$lo" -le "$hi" ]; then ok; else fail "FTP_PASV_LO is not above FTP_PASV_HI" "$lo-$hi"; fi

# THE FLOOR IS 32768 EVERYWHERE THIS RUNS, and it is checked against the
# hardcoded number rather than only against this machine's: a range that is
# below the local floor and above the runner's would pass here and fail there.
FLOOR=32768
if [ "$hi" -lt "$FLOOR" ]; then ok
else fail "the passive range is below the ephemeral floor of $FLOOR" \
          "$lo-$hi overlaps the ports the kernel hands out as source ports"; fi

# And against the actual range where the kernel exposes it, so a machine
# configured lower than the default is caught too.
if [ -r /proc/sys/net/ipv4/ip_local_port_range ]; then
    local_floor="$(awk '{print $1}' /proc/sys/net/ipv4/ip_local_port_range)"
    if [ "$hi" -lt "$local_floor" ]; then ok
    else fail "the passive range is below THIS host's ephemeral floor" \
              "$lo-$hi against ip_local_port_range starting at $local_floor"; fi
else
    # Not a skip: on a host with no /proc the documented floor is the only
    # evidence available, and the check above already made it.
    ok
fi

# --- The server is told the range that is published. -----------------------
publish="$(grep -c 'FTP_PASV_LO-\$FTP_PASV_HI:\$FTP_PASV_LO-\$FTP_PASV_HI' "$SERVERS")"
if [ "$publish" -ge 1 ]; then ok
else fail "servers.sh publishes the passive range on the same host ports"; fi

for setting in pasv_min_port pasv_max_port; do
    if grep -q -- "-o$setting=" "$SERVERS"; then ok
    else fail "servers.sh overrides the image's $setting" \
              "vsftpd's range is baked into /etc/vsftpd.conf; publishing alone moves nothing"; fi
done

# AND THE CONFIG FILE MUST BE NAMED BEFORE THEM. vsftpd processes arguments
# left to right and reads /etc/vsftpd.conf implicitly only when no config file
# was given — after the -o options, which the file then overrides. Measured:
# with the flags alone the server still answered PASV with 40009.
if grep -q -- '/usr/sbin/vsftpd /etc/vsftpd.conf' "$SERVERS"; then ok
else fail "servers.sh names /etc/vsftpd.conf before the -o overrides" \
          "implicit config is read last and wins over -o"; fi

# The override must name the variables, not a second copy of the numbers.
if grep -q -- '-opasv_min_port="\?\$FTP_PASV_LO' "$SERVERS" \
   && grep -q -- '-opasv_max_port="\?\$FTP_PASV_HI' "$SERVERS"; then ok
else fail "the vsftpd override reads FTP_PASV_LO/HI rather than repeating them" \
          "$(grep -n 'pasv_m' "$SERVERS" | tr '\n' '|')"; fi

if [ "$checks" -ne "$EXPECTED_CHECKS" ]; then
    printf '  FAIL  %s checks ran, %s expected — this file did not finish.\n' \
        "$checks" "$EXPECTED_CHECKS"
    exit 1
fi
[ "$fails" -eq 0 ] || { printf '  %s checks, %s failed\n' "$checks" "$fails"; exit 1; }
printf 'ftp-passive-range: all checks passed\n'
