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

func TestSaveFailurePreservesExistingFile(t *testing.T) {
	// "PUT-partial" stores the first bytes of the upload before rejecting it,
	// as a server that runs out of space part-way through does (#68).
	for _, failedMethod := range []string{http.MethodPut, "PUT-partial", "MOVE"} {
		t.Run(failedMethod, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "save.bin")
			original := []byte("existing complete contents")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			handler := &dav.Handler{FileSystem: dav.Dir(root), LockSystem: dav.NewMemLS()}
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Header.Get("X-OC-Mtime") != "" || request.Header.Get("Replace") != "" {
					t.Error("save sent a server-specific overwrite header")
				}
				if request.Method == "MOVE" && request.Header.Get("Overwrite") != "T" {
					t.Error("MOVE did not request standard overwrite semantics")
				}
				if request.Method == http.MethodDelete && !strings.HasPrefix(request.URL.Path, "/.networkfs-upload-") {
					t.Error("save attempted to delete the original")
				}
				if failedMethod == "PUT-partial" && request.Method == http.MethodPut {
					body, _ := io.ReadAll(request.Body)
					partial := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(request.URL.Path, "/")))
					if err := os.WriteFile(partial, body[:min(3, len(body))], 0600); err != nil {
						t.Error(err)
					}
					response.WriteHeader(http.StatusInsufficientStorage)
					return
				}
				if request.Method == failedMethod {
					response.WriteHeader(http.StatusForbidden)
					return
				}
				handler.ServeHTTP(response, request)
			}))
			t.Cleanup(server.Close)
			driver := mount(t, server.URL)
			writer, err := driver.CreateFile(1, "/save.bin")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte("new contents")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err == nil {
				t.Fatal("save reported success after server rejection")
			}
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, original) {
				t.Fatal("failed save changed original contents")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("failed save left temporary uploads: %v", entries)
			}
		})
	}
}
