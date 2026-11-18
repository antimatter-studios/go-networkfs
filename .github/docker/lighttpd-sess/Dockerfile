# A WebDAV server behind HTTP Digest auth with algorithm=MD5-sess, for the
# webdav driver's Digest tests: lighttpd's mod_webdav and mod_auth.
#
# WHY LIGHTTPD. A Digest response is a hash the server recomputes, so the only
# proof that the driver computes it as RFC 7616 says is a server that shares
# none of our code checking it. Apache 2.4.71's mod_auth_digest refuses
# `AuthDigestAlgorithm MD5-sess` at startup ("Unsupported algorithm in
# AuthDigestAlgorithm: MD5-sess"), so the Apache fixture cannot say anything
# about MD5-sess (#100). lighttpd's mod_auth
# verifies MD5-sess as RFC 7616 3.4.2 defines it -- the session key is
# H(H(user:realm:password):nonce:cnonce) -- and checks the nc, cnonce, qop and
# uri a request carries.
#
# WHY A LUA SCRIPT. lighttpd checks an answer with whatever algorithm the
# answer names, MD5-sess included, but only ever offers plain MD5 in its
# challenge, and a client answers with the algorithm it is offered.
# md5-sess.lua, run by mod_magnet, renames the offered algorithm to MD5-sess
# and refuses an answer naming any other, so a client that ignored the offer
# and answered with plain MD5 cannot pass. It never judges an MD5-sess answer:
# that is lighttpd's own code.
#
# THIS IS A TEST SERVER: one account, plain HTTP, started by
# scripts/servers.sh in a throwaway container and never shipped. lighttpd is
# BSD-3-Clause. The base and the packages are pinned to the exact releases
# .github/docker/lighttpd uses, published for amd64 and arm64 alike (#16,
# #20). That server is the same lighttpd behind Basic auth, for the WebDAV
# tests at large; this one is for Digest MD5-sess alone, and keeps its own
# configuration so neither can change the other's. It listens on 8080 because
# it runs unprivileged.
FROM alpine:3.20.10

RUN apk add --no-cache lighttpd=1.4.85-r0 lighttpd-mod_auth=1.4.85-r0 lighttpd-mod_webdav=1.4.85-r0 \
 && mkdir -p /srv/dav /var/lib/lighttpd-dav \
 && chown lighttpd:lighttpd /srv/dav /var/lib/lighttpd-dav
COPY lighttpd.conf md5-sess.lua /etc/lighttpd/
COPY --chmod=0755 entrypoint.sh /usr/local/bin/lighttpd-entrypoint
USER lighttpd

# The account comes from the environment at start, under the names the other
# WebDAV fixtures read, so scripts/servers.sh passes the same two variables.
ENV WEBDAVD_USER=testuser
ENV WEBDAVD_PASS=testpass
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lighttpd-entrypoint"]
