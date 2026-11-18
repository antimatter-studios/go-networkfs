//go:build webdav_integration

// A mount whose url holds the server path, behind HTTP Digest auth (#99),
// against Apache httpd's mod_auth_digest, which refuses a request whose digest
// uri is not the URI it requests with 400 Bad Request ("AH01786: uri
// mismatch"). The server is the webdav container's Digest listener,
// WEBDAV_DIGEST_URL; sabre/dav serves Basic auth only, so this runs against
// Apache alone.

package webdav

import (
	"crypto/rand"
	"os"
	"strings"
	"testing"
)

func TestIntegrationDigestMountWithPathInURL(t *testing.T) {
	root := os.Getenv("WEBDAV_DIGEST_URL")
	if root == "" {
		// A FAILURE, NOT A SKIP, as in integrationConfig.
		t.Fatal("WebDAV digest integration cannot run: WEBDAV_DIGEST_URL unset. " +
			"Run `chore test:webdav` (or `chore test`), which starts the server and exports it.")
	}
	cfg := integrationConfig(t, apacheServer)

	// The directory the second mount is rooted at, made through a mount at
	// the server's root, where the digest uri and the request URI agree.
	// Directories are named with their trailing slash throughout: Apache
	// redirects a collection requested without it, and a digest across that
	// redirect is #95, not this.
	cfg["url"] = root
	top := &WebDAVDriver{}
	if err := top.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	dir := "/digest-path-" + strings.ToLower(rand.Text()[:10])
	if err := top.Mkdir(1, dir+"/"); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = top.Remove(1, dir+"/")
		_ = top.Unmount(1)
	})

	cfg["url"] = strings.TrimSuffix(root, "/") + dir
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	t.Cleanup(func() { _ = d.Unmount(1) })

	if err := d.Mkdir(1, "/sub/"); err != nil {
		t.Fatalf("mkdir /sub under %s: %v", cfg["url"], err)
	}
	for _, name := range []string{"/sub/file.txt", "/sub/with space.txt"} {
		save(t, d, 1, name, []byte("digest"))
		if info := stat(t, d, 1, name); info.Size != int64(len("digest")) {
			t.Errorf("stat %s: size %d", name, info.Size)
		}
		if got := contents(t, d, 1, name); string(got) != "digest" {
			t.Errorf("read %s: %q", name, got)
		}
		if err := d.Remove(1, name); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
		absent(t, d, 1, name)
	}
	// The same file, seen through the mount at the server's root.
	save(t, d, 1, "/sub/seen.txt", []byte("seen"))
	if got := contents(t, top, 1, dir+"/sub/seen.txt"); string(got) != "seen" {
		t.Errorf("the root mount read %q", got)
	}
}
