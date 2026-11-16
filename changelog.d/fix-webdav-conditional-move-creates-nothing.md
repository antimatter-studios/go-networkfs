### Fixed

- **A conditional WebDAV move into a missing directory creates nothing.** A
  conditional rename, or the move that commits a conditional save, whose
  destination's parent collection no longer exists fails with `ErrNotFound`
  instead of recreating that collection and trying again, so it no longer
  brings back a directory another client removed. A conditional operation now
  sends only its own request, never a follow-up no precondition covers.
