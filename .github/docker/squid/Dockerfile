# A forward HTTP proxy, for the webdav driver's Digest tests: Squid in front
# of the Digest servers the other fixtures run (#110).
#
# WHY A PROXY. Go sends a plain http:// request through a proxy with the
# absolute-form request-target -- "PROPFIND http://webdav:81/dir/ HTTP/1.1" --
# while the driver signs each Digest answer for the origin form, "/dir/". RFC
# 7616 3.4.6 says the uri parameter is the request-target, so whether a server
# accepts that answer behind a proxy is a question only a real proxy in front
# of a real server can answer. A proxy forwards the request to the origin
# server in origin form (RFC 9112 3.2.1), and that server is the one that
# checks the answer: lighttpd's mod_auth refuses an answer whose uri is the
# absolute form, and accepts the origin form, through this proxy.
#
# THIS IS A TEST SERVER, started by scripts/servers.sh in a throwaway
# container and never shipped. Squid is GPL-2.0-or-later: it runs here only as
# a container the tests talk to over HTTP, an independent oracle, never linked
# or copied into the drivers. The base and the package are pinned to the exact
# releases, published for amd64 and arm64 alike (#16, #20), on the alpine
# release the lighttpd fixtures use. It listens on 3128 because it runs
# unprivileged.
FROM alpine:3.20.10

RUN apk add --no-cache squid=6.9-r0
COPY squid.conf /etc/squid/squid.conf
USER squid

EXPOSE 3128
ENTRYPOINT ["squid", "-N", "-f", "/etc/squid/squid.conf"]
