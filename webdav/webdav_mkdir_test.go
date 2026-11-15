package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	dav "golang.org/x/net/webdav"
)

// Mkdir succeeds only when a directory exists at the path afterwards (#67).
//
// MKCOL on an existing resource answers 405, and gowebdav reads 405 as
// success whatever the resource is. The server here is golang.org/x/net/webdav,
// an implementation that is not ours, over a real directory.
func TestMkdirOverRegularFileFails(t *testing.T) {
	baseURL, root := startWebDAVServer(t)
	file := filepath.Join(root, "collision")
	if err := os.WriteFile(file, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	driver := mount(t, baseURL)

	if err := driver.Mkdir(1, "/collision"); err == nil {
		t.Fatal("Mkdir over a regular file reported success")
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("Mkdir replaced the regular file with a directory")
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "keep me" {
		t.Fatalf("Mkdir changed the file to %q", contents)
	}
}

// Creating a directory that already exists succeeds and leaves it as it was:
// the caller asked for a directory at that path, and one is there.
func TestMkdirOverExistingDirectorySucceeds(t *testing.T) {
	baseURL, root := startWebDAVServer(t)
	if err := os.MkdirAll(filepath.Join(root, "existing", "child"), 0700); err != nil {
		t.Fatal(err)
	}
	driver := mount(t, baseURL)

	if err := driver.Mkdir(1, "/existing"); err != nil {
		t.Fatalf("Mkdir over an existing directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "existing", "child")); err != nil {
		t.Fatalf("Mkdir disturbed the existing directory: %v", err)
	}
	if err := driver.Mkdir(1, "/fresh"); err != nil {
		t.Fatalf("Mkdir of a new directory: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "fresh")); err != nil || !info.IsDir() {
		t.Fatalf("Mkdir did not create /fresh: %v", err)
	}
}

// A server that refuses MKCOL is reported as a failure, not turned into
// success by the existence check.
func TestMkdirPermissionFailureIsReported(t *testing.T) {
	root := t.TempDir()
	handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MKCOL" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	if err := os.Mkdir(filepath.Join(root, "existing"), 0700); err != nil {
		t.Fatal(err)
	}
	driver := mount(t, server.URL)

	for _, path := range []string{"/denied", "/existing"} {
		if err := driver.Mkdir(1, path); err == nil {
			t.Errorf("Mkdir(%s) succeeded although the server refused MKCOL", path)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "denied")); !os.IsNotExist(err) {
		t.Errorf("a refused Mkdir left something at /denied: %v", err)
	}
}
