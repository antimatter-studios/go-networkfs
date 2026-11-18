### Fixed

- **WebDAV operations on a directory named without its trailing slash work
  against servers that redirect it and demand HTTP Digest auth.** A digest
  response covers the request URI, and the request re-sent to `/dir/` carried
  the one computed for `/dir`, which Apache refuses with 400. The re-sent
  request now goes out with a digest response signed for the slash-terminated
  URL.
