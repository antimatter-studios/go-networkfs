package webdav

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	dav "golang.org/x/net/webdav"
)

// ipv6Request is what a request to the IPv6 server carried.
type ipv6Request struct {
	method, host, destination, ifHeader string
}

// startIPv6WebDAVServer serves an in-process WebDAV handler on the IPv6
// loopback and records every request. golang.org/x/net/webdav cannot evaluate
// an entity tag in an If header (see conditional.go), so a tagged MOVE is
// recorded and then handed on without its conditions, to be performed.
func startIPv6WebDAVServer(t *testing.T) (baseURL, root string, requests func() []ipv6Request) {
	t.Helper()
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatalf("this host has no IPv6 loopback to serve WebDAV on [::1]: %v", err)
	}
	root = t.TempDir()
	handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
	var mu sync.Mutex
	var seen []ipv6Request
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, ipv6Request{r.Method, r.Host, r.Header.Get("Destination"), r.Header.Get("If")})
		mu.Unlock()
		if r.Method == "MOVE" {
			r.Header.Del("If")
			r.Header.Del("If-Match")
		}
		handler.ServeHTTP(w, r)
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server.URL, root, func() []ipv6Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]ipv6Request(nil), seen...)
	}
}

// A URL whose authority is an IPv6 literal keeps its brackets in every URL
// the driver builds from it: the request URI, a MOVE's Destination and the
// resource an If header's tagged list names (#116).
func TestMountOnIPv6AuthorityKeepsItsBrackets(t *testing.T) {
	baseURL, root, requests := startIPv6WebDAVServer(t)
	authority := strings.TrimPrefix(baseURL, "http://")
	if !strings.HasPrefix(authority, "[::1]:") {
		t.Fatalf("server URL %q is not on the bracketed IPv6 loopback", baseURL)
	}
	d := mount(t, baseURL)

	writer, err := d.CreateFile(1, "/a.txt")
	if err != nil {
		t.Fatalf("CreateFile on %s: %v", baseURL, err)
	}
	if _, err := writer.Write([]byte("contents")); err != nil {
		t.Fatalf("Write on %s: %v", baseURL, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close (save) on %s: %v", baseURL, err)
	}

	info, err := d.Stat(1, "/a.txt")
	if err != nil {
		t.Fatalf("Stat on %s: %v", baseURL, err)
	}
	if info.Size != int64(len("contents")) {
		t.Errorf("Stat size = %d; want %d", info.Size, len("contents"))
	}

	reader, err := d.OpenFile(1, "/a.txt")
	if err != nil {
		t.Fatalf("OpenFile on %s: %v", baseURL, err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != "contents" {
		t.Fatalf("read %q, %v; want %q", got, err, "contents")
	}

	if err := d.Rename(1, "/a.txt", "/b.txt"); err != nil {
		t.Fatalf("Rename on %s: %v", baseURL, err)
	}
	info, err = d.Stat(1, "/b.txt")
	if err != nil {
		t.Fatalf("Stat after rename on %s: %v", baseURL, err)
	}
	if err := d.RenameIfMatch(1, "/b.txt", "/c.txt", info.Revision, ""); err != nil {
		t.Fatalf("RenameIfMatch on %s: %v", baseURL, err)
	}

	contents, err := os.ReadFile(filepath.Join(root, "c.txt"))
	if err != nil || string(contents) != "contents" {
		t.Fatalf("server holds c.txt = %q, %v; want %q", contents, err, "contents")
	}

	var moves, taggedMoves int
	for _, r := range requests() {
		if r.host != authority {
			t.Errorf("%s: Host = %q; want %q", r.method, r.host, authority)
		}
		if r.method != "MOVE" {
			continue
		}
		moves++
		if !strings.HasPrefix(r.destination, baseURL+"/") || strings.Contains(r.destination, "%5B") {
			t.Errorf("MOVE Destination = %q; want a URL under %s", r.destination, baseURL)
		}
		if r.ifHeader != "" {
			taggedMoves++
			want := "<" + baseURL + "/b.txt> ([" + info.Revision + "])"
			if r.ifHeader != want {
				t.Errorf("MOVE If = %q; want %q", r.ifHeader, want)
			}
		}
	}
	// The save commits with one MOVE, Rename and RenameIfMatch with one each.
	if moves != 3 {
		t.Errorf("server saw %d MOVE requests; want 3", moves)
	}
	if taggedMoves != 1 {
		t.Errorf("server saw %d tagged MOVE requests; want 1", taggedMoves)
	}
}
