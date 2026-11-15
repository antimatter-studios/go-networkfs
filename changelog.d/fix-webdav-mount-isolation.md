### Fixed
- The standalone WebDAV archive keeps one driver per mount ID. Writes and unmounts used to reach whichever server was mounted last, and unmounting one ID disconnected every other (#66).

### Changed
- `webdav_unmount` of an ID that is not mounted returns 1, and every other export fails for an unknown ID, as the unified archive already did (#66).
