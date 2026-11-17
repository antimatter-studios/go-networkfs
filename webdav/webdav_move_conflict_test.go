package webdav

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	dav "golang.org/x/net/webdav"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

// An unconditional rename or save is one MOVE. gowebdav answers a MOVE's 409
// Conflict by creating the destination's parents and sending the MOVE again,
// with no bound: when there is nothing to create -- the parent is the root of
// the URL space, or MKCOL reports that it already exists -- the same MOVE goes
// out again and again until the goroutine's stack is exhausted (#92).

// runawayMoves is where persistentConflictServer stops answering 409, so a
// client that repeats the MOVE without bound still returns and the test can
// count what it sent instead of crashing.
const runawayMoves = 20

// persistentConflictServer answers every MOVE 409 Conflict, up to
// runawayMoves, and every MKCOL with mkcolStatus. It records both, and serves
// everything else from an in-process WebDAV handler holding a directory /dir,
// so a save's upload into it lands.
func persistentConflictServer(t *testing.T, mkcolStatus int) (url string, requests func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedRequest
	moves := 0
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MOVE", "MKCOL":
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, recordedRequest{method: r.Method, path: r.URL.Path,
				destination: r.Header.Get("Destination"), overwrite: r.Header.Get("Overwrite")})
			if r.Method == "MKCOL" {
				w.WriteHeader(mkcolStatus)
				return
			}
			moves++
			if moves > runawayMoves {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusConflict)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), seen...)
	}
}

func methods(requests []recordedRequest) []string {
	out := make([]string, 0, len(requests))
	for _, r := range requests {
		out = append(out, r.method)
	}
	return out
}

func sameMethods(got []recordedRequest, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i].method != want[i] {
			return false
		}
	}
	return true
}

// A MOVE answered 409 is sent again only after gowebdav created a collection
// for it, and at most once: nothing is left to create when the destination
// sits at the root of the URL space or the server reports the parent already
// there, and a server that keeps answering 409 after creating it still ends
// the operation. Either way the operation fails as ErrNotFound, as a
// conditional MOVE's 409 does.
func TestUnconditionalMoveAnswered409IsBounded(t *testing.T) {
	rename := func(to string) func(d *WebDAVDriver) error {
		return func(d *WebDAVDriver) error { return d.Rename(1, "/a.txt", to) }
	}
	save := func(to string) func(d *WebDAVDriver) error {
		return func(d *WebDAVDriver) error { return api.WriteFile(d, 1, to, []byte("x")) }
	}
	for _, tc := range []struct {
		name        string
		mkcolStatus int
		run         func(d *WebDAVDriver) error
		want        []string
	}{
		{"rename to the root", http.StatusMethodNotAllowed, rename("/b.txt"), []string{"MOVE"}},
		{"save to the root", http.StatusMethodNotAllowed, save("/c.txt"), []string{"MOVE"}},
		{"rename into a parent that exists", http.StatusMethodNotAllowed, rename("/dir/b.txt"), []string{"MOVE", "MKCOL"}},
		{"save into a parent that exists", http.StatusMethodNotAllowed, save("/dir/c.txt"), []string{"MOVE", "MKCOL"}},
		{"rename after the parent was created", http.StatusCreated, rename("/dir/b.txt"), []string{"MOVE", "MKCOL", "MOVE"}},
		{"save after the parent was created", http.StatusCreated, save("/dir/c.txt"), []string{"MOVE", "MKCOL", "MOVE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, requests := persistentConflictServer(t, tc.mkcolStatus)
			d := mount(t, url)
			err := tc.run(d)
			got := requests()
			if !sameMethods(got, tc.want) {
				t.Errorf("sent %d requests %v; want %v", len(got), methods(got[:min(len(got), 6)]), tc.want)
			}
			if !errors.Is(err, api.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound for the MOVE answered 409", err)
			}
		})
	}
}

// The bound does not take away what gowebdav's retry is for: an unconditional
// rename into a parent that does not exist creates it and moves there.
func TestUnconditionalMoveCreatesAMissingParent(t *testing.T) {
	url, requests := conflictServer(t)
	d := mount(t, url)
	if err := d.Rename(1, "/a.txt", "/gone/b.txt"); err != nil {
		t.Fatalf("rename into a missing parent: %v", err)
	}
	if got := requests(); !sameMethods(got, []string{"MOVE", "MKCOL", "MOVE"}) {
		t.Fatalf("sent %v; want MOVE, MKCOL of the parent, MOVE", methods(got))
	}
}
