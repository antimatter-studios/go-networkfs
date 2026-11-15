package webdav

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// What a conditional DELETE or MOVE carries for the resource it names, and
// how a 200 to a MOVE is read (#102).
//
// lighttpd 1.4.85 ignores an entity tag in an If header when the resource is
// a collection and goes ahead with the DELETE or MOVE; it honours If-Match on
// the same request. So the condition on the request URI goes out in both
// headers. If-Match compares strongly, so a weak tag -- Apache's for a file
// changed within the last second -- goes in the If header alone, where Apache
// compares it weakly; in If-Match it could never match.

type seenRequest struct {
	method, ifHeader, ifMatch string
}

// answeringServer answers every DELETE and MOVE with status, recording each.
func answeringServer(t *testing.T, status int) (string, func() []seenRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []seenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete, "MOVE":
			mu.Lock()
			seen = append(seen, seenRequest{r.Method, r.Header.Get("If"), r.Header.Get("If-Match")})
			mu.Unlock()
			w.WriteHeader(status)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
}

func TestConditionOnTheRequestURIIsAlsoSentAsIfMatch(t *testing.T) {
	url, seen := answeringServer(t, http.StatusNoContent)
	d := mount(t, url)
	const strong, weak = `"r1"`, `W/"r2"`

	if err := d.RemoveIfMatch(1, "/dir", strong); err != nil {
		t.Fatal(err)
	}
	if err := d.RenameIfMatch(1, "/dir", "/moved", strong, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveIfMatch(1, "/dir", weak); err != nil {
		t.Fatal(err)
	}
	if err := d.RenameIfMatch(1, "/dir", "/moved", weak, ""); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 4 {
		t.Fatalf("recorded %d requests; want 4: %+v", len(got), got)
	}
	for i, want := range []string{strong, strong, "", ""} {
		if got[i].ifMatch != want {
			t.Errorf("%s %d: If-Match %q; want %q", got[i].method, i, got[i].ifMatch, want)
		}
		if got[i].ifHeader == "" {
			t.Errorf("%s %d carried no If header", got[i].method, i)
		}
	}
}

// RFC 4918 lists 201 and 204 for a successful MOVE, but any 2xx is success in
// HTTP, and lighttpd answers 200 to a MOVE of a collection.
func TestMoveAnsweredOKSucceeds(t *testing.T) {
	url, _ := answeringServer(t, http.StatusOK)
	d := mount(t, url)
	if err := d.Rename(1, "/dir", "/moved"); err != nil {
		t.Errorf("Rename answered 200: %v", err)
	}
	if err := d.RenameIfMatch(1, "/dir", "/moved", `"r1"`, ""); err != nil {
		t.Errorf("RenameIfMatch answered 200: %v", err)
	}
}
