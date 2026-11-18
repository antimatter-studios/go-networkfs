#!/bin/sh
# Writes the one account from WEBDAVD_USER and WEBDAVD_PASS, for Basic and for
# Digest auth, then runs httpd in the foreground as the official image does.
set -eu
htpasswd -bcB /usr/local/apache2/var/htpasswd "$WEBDAVD_USER" "$WEBDAVD_PASS" 2>/dev/null
# htdigest only reads the password from a terminal, so the line it would write
# -- user:realm:MD5(user:realm:password) -- is written here. The realm is the
# AuthName of the port 81 server in dav.conf.
realm=webdav-digest
hash=$(printf '%s:%s:%s' "$WEBDAVD_USER" "$realm" "$WEBDAVD_PASS" | md5sum | cut -d' ' -f1)
printf '%s:%s:%s\n' "$WEBDAVD_USER" "$realm" "$hash" > /usr/local/apache2/var/htdigest
chown www-data:www-data /usr/local/apache2/var/htpasswd /usr/local/apache2/var/htdigest
exec httpd-foreground
