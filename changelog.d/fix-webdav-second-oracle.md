### Fixed

- A WebDAV conditional save at the revision of a file another client has since removed fails with a precondition conflict on a server that answers its `MOVE` 404 rather than 412, as sabre/dav does. It used to fail as an ordinary error, as if the save's own upload had gone (#91).
- A WebDAV conditional save returns its revision from a server that confirms the target's entity tag by answering the `HEAD` 412 rather than 304, as sabre/dav does. It used to return no revision (#91).
