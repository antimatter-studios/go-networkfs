### Fixed
- `networkfs_writefile` and `webdav_writefile` refuse a buffer longer than 2^31 - 1 bytes, or a NULL buffer with a nonzero length, before reading it or sending anything. A 4 GiB length used to wrap to zero and replace the file with an empty one (#65).
