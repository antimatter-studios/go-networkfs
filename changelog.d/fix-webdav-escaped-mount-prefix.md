### Fixed

- A WebDAV mount whose collection path holds an escaped character, such as `/dav%20root`, sends that path escaped once on every request, MOVE Destination and If header, instead of as `/dav%2520root` (#117).
