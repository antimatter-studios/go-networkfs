### Fixed

- **An unconditional WebDAV rename or save answered 409 Conflict no longer
  repeats its MOVE without bound.** The MOVE is sent again only after a missing
  parent collection was created for it, and at most once; otherwise the
  operation fails with `ErrNotFound`. Before, a 409 to a destination at the
  root of the server's URL space, or into a parent the server reported present,
  repeated the MOVE until the goroutine's stack was exhausted (#92).
