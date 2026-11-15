package webdav

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dav "golang.org/x/net/webdav"
)

func TestSaveReplacesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "save.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("H"), 281), 0600); err != nil {
		t.Fatal(err)
	}
	handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut && request.Header.Get("X-OC-Mtime") == "" {
			if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(request.URL.Path, "/"))); err == nil {
				request.URL.Path += ".collision-copy"
			}
		}
		handler.ServeHTTP(response, request)
	}))
	t.Cleanup(server.Close)
	driver := mount(t, server.URL)
	for _, expected := range [][]byte{bytes.Repeat([]byte("complete-save"), 50000), []byte("shorter-save"), {}} {
		writer, err := driver.CreateFile(1, "/save.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(expected); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		reader, err := driver.OpenFile(1, "/save.bin")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("save left %d bytes at target; want %d", len(actual), len(expected))
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("save created alternate files: %v", entries)
	}
}
