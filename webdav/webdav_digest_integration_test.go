//go:build webdav_integration

// A directory named without its trailing slash behind HTTP Digest auth (#95),
// against Apache httpd's mod_dav and mod_auth_digest with mod_dir's
// DirectorySlash at its default. A digest response covers the request URI, so
// the request re-sent to "/dir/" after the 301 has to carry credentials
// computed for "/dir/", not the ones computed for "/dir". The server is the
// webdav container's second listener, WEBDAV_DIGEST_URL.

package webdav

import (
	"crypto/rand"
	"strings"
	"testing"
)

// digestRedirectServer is the Apache container's Digest listener. It is not one of
// integrationServers: only this test needs a second authentication scheme.
var digestRedirectServer = integrationServer{name: "apache-digest", urlVar: "WEBDAV_DIGEST_URL"}

func TestIntegrationDirectoryWithoutTrailingSlashBehindDigestAuth(t *testing.T) {
	cfg := integrationConfig(t, digestRedirectServer)
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	dir := "/digest-" + strings.ToLower(rand.Text()[:10])
	if err := d.Mkdir(1, dir); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = d.Remove(1, dir)
		_ = d.Unmount(1)
	})
	from, to := dir+"/from", dir+"/to"

	if err := d.Mkdir(1, from); err != nil {
		t.Fatalf("mkdir %s: %v", from, err)
	}
	save(t, d, 1, from+"/inside.txt", []byte("inside"))

	info, err := d.Stat(1, from)
	if err != nil {
		t.Fatalf("stat %s: %v", from, err)
	}
	if !info.IsDir {
		t.Fatalf("stat %s reported a file", from)
	}

	if err := d.Rename(1, from, to); err != nil {
		t.Fatalf("rename %s to %s: %v", from, to, err)
	}
	absent(t, d, 1, from)
	if got := contents(t, d, 1, to+"/inside.txt"); string(got) != "inside" {
		t.Fatalf("the rename carried %q", got)
	}

	if err := d.Remove(1, to); err != nil {
		t.Fatalf("remove %s: %v", to, err)
	}
	absent(t, d, 1, to)
}
