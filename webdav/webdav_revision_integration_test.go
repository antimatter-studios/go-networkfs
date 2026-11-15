//go:build webdav_integration

// One state of a file reports one revision, as api.SameRevision compares them
// (#94), against Apache httpd's mod_dav.
//
// Apache's default entity tag for a file is built from its size and
// modification time, and is marked weak while that modification time is
// within a second of the request: W/"7-66055e976a900" straight after a write,
// "7-66055e976a900" a second later, for a file nobody touched. The weak tag is
// what a revision recorded at save time usually holds, the strong one what a
// later Stat or ListDir reports, and a consumer comparing them as strings sees
// a change that never happened.

package webdav

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

// strongRevisionWait bounds how long the test waits for Apache to stop
// marking a file's tag weak. Apache does so one second after the file's
// modification time; the rest is slack for a slow runner.
const strongRevisionWait = 10 * time.Second

func isWeak(revision string) bool {
	return strings.HasPrefix(revision, "W/")
}

// revisionClients mounts two independent clients and names a file for the
// test in the server's root, removed when the test ends. It makes no
// directory, so it depends on nothing but files: a directory named without its
// trailing slash is answered with a redirect by Apache (#83).
func revisionClients(t *testing.T) (a, b *WebDAVDriver, name string) {
	t.Helper()
	cfg := integrationConfig(t)
	a, b = &WebDAVDriver{}, &WebDAVDriver{}
	if err := a.Mount(1, cfg); err != nil {
		t.Fatalf("mount client A: %v", err)
	}
	if err := b.Mount(2, cfg); err != nil {
		t.Fatalf("mount client B: %v", err)
	}
	name = "revision-" + strings.ToLower(rand.Text()[:10]) + ".txt"
	t.Cleanup(func() {
		_ = a.Remove(1, "/"+name)
		_ = a.Unmount(1)
		_ = b.Unmount(2)
	})
	return a, b, name
}

// listedRevision is the revision ListDir reports for name in dir.
func listedRevision(t *testing.T, d *WebDAVDriver, id int, dir, name string) string {
	t.Helper()
	entries, err := d.ListDir(id, dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e.Revision
		}
	}
	t.Fatalf("list %s: no entry %s", dir, name)
	return ""
}

func TestIntegrationOneStateHasOneRevision(t *testing.T) {
	a, b, name := revisionClients(t)
	path := "/" + name

	saved, err := api.WriteFileIfMatchRevision(a, 1, path, "", []byte("written"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fresh := observe(t, a, 1, path).Revision
	// The case under test is a tag that changes form. If the first stat came
	// too late to see the weak one, the test has not exercised it.
	if !isWeak(fresh) {
		t.Fatalf("stat straight after the write reported %q; the fixture is expected to report a weak tag "+
			"within a second of a write (Apache's FileETag MTime Size), so this test did not exercise #94", fresh)
	}
	if saved == "" {
		t.Fatal("the conditional create returned no revision")
	}

	// Wait, touching nothing, until the server reports the same state with
	// a strong tag.
	deadline := time.Now().Add(strongRevisionWait)
	settled := fresh
	for isWeak(settled) {
		if time.Now().After(deadline) {
			t.Fatalf("stat still reports weak tag %q %v after the write", settled, strongRevisionWait)
		}
		time.Sleep(250 * time.Millisecond)
		settled = observe(t, a, 1, path).Revision
	}
	listed := listedRevision(t, b, 2, "/", name)

	for what, earlier := range map[string]string{
		"the revision the save returned":                     saved,
		"the revision stat reported straight after the save": fresh,
	} {
		for later, now := range map[string]string{"stat": settled, "listdir": listed} {
			if !api.SameRevision(earlier, now) {
				t.Errorf("%s, %q, and the %s of the unchanged file a second later, %q, are not the same revision",
					what, earlier, later, now)
			}
		}
	}

	// The comparison still sees a real change.
	save(t, b, 2, path, []byte("changed"))
	changed := observe(t, a, 1, path).Revision
	for _, earlier := range []string{saved, fresh, settled} {
		if api.SameRevision(earlier, changed) {
			t.Errorf("revision %q before B's save and %q after it compare the same", earlier, changed)
		}
	}
}
