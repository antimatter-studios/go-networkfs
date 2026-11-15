# The WebDAV server for the webdav driver's tests: Apache httpd's mod_dav.
#
# WHY APACHE. The driver's conditional mutations hand a revision (an entity
# tag) back in an RFC 4918 If header and rely on the server to refuse a stale
# one with 412 while it applies the change (issue #70). The server this
# replaced, test/webdavd over golang.org/x/net/webdav, cannot evaluate an
# entity tag in an If header at all -- it answers 412 to every such request --
# so it could prove a conflict but never a valid conditional save. mod_dav is
# the reference implementation of RFC 4918 and shares no code or reading of
# the protocol with this repository, which is what "validate against something
# that is not us" asks of a fixture.
#
# STILL BUILT HERE, NOT PULLED AS-IS, for the reasons the earlier image was
# (issues #16 and #20): the base is pinned to an exact release, httpd's
# official image is published for amd64 and arm64 alike, and the DAV and
# authentication configuration lives in this repository next to the tests that
# depend on it.
#
# THIS IS A TEST SERVER: one account, plain HTTP, started by
# scripts/servers.sh in a throwaway container and never shipped.
FROM httpd:2.4.71

COPY .github/docker/webdav/dav.conf /usr/local/apache2/conf/extra/dav.conf
COPY .github/docker/webdav/entrypoint.sh /usr/local/bin/webdav-entrypoint
RUN echo 'Include conf/extra/dav.conf' >> /usr/local/apache2/conf/httpd.conf \
 && rm -f /usr/local/apache2/htdocs/index.html \
 && mkdir -p /usr/local/apache2/var /usr/local/apache2/digest \
 && chown -R www-data:www-data /usr/local/apache2/var /usr/local/apache2/htdocs /usr/local/apache2/digest \
 && chmod 0755 /usr/local/bin/webdav-entrypoint

# The account comes from the environment at start, as it did for webdavd, so
# scripts/servers.sh passes the same two variables to either.
ENV WEBDAVD_USER=testuser
ENV WEBDAVD_PASS=testpass
EXPOSE 80 81
ENTRYPOINT ["/usr/local/bin/webdav-entrypoint"]
