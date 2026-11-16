### Fixed

- **WebDAV operations on a directory named without its trailing slash work
  against servers that redirect it.** Apache's mod_dir answers `/dir` with a
  301 to `/dir/`, which Go's HTTP client re-sent as a GET, so Stat, Mkdir,
  Remove and Rename of such a directory failed. A redirect that only adds the
  trailing slash, on the same scheme and host, is now followed with the
  original method, headers and body.
