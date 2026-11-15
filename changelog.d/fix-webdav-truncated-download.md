### Fixed
- `networkfs_openfile` and `webdav_openfile` return failure when a download ends before its Content-Length, instead of returning success with the bytes received so far (#64).
- A failed openfile now always sets `out.data` to NULL and `out.len` to 0, so the caller has nothing to free whatever the buffer held before (#64).
