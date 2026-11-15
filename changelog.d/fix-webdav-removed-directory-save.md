### Fixed

- **A conditional WebDAV save into a missing directory creates nothing.** A
  conditional save (`CreateFileIfMatch`, `WriteFileIfMatch`) whose directory
  no longer exists fails with `ErrNotFound`. Its temporary upload no longer
  recreates a directory another client removed, so a create-only save can no
  longer land inside it.
