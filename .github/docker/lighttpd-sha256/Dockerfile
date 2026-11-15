# A WebDAV server behind HTTP Digest auth with algorithm=SHA-256, for the
# webdav driver's Digest tests: lighttpd's mod_webdav and mod_auth (#108).
#
# WHY LIGHTTPD. A Digest response is a hash the server recomputes, so the only
# proof that the driver computes it as RFC 7616 says is a server that shares
# none of our code checking it. Apache 2.4's mod_auth_digest offers MD5 alone.
# lighttpd's mod_auth offers and checks SHA-256 when it is built with a crypto
# library, as Alpine builds it (OpenSSL), and checks the uri, nc, cnonce and
# qop a request carries. It cannot offer SHA-512-256 in this build ("invalid
# algorithm" at startup), so that one is checked by unit tests against values
# computed outside Go (webdav/digest_algorithm_test.go).
#
# TWO LISTENERS, one account:
#   8080  offers SHA-256 alone.
#   8081  offers SHA-256 and MD5, as two WWW-Authenticate headers in that
#         order (RFC 7616 3.7). A client that takes only the first header it
#         can parse, or ignores the order, answers with the wrong one.
#
# The password file is lighttpd's "plain" backend, so lighttpd computes H(A1)
# for whichever algorithm an answer names, from the password itself.
#
# THIS IS A TEST SERVER: one account, plain HTTP, started by
# scripts/servers.sh in a throwaway container and never shipped. lighttpd is
# BSD-3-Clause. The base and the packages are pinned to the exact releases
# .github/docker/lighttpd and .github/docker/lighttpd-sess use, published for
# amd64 and arm64 alike (#16, #20). This one keeps its own configuration so
# neither of those can change it. It listens on 8080 and 8081 because it runs
# unprivileged.
FROM alpine:3.20.10

RUN apk add --no-cache lighttpd=1.4.85-r0 lighttpd-mod_auth=1.4.85-r0 lighttpd-mod_webdav=1.4.85-r0 \
 && mkdir -p /srv/dav /var/lib/lighttpd-dav \
 && chown lighttpd:lighttpd /srv/dav /var/lib/lighttpd-dav
COPY lighttpd.conf /etc/lighttpd/
COPY --chmod=0755 entrypoint.sh /usr/local/bin/lighttpd-entrypoint
USER lighttpd

# The account comes from the environment at start, under the names the other
# WebDAV fixtures read, so scripts/servers.sh passes the same two variables.
ENV WEBDAVD_USER=testuser
ENV WEBDAVD_PASS=testpass
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/lighttpd-entrypoint"]
