package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	dav "golang.org/x/net/webdav"
)

// A directory named without its trailing slash (#83).
//
// Apache's mod_dir answers any request for a collection named without its
// trailing slash -- PROPFIND, DELETE and MOVE included -- with 301 Moved
// Permanently to the slash-terminated URL. Go's HTTP client follows a 301 for
// any method but GET and HEAD as a GET, so the server never sees the PROPFIND,
// DELETE or MOVE at all. The server here does what mod_dir does in front of
// golang.org/x/net/webdav, an implementation that is not ours, and records the
// method of every request that reaches the WebDAV handler.
type slashRedirectServer struct {
	URL  string
	root string

	mu      sync.Mutex
	methods []string
}

func startSlashRedirectServer(t *testing.T) *slashRedirectServer {
	t.Helper()
	s := &slashRedirectServer{root: t.TempDir()}
	handler := &dav.Handler{FileSystem: dav.Dir(s.root), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/") {
			info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(r.URL.Path)))
			if err == nil && info.IsDir() {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
				return
			}
		}
		s.mu.Lock()
		s.methods = append(s.methods, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// noGetsReached fails the test if a request reached the WebDAV handler as a
// GET: nothing below reads a file, so a GET is a redirected PROPFIND, DELETE
// or MOVE.
func (s *slashRedirectServer) noGetsReached(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.methods {
		if strings.HasPrefix(m, http.MethodGet+" ") {
			t.Errorf("a redirect turned a request into %s; requests seen: %v", m, s.methods)
		}
	}
}

func (s *slashRedirectServer) mkdirAll(t *testing.T, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.root, filepath.FromSlash(rel)), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestStatOfDirectoryWithoutTrailingSlashBehindRedirect(t *testing.T) {
	server := startSlashRedirectServer(t)
	server.mkdirAll(t, "dir")
	driver := mount(t, server.URL)

	info, err := driver.Stat(1, "/dir")
	if err != nil {
		t.Fatalf("Stat(/dir): %v", err)
	}
	if !info.IsDir {
		t.Error("Stat(/dir) reported a file")
	}
	if info.Name != "dir" {
		t.Errorf("Stat(/dir) named it %q", info.Name)
	}
	server.noGetsReached(t)
}

func TestMkdirBehindRedirect(t *testing.T) {
	server := startSlashRedirectServer(t)
	server.mkdirAll(t, "existing")
	driver := mount(t, server.URL)

	if err := driver.Mkdir(1, "/fresh"); err != nil {
		t.Fatalf("Mkdir of a new directory: %v", err)
	}
	if info, err := os.Stat(filepath.Join(server.root, "fresh")); err != nil || !info.IsDir() {
		t.Fatalf("Mkdir did not create /fresh: %v", err)
	}
	if err := driver.Mkdir(1, "/existing"); err != nil {
		t.Fatalf("Mkdir of an existing directory: %v", err)
	}
	server.noGetsReached(t)
}

func TestRemoveOfDirectoryBehindRedirect(t *testing.T) {
	server := startSlashRedirectServer(t)
	server.mkdirAll(t, "dir/child")
	driver := mount(t, server.URL)

	if err := driver.Remove(1, "/dir"); err != nil {
		t.Fatalf("Remove(/dir): %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.root, "dir")); !os.IsNotExist(err) {
		t.Fatalf("Remove(/dir) left the directory behind: %v", err)
	}
	server.noGetsReached(t)
}

func TestRenameOfDirectoryBehindRedirect(t *testing.T) {
	server := startSlashRedirectServer(t)
	server.mkdirAll(t, "from/child")
	driver := mount(t, server.URL)

	if err := driver.Rename(1, "/from", "/to"); err != nil {
		t.Fatalf("Rename(/from, /to): %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.root, "from")); !os.IsNotExist(err) {
		t.Fatalf("Rename left the source behind: %v", err)
	}
	if info, err := os.Stat(filepath.Join(server.root, "to", "child")); err != nil || !info.IsDir() {
		t.Fatalf("Rename did not carry the directory to /to: %v", err)
	}
	server.noGetsReached(t)
}
