# The third WebDAV server for the webdav driver's tests: lighttpd's mod_webdav.
#
# WHY A THIRD SERVER. Apache httpd's mod_dav (.github/docker/webdav) reports an
# entity tag for a collection and evaluates one in an If header; sabre/dav
# (.github/docker/sabredav) reports none for a collection. So the driver's
# conditional operations on a directory had Apache as their only oracle
# (#102). lighttpd reports collection entity tags too, and shares no code with
# either server or with this repository.
#
# It also reads RFC 4918 differently on exactly that path, which is the point
# of another reading: it ignores an entity tag in an If header when the
# resource is a collection, and goes ahead with a DELETE or MOVE whose tag does
# not match, while it honours If-Match on the same request.
#
# BSD-3-Clause. The base image and the packages are pinned to exact releases,
# published for amd64 and arm64 alike (#16, #20).
#
# THIS IS A TEST SERVER: one account, plain HTTP, started by
# scripts/servers.sh in a throwaway container and never shipped.
FROM alpine:3.20.10

RUN apk add --no-cache lighttpd=1.4.85-r0 lighttpd-mod_auth=1.4.85-r0 lighttpd-mod_webdav=1.4.85-r0 \
 && mkdir -p /srv/dav /var/lib/lighttpd-dav \
 && chown lighttpd:lighttpd /srv/dav /var/lib/lighttpd-dav

COPY .github/docker/lighttpd/lighttpd.conf /etc/lighttpd/dav.conf
COPY .github/docker/lighttpd/entrypoint.sh /usr/local/bin/lighttpd-dav-entrypoint
RUN chmod 0755 /usr/local/bin/lighttpd-dav-entrypoint

# The account comes from the environment at start, under the names the other
# WebDAV fixtures read, so scripts/servers.sh passes the same two variables.
ENV WEBDAVD_USER=testuser
ENV WEBDAVD_PASS=testpass
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/lighttpd-dav-entrypoint"]
