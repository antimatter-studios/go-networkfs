package webdav

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	dav "golang.org/x/net/webdav"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

// The conditional operations against servers that are not the integration
// fixture: what goes on the wire, what a refusal maps to, and what happens
// when a server cannot evaluate the condition at all. Their behaviour against
// a server that can is webdav_conditional_integration_test.go's.

func TestStatAndListDirReportTheEntityTag(t *testing.T) {
	url, root := startWebDAVServer(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	d := mount(t, url)
	info, err := d.Stat(1, "/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !entityTag.MatchString(info.Revision) {
		t.Fatalf("Stat revision %q is not an entity tag", info.Revision)
	}
	entries, err := d.ListDir(1, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Revision != info.Revision {
		t.Fatalf("ListDir revisions %+v; want Stat's %q", entries, info.Revision)
	}
}

// golang.org/x/net/webdav implements the If header for lock tokens and treats
// an entity tag in it as a condition it cannot confirm. Every conditional
// operation must then fail and change nothing -- never go ahead as if no
// condition had been asked for.
func TestServerThatCannotEvaluateEntityTagsChangesNothing(t *testing.T) {
	url, root := startWebDAVServer(t)
	for name, body := range map[string]string{"a.txt": "file a", "b.txt": "file b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d := mount(t, url)
	a, err := d.Stat(1, "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Stat(1, "/b.txt")
	if err != nil {
		t.Fatal(err)
	}

	for what, err := range map[string]error{
		"save":              api.WriteFileIfMatch(d, 1, "/a.txt", a.Revision, []byte("replaced")),
		"remove":            d.RemoveIfMatch(1, "/a.txt", a.Revision),
		"rename":            d.RenameIfMatch(1, "/a.txt", "/c.txt", a.Revision, ""),
		"replacing rename":  d.RenameIfMatch(1, "/a.txt", "/b.txt", a.Revision, b.Revision),
		"create over b.txt": api.WriteFileIfMatch(d, 1, "/b.txt", "", []byte("replaced")),
	} {
		if !errors.Is(err, api.ErrPreconditionFailed) {
			t.Errorf("%s: got %v, want ErrPreconditionFailed", what, err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("refused operations changed the namespace: %v", entries)
	}
	for name, body := range map[string]string{"a.txt": "file a", "b.txt": "file b"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != body {
			t.Fatalf("%s holds %q (%v); want %q", name, got, err, body)
		}
	}
}

// recordedRequest is what a conditional request carried.
type recordedRequest struct {
	method, path, destination, overwrite, ifHeader string
}

// recordingServer answers every MOVE and DELETE with status after recording
// it, and serves everything else from an in-process WebDAV handler.
func recordingServer(t *testing.T, status int) (url string, requests func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedRequest
	handler := &dav.Handler{FileSystem: dav.Dir(t.TempDir()), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MOVE" || r.Method == http.MethodDelete {
			mu.Lock()
			seen = append(seen, recordedRequest{r.Method, r.URL.Path, r.Header.Get("Destination"),
				r.Header.Get("Overwrite"), r.Header.Get("If")})
			mu.Unlock()
			if !strings.Contains(r.URL.Path, ".networkfs-upload-") || r.Method == "MOVE" {
				w.WriteHeader(status)
				return
			}
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

func mountWithPrefix(t *testing.T, url, prefix string) *WebDAVDriver {
	t.Helper()
	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": url, "path": prefix}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Unmount(1) })
	return d
}

// Each condition is a tagged list naming the resource by the URL the request
// itself uses for it, prefix included, so the server can match it.
func TestConditionsAreTaggedListsOnTheResourcesTheyName(t *testing.T) {
	url, requests := recordingServer(t, http.StatusNoContent)
	d := mountWithPrefix(t, url, "/dav")
	const rev, rev2 = `"r1"`, `W/"r2"`

	if err := d.RemoveIfMatch(1, "/a b.txt", rev); err != nil {
		t.Fatal(err)
	}
	if err := d.RenameIfMatch(1, "/a.txt", "/b.txt", rev, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.RenameIfMatch(1, "/a.txt", "/b.txt", rev, rev2); err != nil {
		t.Fatal(err)
	}
	if err := api.WriteFileIfMatch(d, 1, "/c.txt", rev2, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := api.WriteFileIfMatch(d, 1, "/c.txt", "", []byte("x")); err != nil {
		t.Fatal(err)
	}

	got := requests()
	if len(got) != 5 {
		t.Fatalf("recorded %d requests; want 5: %+v", len(got), got)
	}
	want := []recordedRequest{
		{"DELETE", "/dav/a b.txt", "", "", `<` + url + `/dav/a%20b.txt> (["r1"])`},
		{"MOVE", "/dav/a.txt", url + "/dav/b.txt", "F", `<` + url + `/dav/a.txt> (["r1"])`},
		{"MOVE", "/dav/a.txt", url + "/dav/b.txt", "T", `<` + url + `/dav/a.txt> (["r1"]) <` + url + `/dav/b.txt> ([W/"r2"])`},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("request %d:\n got  %+v\n want %+v", i, got[i], w)
		}
	}
	// The saves move their own temporary upload; only the target is
	// conditioned.
	if got[3].overwrite != "T" || got[3].ifHeader != `<`+url+`/dav/c.txt> ([W/"r2"])` {
		t.Errorf("conditional save: %+v", got[3])
	}
	if got[4].overwrite != "F" || got[4].ifHeader != "" {
		t.Errorf("create-only save: %+v", got[4])
	}
	for _, r := range got[3:] {
		if !strings.Contains(r.path, "/dav/.networkfs-upload-") || r.destination != url+"/dav/c.txt" {
			t.Errorf("save moved %q to %q", r.path, r.destination)
		}
	}
}

// 412 is a conflict wherever it comes from. 404 is one when the resource the
// caller observed has gone, and an ordinary failure otherwise.
func TestRefusalsMapToPreconditionFailed(t *testing.T) {
	for _, status := range []int{http.StatusPreconditionFailed, http.StatusNotFound} {
		url, _ := recordingServer(t, status)
		d := mount(t, url)
		for what, err := range map[string]error{
			"remove": d.RemoveIfMatch(1, "/a.txt", `"r"`),
			"rename": d.RenameIfMatch(1, "/a.txt", "/b.txt", `"r"`, ""),
		} {
			if !errors.Is(err, api.ErrPreconditionFailed) {
				t.Errorf("%s answered %d: got %v, want ErrPreconditionFailed", what, status, err)
			}
		}
		err := api.WriteFileIfMatch(d, 1, "/a.txt", `"r"`, []byte("x"))
		switch {
		case status == http.StatusPreconditionFailed && !errors.Is(err, api.ErrPreconditionFailed):
			t.Errorf("save answered 412: got %v, want ErrPreconditionFailed", err)
		case status == http.StatusNotFound && (err == nil || errors.Is(err, api.ErrPreconditionFailed)):
			t.Errorf("save whose upload vanished: got %v, want an ordinary failure", err)
		}
	}
}

// A revision travels inside a header, so one that is not an entity tag is
// refused before anything is sent, and none of these may be empty.
func TestRevisionsAreCheckedBeforeAnythingIsSent(t *testing.T) {
	url, requests := recordingServer(t, http.StatusNoContent)
	d := mount(t, url)
	for _, bad := range []string{"", "r1", `"r1"]`, `"r"1"`, "\"r\r\n\"", `W/r1`} {
		if bad != "" {
			if _, err := d.CreateFileIfMatch(1, "/a.txt", bad); err == nil {
				t.Errorf("save accepted revision %q", bad)
			}
			if err := d.RenameIfMatch(1, "/a.txt", "/b.txt", `"ok"`, bad); err == nil {
				t.Errorf("rename accepted destination revision %q", bad)
			}
		}
		if err := d.RemoveIfMatch(1, "/a.txt", bad); err == nil {
			t.Errorf("remove accepted revision %q", bad)
		}
		if err := d.RenameIfMatch(1, "/a.txt", "/b.txt", bad, ""); err == nil {
			t.Errorf("rename accepted source revision %q", bad)
		}
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("refused revisions still sent %+v", got)
	}
}

func TestConditionalOperationsNeedAMount(t *testing.T) {
	d := &WebDAVDriver{}
	if _, err := d.CreateFileIfMatch(1, "/a", `"r"`); err != api.ErrNotConnected {
		t.Errorf("CreateFileIfMatch: %v", err)
	}
	if err := d.RemoveIfMatch(1, "/a", `"r"`); err != api.ErrNotConnected {
		t.Errorf("RemoveIfMatch: %v", err)
	}
	if err := d.RenameIfMatch(1, "/a", "/b", `"r"`, ""); err != api.ErrNotConnected {
		t.Errorf("RenameIfMatch: %v", err)
	}
}
