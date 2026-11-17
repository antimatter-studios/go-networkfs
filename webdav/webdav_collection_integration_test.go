//go:build webdav_integration

// A directory named without its trailing slash, against Apache httpd's mod_dav
// with mod_dir's DirectorySlash at its default (#83): every request for
// "/dir" is answered with a 301 to "/dir/". sabre/dav answers "/dir" itself,
// so against it this is the same sequence without the redirect.

package webdav

import (
	"testing"
)

func TestIntegrationDirectoryWithoutTrailingSlash(t *testing.T) {
	eachServer(t, func(t *testing.T, server integrationServer) {
		a, b, dir := integrationClients(t, server)
		from, to, renamed := dir+"/from", dir+"/to", dir+"/renamed"

		if err := a.Mkdir(1, from); err != nil {
			t.Fatalf("mkdir %s: %v", from, err)
		}
		if err := a.Mkdir(1, from); err != nil {
			t.Fatalf("mkdir of the existing %s: %v", from, err)
		}
		save(t, a, 1, from+"/inside.txt", []byte("inside"))

		info, err := b.Stat(2, from)
		if err != nil {
			t.Fatalf("stat %s: %v", from, err)
		}
		if !info.IsDir {
			t.Fatalf("stat %s reported a file", from)
		}

		if err := a.Rename(1, from, to); err != nil {
			t.Fatalf("rename %s to %s: %v", from, to, err)
		}
		absent(t, b, 2, from)
		if got := contents(t, b, 2, to+"/inside.txt"); string(got) != "inside" {
			t.Fatalf("the rename carried %q", got)
		}

		// A conditional rename needs the directory's revision, and only a
		// server that gives a directory one can take it.
		if server.collectionRevisions {
			observed := observe(t, a, 1, to)
			if err := a.RenameIfMatch(1, to, renamed, observed.Revision, ""); err != nil {
				t.Fatalf("conditional rename %s to %s: %v", to, renamed, err)
			}
		} else {
			if info := stat(t, a, 1, to); info.Revision != "" {
				t.Fatalf("%s reported revision %q for a directory; integrationServers says it reports none", server.name, info.Revision)
			}
			if err := a.Rename(1, to, renamed); err != nil {
				t.Fatalf("rename %s to %s: %v", to, renamed, err)
			}
		}
		absent(t, b, 2, to)

		if err := a.Remove(1, renamed); err != nil {
			t.Fatalf("remove %s: %v", renamed, err)
		}
		absent(t, b, 2, renamed)
	})
}
