//go:build webdav_integration

// Conditional mutations against a real WebDAV server (issue #70).
//
// Two clients -- two mounts, each its own driver and connection -- share one
// server. Client A observes a file's revision; client B changes the file;
// client A then mutates the file conditioned on what it observed. The server,
// not this driver, decides whether the condition holds, so these tests run
// against an independent implementation: Apache httpd's mod_dav, the
// container scripts/servers.sh starts as `webdav`.
//
// Run with `chore test:webdav`, or by hand with the server up:
//
//	WEBDAV_URL=http://127.0.0.1:8080 WEBDAV_USER=testuser WEBDAV_PASS=testpass \
//	  go test -tags=webdav_integration -run Integration ./webdav/...

package webdav

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

func integrationConfig(t *testing.T) map[string]string {
	t.Helper()
	cfg := map[string]string{
		"url":  os.Getenv("WEBDAV_URL"),
		"user": os.Getenv("WEBDAV_USER"),
		"pass": os.Getenv("WEBDAV_PASS"),
	}
	var missing []string
	for _, k := range []string{"url", "user", "pass"} {
		if cfg[k] == "" {
			missing = append(missing, "WEBDAV_"+strings.ToUpper(k))
		}
	}
	if len(missing) > 0 {
		// A FAILURE, NOT A SKIP: the webdav_integration tag says there is a
		// server, so a missing address is a rig bug, and a skip would report
		// it as a pass.
		t.Fatalf("WebDAV integration cannot run: %s unset. Run `chore test:webdav` "+
			"(or `chore test`), which starts the server and exports them.",
			strings.Join(missing, ", "))
	}
	return cfg
}

// integrationClients mounts two independent clients and gives the test its
// own directory on the server, removed when the test ends.
func integrationClients(t *testing.T) (a, b *WebDAVDriver, dir string) {
	t.Helper()
	cfg := integrationConfig(t)
	a, b = &WebDAVDriver{}, &WebDAVDriver{}
	if err := a.Mount(1, cfg); err != nil {
		t.Fatalf("mount client A: %v", err)
	}
	if err := b.Mount(2, cfg); err != nil {
		t.Fatalf("mount client B: %v", err)
	}
	dir = "/conditional-" + strings.ToLower(rand.Text()[:10])
	if err := a.Mkdir(1, dir); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = a.Remove(1, dir)
		_ = a.Unmount(1)
		_ = b.Unmount(2)
	})
	return a, b, dir
}

func save(t *testing.T, d *WebDAVDriver, id int, path string, data []byte) {
	t.Helper()
	if err := api.WriteFile(d, id, path, data); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contents(t *testing.T, d *WebDAVDriver, id int, path string) []byte {
	t.Helper()
	r, err := d.OpenFile(id, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func observe(t *testing.T, d *WebDAVDriver, id int, path string) api.FileInfo {
	t.Helper()
	info, err := d.Stat(id, path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Revision == "" {
		t.Fatalf("stat %s reported no revision", path)
	}
	return info
}

func absent(t *testing.T, d *WebDAVDriver, id int, path string) {
	t.Helper()
	if _, err := d.Stat(id, path); err == nil {
		t.Fatalf("%s exists; want it absent", path)
	}
}

// listing is the names in dir, which shows any temporary upload a refused
// save left behind.
func listing(t *testing.T, d *WebDAVDriver, id int, dir string) []string {
	t.Helper()
	entries, err := d.ListDir(id, dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}

func wantConflict(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, api.ErrPreconditionFailed) {
		t.Fatalf("%s: got %v, want ErrPreconditionFailed", what, err)
	}
}

// A stale save is refused even when the intervening edit left the size and
// the modification time a client sees unchanged -- the case a comparison of
// Stat before the write would miss. The edit has to land in the same second
// as the observation for that, so the attempt is repeated, each one starting
// at the top of a second, until it does.
func TestIntegrationStaleSaveWithUnchangedSizeAndTimeConflicts(t *testing.T) {
	a, b, dir := integrationClients(t)
	path := dir + "/document.txt"
	original := []byte("original contents A")
	intervening := []byte("intervening edit  B")
	stale := []byte("stale client's save")

	for attempt := 1; ; attempt++ {
		time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
		save(t, a, 1, path, original)
		observed := observe(t, a, 1, path)
		save(t, b, 2, path, intervening)
		after := observe(t, b, 2, path)
		if after.Size != observed.Size || after.ModTime != observed.ModTime {
			if attempt == 5 {
				t.Fatalf("the intervening edit never landed in the observed second: size %d->%d, mtime %d->%d",
					observed.Size, after.Size, observed.ModTime, after.ModTime)
			}
			continue
		}

		err := api.WriteFileIfMatch(a, 1, path, observed.Revision, stale)
		wantConflict(t, "save over an intervening edit", err)
		if got := contents(t, a, 1, path); !bytes.Equal(got, intervening) {
			t.Fatalf("refused save changed the file: %q", got)
		}
		if names := listing(t, a, 1, dir); len(names) != 1 {
			t.Fatalf("refused save left resources behind: %v", names)
		}
		return
	}
}

func TestIntegrationConditionalSave(t *testing.T) {
	a, b, dir := integrationClients(t)
	path := dir + "/notes.txt"
	save(t, a, 1, path, []byte("first"))

	// A save conditioned on the current revision goes through, and so does
	// the next one conditioned on the revision after it.
	observed := observe(t, a, 1, path)
	if err := api.WriteFileIfMatch(a, 1, path, observed.Revision, []byte("second")); err != nil {
		t.Fatalf("save at the current revision: %v", err)
	}
	if got := contents(t, b, 2, path); string(got) != "second" {
		t.Fatalf("client B reads %q after A's save", got)
	}
	observed = observe(t, a, 1, path)
	if err := api.WriteFileIfMatch(a, 1, path, observed.Revision, []byte("third, longer")); err != nil {
		t.Fatalf("second save at the current revision: %v", err)
	}

	// B edits; A's save at the revision it held is refused and B's edit stays.
	observed = observe(t, a, 1, path)
	save(t, b, 2, path, []byte("B's edit"))
	wantConflict(t, "save over B's edit", api.WriteFileIfMatch(a, 1, path, observed.Revision, []byte("A's stale save")))
	if got := contents(t, a, 1, path); string(got) != "B's edit" {
		t.Fatalf("refused save changed the file: %q", got)
	}

	// B removes the file; A's save at the revision it held does not bring
	// it back.
	observed = observe(t, a, 1, path)
	if err := b.Remove(2, path); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, "save over B's removal", api.WriteFileIfMatch(a, 1, path, observed.Revision, []byte("resurrected")))
	absent(t, a, 1, path)
	if names := listing(t, a, 1, dir); len(names) != 0 {
		t.Fatalf("refused saves left resources behind: %v", names)
	}
}

func TestIntegrationConditionalCreate(t *testing.T) {
	a, b, dir := integrationClients(t)
	path := dir + "/new.txt"

	// An empty revision creates a file only where none exists.
	if err := api.WriteFileIfMatch(a, 1, path, "", []byte("created by A")); err != nil {
		t.Fatalf("create where nothing exists: %v", err)
	}
	wantConflict(t, "create over A's file", api.WriteFileIfMatch(b, 2, path, "", []byte("created by B")))
	if got := contents(t, b, 2, path); string(got) != "created by A" {
		t.Fatalf("refused create changed the file: %q", got)
	}
	if names := listing(t, a, 1, dir); len(names) != 1 {
		t.Fatalf("refused create left resources behind: %v", names)
	}
}

func TestIntegrationConditionalRemove(t *testing.T) {
	a, b, dir := integrationClients(t)
	path := dir + "/remove.txt"
	save(t, a, 1, path, []byte("version 1"))

	observed := observe(t, a, 1, path)
	save(t, b, 2, path, []byte("version 2"))
	wantConflict(t, "remove after B's edit", a.RemoveIfMatch(1, path, observed.Revision))
	if got := contents(t, a, 1, path); string(got) != "version 2" {
		t.Fatalf("refused remove changed the file: %q", got)
	}

	observed = observe(t, a, 1, path)
	if err := a.RemoveIfMatch(1, path, observed.Revision); err != nil {
		t.Fatalf("remove at the current revision: %v", err)
	}
	absent(t, b, 2, path)

	// Removing what B already removed is a conflict too: the file A
	// observed is not there to remove.
	save(t, a, 1, path, []byte("again"))
	observed = observe(t, a, 1, path)
	if err := b.Remove(2, path); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, "remove after B's removal", a.RemoveIfMatch(1, path, observed.Revision))

	// A directory is removed at its own revision.
	sub := dir + "/sub"
	if err := a.Mkdir(1, sub); err != nil {
		t.Fatal(err)
	}
	observed = observe(t, a, 1, sub)
	if err := a.RemoveIfMatch(1, sub, observed.Revision); err != nil {
		t.Fatalf("remove a directory at its revision: %v", err)
	}
	absent(t, a, 1, sub)
}

// Rename plus content: a client that renames a file it observed must not
// carry off, or land on top of, contents another client wrote since.
func TestIntegrationConditionalRename(t *testing.T) {
	a, b, dir := integrationClients(t)
	from, to := dir+"/draft.txt", dir+"/final.txt"
	save(t, a, 1, from, []byte("draft"))

	// B edits the source; A's rename at the old revision is refused and
	// the namespace is as B left it.
	observed := observe(t, a, 1, from)
	save(t, b, 2, from, []byte("draft edited by B"))
	wantConflict(t, "rename after B's edit", a.RenameIfMatch(1, from, to, observed.Revision, ""))
	if got := contents(t, a, 1, from); string(got) != "draft edited by B" {
		t.Fatalf("refused rename changed the source: %q", got)
	}
	absent(t, a, 1, to)

	// B creates the destination; a rename that expects it free is refused.
	observed = observe(t, a, 1, from)
	save(t, b, 2, to, []byte("B's final"))
	wantConflict(t, "rename onto B's new file", a.RenameIfMatch(1, from, to, observed.Revision, ""))
	if got := contents(t, a, 1, to); string(got) != "B's final" {
		t.Fatalf("refused rename replaced the destination: %q", got)
	}
	if got := contents(t, a, 1, from); string(got) != "draft edited by B" {
		t.Fatalf("refused rename moved the source: %q", got)
	}

	// A observes both; B edits the destination; A's replacing rename is
	// refused.
	observedFrom, observedTo := observe(t, a, 1, from), observe(t, a, 1, to)
	save(t, b, 2, to, []byte("B's final, revised"))
	wantConflict(t, "rename over B's edited destination", a.RenameIfMatch(1, from, to, observedFrom.Revision, observedTo.Revision))
	if got := contents(t, a, 1, to); string(got) != "B's final, revised" {
		t.Fatalf("refused rename replaced the destination: %q", got)
	}

	// At current revisions the replacing rename goes through.
	observedFrom, observedTo = observe(t, a, 1, from), observe(t, a, 1, to)
	if err := a.RenameIfMatch(1, from, to, observedFrom.Revision, observedTo.Revision); err != nil {
		t.Fatalf("replacing rename at current revisions: %v", err)
	}
	absent(t, b, 2, from)
	if got := contents(t, b, 2, to); string(got) != "draft edited by B" {
		t.Fatalf("rename landed %q", got)
	}

	// And a rename to a free path at the current revision goes through.
	observed = observe(t, a, 1, to)
	moved := dir + "/archived.txt"
	if err := a.RenameIfMatch(1, to, moved, observed.Revision, ""); err != nil {
		t.Fatalf("rename to a free path: %v", err)
	}
	absent(t, a, 1, to)
	if got := contents(t, b, 2, moved); string(got) != "draft edited by B" {
		t.Fatalf("rename landed %q", got)
	}
}

// A conditional rename into a directory another client removed is refused and
// does not bring the directory back (#85). Apache 2.4 answers this MOVE 500,
// not the 409 Conflict RFC 4918 asks for, so it does not reach the 409 path
// gowebdav would answer by creating the parent and retrying; the unit test
// TestConditionalMoveIntoAMissingParentCreatesNothing holds that path. This
// one holds the outcome against a real server whatever status it chooses.
func TestIntegrationConditionalRenameIntoARemovedDirectory(t *testing.T) {
	a, b, dir := integrationClients(t)
	from, target := dir+"/draft.txt", dir+"/target"
	save(t, a, 1, from, []byte("draft"))
	if err := a.Mkdir(1, target); err != nil {
		t.Fatal(err)
	}

	observed := observe(t, a, 1, from)
	observe(t, a, 1, target)
	if err := b.Remove(2, target); err != nil {
		t.Fatal(err)
	}
	err := a.RenameIfMatch(1, from, target+"/final.txt", observed.Revision, "")
	if err == nil {
		t.Fatal("rename into the directory B removed succeeded")
	}
	absent(t, b, 2, target)
	if got := contents(t, b, 2, from); string(got) != "draft" {
		t.Fatalf("refused rename changed the source: %q", got)
	}
	if names := listing(t, b, 2, dir); len(names) != 1 {
		t.Fatalf("refused rename changed the namespace: %v", names)
	}
}

// A conditional save returns the revision of the state it wrote (#84), so a
// client never has to stat for it. A stat after the save can report another
// client's later edit, and a client that adopted it would overwrite that edit
// with its next save without a conflict. Here A carries each save's returned
// revision into the next one; when B saves in between, A's next save is
// refused, although a stat by A at that moment would have handed it a
// revision its save would have gone through at.
func TestIntegrationConditionalSaveReturnsItsRevision(t *testing.T) {
	a, b, dir := integrationClients(t)
	path := dir + "/revisions.txt"

	created, err := api.WriteFileIfMatchRevision(a, 1, path, "", []byte("created by A"))
	if err != nil {
		t.Fatalf("create where nothing exists: %v", err)
	}
	if created == "" {
		t.Fatal("a conditional create returned no revision")
	}

	// The returned revision is the file's: the server accepts a save at it.
	saved, err := api.WriteFileIfMatchRevision(a, 1, path, created, []byte("saved by A"))
	if err != nil {
		t.Fatalf("save at the revision the create returned: %v", err)
	}
	if saved == "" {
		t.Fatal("a conditional save returned no revision")
	}
	resaved, err := api.WriteFileIfMatchRevision(a, 1, path, saved, []byte("saved by A again"))
	if err != nil {
		t.Fatalf("save at the revision the previous save returned: %v", err)
	}
	if resaved == "" {
		t.Fatal("a second conditional save returned no revision")
	}

	// B saves. What a stat would now report is B's revision, not A's.
	save(t, b, 2, path, []byte("B's edit"))
	if observed := observe(t, a, 1, path); observed.Revision == resaved {
		t.Fatalf("B's save left the revision at %q, the one A's save returned", resaved)
	}

	// A's next save, at the revision its own save returned, is refused, and
	// B's edit stays.
	stale, err := api.WriteFileIfMatchRevision(a, 1, path, resaved, []byte("A's stale save"))
	wantConflict(t, "save at the returned revision after B's edit", err)
	if stale != "" {
		t.Fatalf("a refused save returned revision %q", stale)
	}
	if got := contents(t, a, 1, path); string(got) != "B's edit" {
		t.Fatalf("refused save changed the file: %q", got)
	}
	if names := listing(t, a, 1, dir); len(names) != 1 {
		t.Fatalf("saves left resources behind: %v", names)
	}
}
