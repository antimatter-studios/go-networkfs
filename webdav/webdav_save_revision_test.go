package webdav

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	dav "golang.org/x/net/webdav"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

// The revision a conditional save returns (#84), against servers whose
// behaviour after the save's MOVE the test chooses. Against Apache it is
// webdav_conditional_integration_test.go's.

// acceptingServer serves an in-process WebDAV tree whose MOVE accepts every
// condition, as a server would whose condition held, and then calls afterMove
// with the destination's path on disk before answering. golang.org/x/net/webdav
// cannot evaluate an entity tag in an If header, so the header is dropped for
// it; what is under test is what the driver does once a save has gone through.
func acceptingServer(t *testing.T, afterMove func(destination string)) (url, root string) {
	t.Helper()
	root = t.TempDir()
	handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "MOVE" {
			handler.ServeHTTP(w, r)
			return
		}
		r.Header.Del("If")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code < 300 && afterMove != nil {
			destination, err := urlPath(r.Header.Get("Destination"))
			if err != nil {
				t.Errorf("MOVE destination: %v", err)
			} else {
				afterMove(filepath.Join(root, filepath.FromSlash(destination)))
			}
		}
		for k, v := range recorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	return server.URL, root
}

func urlPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

// On a server that keeps a resource's entity tag across a MOVE, a save returns
// the target's revision: the one Stat reports for the state the save wrote.
func TestConditionalSaveReturnsTheRevisionOfWhatItWrote(t *testing.T) {
	url, _ := acceptingServer(t, nil)
	d := mount(t, url)

	created, err := api.WriteFileIfMatchRevision(d, 1, "/doc.txt", "", []byte("created"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := d.Stat(1, "/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if created == "" || created != info.Revision {
		t.Fatalf("create returned revision %q; Stat reports %q", created, info.Revision)
	}

	saved, err := api.WriteFileIfMatchRevision(d, 1, "/doc.txt", created, []byte("saved again, longer"))
	if err != nil {
		t.Fatal(err)
	}
	info, err = d.Stat(1, "/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if saved == "" || saved != info.Revision || saved == created {
		t.Fatalf("save returned revision %q; Stat reports %q, the create returned %q", saved, info.Revision, created)
	}
}

// RFC 4918 lets a server give the destination of a MOVE a new entity tag. The
// upload's tag then names nothing, and the save, which did go through,
// returns no revision rather than one it cannot vouch for.
func TestConditionalSaveReturnsNoRevisionWhenTheMoveChangesIt(t *testing.T) {
	url, root := acceptingServer(t, func(destination string) {
		then := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		if err := os.Chtimes(destination, then, then); err != nil {
			t.Errorf("chtimes: %v", err)
		}
	})
	d := mount(t, url)

	revision, err := api.WriteFileIfMatchRevision(d, 1, "/doc.txt", "", []byte("created"))
	if err != nil {
		t.Fatalf("the save went through and must say so: %v", err)
	}
	if revision != "" {
		t.Fatalf("save returned revision %q for a target whose tag the MOVE changed", revision)
	}
	if got, err := os.ReadFile(filepath.Join(root, "doc.txt")); err != nil || string(got) != "created" {
		t.Fatalf("doc.txt holds %q (%v)", got, err)
	}
}

// Another client's save that lands straight after this one's MOVE is never
// reported as this save's revision: that is the lost update a follow-up Stat
// would invite. The save returns no revision instead.
func TestConditionalSaveNeverReturnsAnotherClientsRevision(t *testing.T) {
	url, _ := acceptingServer(t, func(destination string) {
		if err := os.WriteFile(destination, []byte("another client's save"), 0600); err != nil {
			t.Errorf("intervening save: %v", err)
		}
	})
	d := mount(t, url)

	revision, err := api.WriteFileIfMatchRevision(d, 1, "/doc.txt", "", []byte("created"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := d.Stat(1, "/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if revision == info.Revision {
		t.Fatalf("save returned %q, the revision of another client's save", revision)
	}
	if revision != "" {
		t.Fatalf("save returned revision %q it could not confirm", revision)
	}
}

// sabre/dav 4.7 answers a HEAD whose If-None-Match matches with 412
// Precondition Failed, where RFC 9110 (13.1.2) asks for 304 to a GET or HEAD.
// Both say the condition evaluated false: the target is at the tag. A save
// confirmed that way returns the tag it uploaded (#91).
func TestConditionalSaveRevisionIsConfirmedByA412ToItsHead(t *testing.T) {
	handler := &dav.Handler{FileSystem: dav.Dir(t.TempDir()), LockSystem: dav.NewMemLS()}
	sabre := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MOVE" {
			r.Header.Del("If") // as acceptingServer: the condition holds
		}
		if r.Method != http.MethodHead {
			handler.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		status := recorder.Code
		if status == http.StatusNotModified {
			status = http.StatusPreconditionFailed
		}
		for k, v := range recorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(sabre.Close)
	d := mount(t, sabre.URL)

	created, err := api.WriteFileIfMatchRevision(d, 1, "/doc.txt", "", []byte("created"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := d.Stat(1, "/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if created == "" || created != info.Revision {
		t.Fatalf("create returned revision %q; Stat reports %q", created, info.Revision)
	}
}
