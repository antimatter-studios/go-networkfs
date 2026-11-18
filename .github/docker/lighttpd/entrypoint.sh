#!/bin/sh
# Writes the one account from WEBDAVD_USER and WEBDAVD_PASS, then runs lighttpd
# in the foreground.
set -eu
printf '%s:%s\n' "$WEBDAVD_USER" "$WEBDAVD_PASS" > /var/lib/lighttpd-dav/users
chown lighttpd:lighttpd /var/lib/lighttpd-dav/users
chmod 0600 /var/lib/lighttpd-dav/users
exec lighttpd -D -f /etc/lighttpd/dav.conf
