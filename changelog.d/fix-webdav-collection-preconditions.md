### Fixed
- A conditional WebDAV remove or rename sends the revision of the resource it names as `If-Match` as well as in the `If` header, so a server that ignores an entity tag in `If` for a directory, as lighttpd 1.4.85 does, refuses a stale directory remove or rename instead of performing it (#102).
- A WebDAV rename answered `200 OK` succeeds; lighttpd answers 200 to a MOVE of a directory, and the driver accepted only 201 and 204 (#102).
- WebDAV mounts normalize explicit HTTP port 80 and HTTPS port 443 consistently in request URLs, MOVE destinations and tagged conditions, avoiding lighttpd's HTTP 502 response to an equivalent default-port destination while preserving custom ports and IPv6 (#115).
