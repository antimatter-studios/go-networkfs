# The second WebDAV server for the webdav driver's tests: sabre/dav.
#
# WHY A SECOND SERVER. Apache httpd's mod_dav (.github/docker/webdav) is the
# first, and the only one the conditional mutations were checked against. One
# oracle is one reading of RFC 4918, and where it departs from the RFC the
# driver's handling of the RFC's answer goes untested. Apache 2.4.71 answers a
# MOVE into a missing parent 500 where RFC 4918 (9.9.4) asks for 409 Conflict,
# so the driver's 409 path -- the one #85 is about -- was held only by a unit
# test over a server written here, which shares our reading of the protocol
# (#91). sabre/dav answers that MOVE 409, evaluates entity tags in an If
# header, and shares no code with Apache or with this repository.
#
# It also departs from the RFCs where Apache does not, which is the point of a
# second reading: it answers 404, not 412, to an If header naming a resource
# that does not exist, and 412, not 304, to a HEAD whose If-None-Match
# matches. Its filesystem tree reports no entity tag for a collection.
#
# BSD-3-Clause, as are its dependencies but psr/log (MIT): composer.lock pins
# every one of them to an exact commit, and the two base images are pinned to
# exact releases published for amd64 and arm64 alike (#16, #20).
#
# PHP's built-in web server, with workers, because this is a test fixture: one
# account, plain HTTP, started by scripts/servers.sh in a throwaway container
# and never shipped. It listens on 8080 because it runs unprivileged.
FROM composer:2.8.12 AS vendor
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-dev --no-interaction --no-progress --classmap-authoritative

FROM php:8.3.29-cli-alpine
WORKDIR /opt/sabredav
COPY --from=vendor /app/vendor ./vendor
COPY server.php ./
RUN mkdir -p /srv/dav /var/lib/sabredav /opt/sabredav/public \
 && chown www-data:www-data /srv/dav /var/lib/sabredav
USER www-data

# The account comes from the environment at start, under the names the Apache
# fixture reads, so scripts/servers.sh passes the same two variables to both.
ENV WEBDAVD_USER=testuser
ENV WEBDAVD_PASS=testpass
ENV PHP_CLI_SERVER_WORKERS=4
EXPOSE 8080
# The document root is an empty directory, so every request reaches the router
# script rather than a static file.
CMD ["php", "-S", "0.0.0.0:8080", "-t", "/opt/sabredav/public", "/opt/sabredav/server.php"]
