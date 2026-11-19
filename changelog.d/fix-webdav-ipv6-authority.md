### Fixed

- A WebDAV mount on an IPv6 literal authority, such as `http://[::1]:8080/`, keeps its brackets in every request URL, MOVE Destination and tagged If header instead of failing before its first request.
