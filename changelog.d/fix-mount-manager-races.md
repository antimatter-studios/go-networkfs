### Fixed
- `MountManager` is safe for concurrent use: mounting or unmounting one ID while other mounts are in use no longer races on the mount table (#69).

### Added
- `MountManager.Acquire` leases a mount for one operation. `Unmount`, and a `Mount` that replaces an ID, wait for leased operations to finish before tearing the old driver down, and refuse new leases at once. Both C archives use it for every operation (#69).

### Changed
- Mounting an ID that is already mounted unmounts the driver it replaces (#69).
