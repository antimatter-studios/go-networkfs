### Fixed
- WebDAV `Mkdir` over an existing regular file fails and leaves the file alone. A server's 405 answer was read as success whatever was at the path (#67).

### Changed
- WebDAV `Mkdir` of a directory that already exists succeeds, and confirms with a PROPFIND that a directory is there (#67).
