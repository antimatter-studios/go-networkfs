### Added

- `api.SameRevision`, `networkfs_same_revision` and `webdav_same_revision` compare two revisions for whether they name the same state of a file. Apache reports one unchanged WebDAV file as `W/"x"` within a second of a write and as `"x"` after it, so comparing revisions as strings saw a change that never happened; the comparison is RFC 9110's weak comparison for entity tags and exact for anything else, and an empty revision is never the same as anything (#94).

### Changed

- The documentation tells callers to compare revisions with `SameRevision` instead of for string equality (#94).
