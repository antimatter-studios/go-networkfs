//go:build webdav_integration

// An MD5-sess Digest answer signed for the URI each request goes out with
// (#107), against lighttpd's mod_auth behind the `lighttpd-sess` fixture
// (.github/docker/lighttpd-sess, WEBDAV_LIGHTTPD_SESS_URL). lighttpd refuses an
// answer whose uri parameter is not the request's URI, and the fixture refuses
// one that names any algorithm but MD5-sess, so neither a digest signed for
// the wrong URI nor one quietly re-signed as plain MD5 can pass.
//
// Two requests go out for a URI other than the path gowebdav signs for:
//
//   - every request of a mount whose url holds a path, which gowebdav signs
//     for the path relative to that url (#99 is the same against Apache's
//     plain MD5);
//   - a MOVE of a directory named without its trailing slash, which lighttpd
//     answers with a 308 to the slash-terminated URL, re-sent there by the
//     driver (#95 is the same against Apache's plain MD5).

package webdav

import (
	"crypto/rand"
	"strings"
	"testing"
)

func TestIntegrationDigestMD5SessMountWithPathInURL(t *testing.T) {
	cfg := integrationConfig(t, lighttpdSessServer)
	root := cfg["url"]

	// The directory the second mount is rooted at, made through a mount at
	// the server's root, where the digest uri and the request URI agree.
	top := &WebDAVDriver{}
	if err := top.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", root, err)
	}
	dir := "/md5-sess-path-" + strings.ToLower(rand.Text()[:10])
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
		save(t, d, 1, name, []byte("md5-sess"))
		if info := stat(t, d, 1, name); info.Size != int64(len("md5-sess")) || info.IsDir {
			t.Errorf("stat %s: size %d, dir %v", name, info.Size, info.IsDir)
		}
		if got := contents(t, d, 1, name); string(got) != "md5-sess" {
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

func TestIntegrationDigestMD5SessDirectoryWithoutTrailingSlash(t *testing.T) {
	cfg := integrationConfig(t, lighttpdSessServer)
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	dir := "/md5-sess-slash-" + strings.ToLower(rand.Text()[:10])
	if err := d.Mkdir(1, dir+"/"); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = d.Remove(1, dir+"/")
		_ = d.Unmount(1)
	})
	from, to := dir+"/from", dir+"/to"
	if err := d.Mkdir(1, from+"/"); err != nil {
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

	// lighttpd answers "MOVE /dir/from" with 308 to "/dir/from/".
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
