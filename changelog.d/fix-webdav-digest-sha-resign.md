### Fixed
- A WebDAV Digest answer under `algorithm=SHA-256`, `SHA-512-256` or their `-sess` variants, re-sent for another URI under a session the mount no longer holds, is signed again for that URI with the algorithm it names, not left signed for the URI it was first sent to (#111).
