### Fixed
- WebDAV mounts behind HTTP Digest auth answer `algorithm=SHA-256`, `SHA-512-256` and their `-sess` variants, so a server that offers only SHA-256 can be mounted (#108).
- A WebDAV server that offers several Digest challenges is answered with the first one the driver supports, in the server's order of preference, whether the challenges come in separate `WWW-Authenticate` headers or in one (#108).
