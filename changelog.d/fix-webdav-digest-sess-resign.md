### Fixed
- A WebDAV mount behind HTTP Digest auth with `algorithm=MD5-sess` works when its `url` holds a path: every answer is signed for the URI the request goes out with (#107).
- A directory named without its trailing slash behind an MD5-sess Digest server survives the server's redirect: the re-sent request is answered under the same session key with the next nonce count (#107).
