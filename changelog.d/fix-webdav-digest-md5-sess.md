### Fixed
- WebDAV mounts behind HTTP Digest auth now answer `algorithm=MD5-sess` challenges. The session key is computed over the server's nonce as RFC 7616 defines it, so these servers can be mounted (#100).
- Every WebDAV Digest answer names the algorithm it used and sends `nc` as eight hexadecimal digits that count the requests made with the nonce. A server that checks either of these no longer refuses the requests (#100).
