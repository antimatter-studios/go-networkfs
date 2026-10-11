### Changed

- **The family's scripts run in place from rust-fs-core, and this repository
  keeps no copy.** `scripts/core.sh` and `scripts/tier.sh` are gone; CI and
  chores run `../rust-fs-core/scripts/NAME.sh` at the pinned version
  (rust-fs-core 0.3.3, #212).
- **`scripts/resolve-output-budget.sh` is gone with the copies it served.**
  `chore siblings` links the machine's one rust-fs-core checkout beside a
  worktree, and the runner container mounts it at `/rust-fs-core`.

### Fixed

- **A transient HTTP 5xx from the chore release download no longer fails a CI
  job.** `scripts/ci-install-chore.sh` retries both downloads up to five times
  on any error; the checksum check still guards what was fetched.
