//go:build webdav_integration

// A SHA-256 Digest answer signed for the URI each request goes out with
// (#111), against lighttpd's mod_auth behind the `lighttpd-sha256` fixture
// (.github/docker/lighttpd-sha256, WEBDAV_LIGHTTPD_SHA256_URL), on its listener
// that offers SHA-256 alone. lighttpd refuses an answer whose uri parameter is
// not the request's URI, and an answer under any other algorithm, so neither a
// digest signed for the wrong URI nor one re-signed as MD5 can pass.
//
// The two requests that go out for a URI other than the path gowebdav signs
// for are those of TestIntegrationDigestMD5SessMountWithPathInURL and
// TestIntegrationDigestMD5SessDirectoryWithoutTrailingSlash: every request of a
// mount whose url holds a path, a name that needs percent-encoding among
// them, and a directory named without its trailing slash, which lighttpd
// redirects to the slash-terminated URL.

package webdav

import "testing"

func TestIntegrationDigestSHA256MountWithPathInURL(t *testing.T) {
	exerciseDigestMountWithPathInURL(t, lighttpdSHA256Server, "sha-256")
}

func TestIntegrationDigestSHA256DirectoryWithoutTrailingSlash(t *testing.T) {
	exerciseDigestDirectoryWithoutTrailingSlash(t, lighttpdSHA256Server, "sha-256")
}
