package webdav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	dav "golang.org/x/net/webdav"
)

// A mount URL whose collection path holds an escaped character -- a space,
// sent as %20 -- names that collection already escaped. gowebdav escapes the
// whole URL it is given, so a prefix handed to it in that form goes out as
// /dav%2520root, a collection the server does not have (#117). Every request
// line, every MOVE's Destination and every resource an If header names has to
// carry the prefix escaped once.

// wireRequest is what a request carried on the wire: the request URI exactly
// as sent, before the server decoded it, and the headers that name URLs.
type wireRequest struct {
	method      string
	requestURI  string
	destination string
	ifHeader    string
}

// escapedPrefixServer serves an in-process WebDAV collection at "/dav root"
// and records every request's wire bytes.
func escapedPrefixServer(t *testing.T) (url string, requests func() []wireRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []wireRequest
	handler := &dav.Handler{Prefix: "/dav root", FileSystem: dav.Dir(t.TempDir()), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, wireRequest{r.Method, r.RequestURI, r.Header.Get("Destination"), r.Header.Get("If")})
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []wireRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]wireRequest(nil), seen...)
	}
}

func TestEscapedMountPrefixIsSentEscapedOnce(t *testing.T) {
	url, requests := escapedPrefixServer(t)
	const prefix = "/dav%20root/"
	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": url + "/dav%20root"}); err != nil {
		t.Fatalf("mount: %v; the server saw %+v", err, requests())
	}
	t.Cleanup(func() { _ = d.Unmount(1) })

	w, err := d.CreateFile(1, "/a b.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("write through the escaped prefix: %v", err)
	}
	if rc, err := d.OpenFile(1, "/a b.txt"); err != nil {
		t.Errorf("read through the escaped prefix: %v", err)
	} else {
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(data) != "hello" {
			t.Errorf("read %q through the escaped prefix; want %q", data, "hello")
		}
	}
	if _, err := d.ListDir(1, "/"); err != nil {
		t.Errorf("list through the escaped prefix: %v", err)
	}
	if err := d.Rename(1, "/a b.txt", "/b.txt"); err != nil {
		t.Errorf("rename through the escaped prefix: %v", err)
	}
	// golang.org/x/net/webdav cannot evaluate an entity tag in an If header
	// and refuses it; only the URLs the request carried are under test.
	_ = d.RenameIfMatch(1, "/b.txt", "/c.txt", `"r1"`, "")

	seen := map[string]bool{}
	for _, r := range requests() {
		seen[r.method] = true
		if !strings.HasPrefix(r.requestURI, prefix) {
			t.Errorf("%s request URI %q; want it under %q", r.method, r.requestURI, prefix)
		}
		if r.method == "MOVE" && !strings.HasPrefix(r.destination, url+prefix) {
			t.Errorf("MOVE Destination %q; want it under %q", r.destination, url+prefix)
		}
		if r.ifHeader != "" && r.ifHeader != `<`+url+prefix+`b.txt> (["r1"])` {
			t.Errorf("%s If header %q; want %q", r.method, r.ifHeader, `<`+url+prefix+`b.txt> (["r1"])`)
		}
	}
	for _, method := range []string{"PROPFIND", "PUT", "GET", "MOVE"} {
		if !seen[method] {
			t.Errorf("no %s request reached the server", method)
		}
	}
	conditional := false
	for _, r := range requests() {
		if r.method == "MOVE" && r.ifHeader != "" {
			conditional = true
		}
	}
	if !conditional {
		t.Error("no conditional MOVE carrying an If header reached the server")
	}
}
