#!/bin/sh
# Writes the one account from WEBDAVD_USER and WEBDAVD_PASS as an htdigest
# line -- user:realm:MD5(user:realm:password) -- then runs lighttpd in the
# foreground. The realm is the one lighttpd.conf requires.
set -eu
realm=webdav-md5-sess
hash=$(printf '%s:%s:%s' "$WEBDAVD_USER" "$realm" "$WEBDAVD_PASS" | md5sum | cut -d' ' -f1)
printf '%s:%s:%s\n' "$WEBDAVD_USER" "$realm" "$hash" > /var/lib/lighttpd-dav/htdigest
exec lighttpd -D -f /etc/lighttpd/lighttpd.conf
