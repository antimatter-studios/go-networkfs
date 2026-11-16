### Added

- `api.WriteFileIfMatchRevision`, `networkfs_writefile_if_revision` and `webdav_writefile_if_revision` return the revision of the state a conditional write created, so a client carries it into its next conditional write instead of stat'ing for it. A stat after the write could report another client's save that landed in between, and a write at that revision replaced the other client's edit without a conflict. The returned string is freed with `networkfs_free` or `webdav_free`; the existing `*_writefile_if` exports are unchanged (#84).

### Fixed

- A WebDAV conditional save returns a revision only when the server confirms the target is at the entity tag of the save's own upload (a `HEAD` with `If-None-Match` after the `MOVE`); it never reports a tag the target returned, which could be another client's. When the server does not confirm it, the save still succeeds and the revision is empty, meaning unknown (#84).
