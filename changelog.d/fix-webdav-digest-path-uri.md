### Fixed
- A WebDAV mount whose `url` holds the server path (`https://host/remote.php/dav/files/user`) works against a server that uses HTTP Digest auth: each request's digest is signed for the URI it actually requests, where it used to be signed for the path relative to the mount, and a server that checks the two refused every request (#99).
