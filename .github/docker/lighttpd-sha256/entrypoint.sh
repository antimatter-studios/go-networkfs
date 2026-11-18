#!/bin/sh
# Writes the one account from WEBDAVD_USER and WEBDAVD_PASS as a line of
# lighttpd's plain password file -- user:password -- then runs lighttpd in the
# foreground.
set -eu
umask 077
printf '%s:%s\n' "$WEBDAVD_USER" "$WEBDAVD_PASS" > /var/lib/lighttpd-dav/passwords
exec lighttpd -D -f /etc/lighttpd/lighttpd.conf
