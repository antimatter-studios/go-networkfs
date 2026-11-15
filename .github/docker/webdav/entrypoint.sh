#!/bin/sh
# Writes the one account from WEBDAVD_USER and WEBDAVD_PASS, then runs httpd in
# the foreground as the official image does.
set -eu
htpasswd -bcB /usr/local/apache2/var/htpasswd "$WEBDAVD_USER" "$WEBDAVD_PASS" 2>/dev/null
chown www-data:www-data /usr/local/apache2/var/htpasswd
exec httpd-foreground
