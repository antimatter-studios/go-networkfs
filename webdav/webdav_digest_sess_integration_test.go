//go:build webdav_integration

// HTTP Digest auth with algorithm=MD5-sess against a server that is not ours
// (#100): lighttpd's mod_webdav behind its own mod_auth, the container
// scripts/servers.sh starts as `lighttpd-sess`. lighttpd checks an MD5-sess answer
// as RFC 7616 3.4.2 defines it, and the fixture refuses an answer naming any
// other algorithm (.github/docker/lighttpd-sess), so a client that miscomputes the
// session key, or quietly answers with plain MD5, cannot pass.
//
// Apache, the first WebDAV server, refuses to offer MD5-sess at all, so this
// server is used by the Digest tests alone, not by eachServer. The `lighttpd`
// server eachServer uses is the same lighttpd behind Basic auth.
//
// Run with `chore test:webdav`, or by hand with the server up:
//
//	WEBDAV_LIGHTTPD_SESS_URL=http://127.0.0.1:8085 WEBDAV_USER=testuser WEBDAV_PASS=testpass \
//	  go test -tags=webdav_integration -run IntegrationDigestMD5Sess ./webdav/...

package webdav

import (
	"crypto/rand"
	"strings"
	"testing"
)

var lighttpdSessServer = integrationServer{name: "lighttpd-sess", urlVar: "WEBDAV_LIGHTTPD_SESS_URL"}

func TestIntegrationDigestMD5SessWriteStatReadRemove(t *testing.T) {
	cfg := integrationConfig(t, lighttpdSessServer)
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	dir := "/md5-sess-" + strings.ToLower(rand.Text()[:10])
	if err := d.Mkdir(1, dir); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = d.Remove(1, dir)
		_ = d.Unmount(1)
	})

	path := dir + "/session.txt"
	body := []byte("answered with an MD5-sess session key")
	save(t, d, 1, path, body)
	if info := stat(t, d, 1, path); info.Size != int64(len(body)) || info.IsDir {
		t.Fatalf("stat %s: size %d, dir %v; want a %d-byte file", path, info.Size, info.IsDir, len(body))
	}
	if got := contents(t, d, 1, path); string(got) != string(body) {
		t.Fatalf("read %s: %q, want %q", path, got, body)
	}
	if names := listing(t, d, 1, dir); len(names) != 1 || names[0] != "session.txt" {
		t.Fatalf("list %s: %v, want [session.txt]", dir, names)
	}

	// Overwritten in place, then removed: more requests answered with the
	// same nonce, each with its own nonce count.
	save(t, d, 1, path, []byte("again"))
	if got := contents(t, d, 1, path); string(got) != "again" {
		t.Fatalf("read %s after the overwrite: %q", path, got)
	}
	if err := d.Remove(1, path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	absent(t, d, 1, path)
}
