package webdav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSlashRedirect(t *testing.T) {
	requested, err := url.Parse("http://dav.example:8080/base/dir")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		status   int
		location string
		want     string
	}{
		{"absolute 301", http.StatusMovedPermanently, "http://dav.example:8080/base/dir/", "http://dav.example:8080/base/dir/"},
		{"relative 301", http.StatusMovedPermanently, "/base/dir/", "http://dav.example:8080/base/dir/"},
		{"302", http.StatusFound, "/base/dir/", "http://dav.example:8080/base/dir/"},
		{"307", http.StatusTemporaryRedirect, "/base/dir/", "http://dav.example:8080/base/dir/"},
		{"308", http.StatusPermanentRedirect, "/base/dir/", "http://dav.example:8080/base/dir/"},
		{"host differs only in case", http.StatusMovedPermanently, "http://DAV.example:8080/base/dir/", "http://DAV.example:8080/base/dir/"},
		{"not a redirect", http.StatusOK, "/base/dir/", ""},
		{"303", http.StatusSeeOther, "/base/dir/", ""},
		{"no location", http.StatusMovedPermanently, "", ""},
		{"another path", http.StatusMovedPermanently, "/elsewhere/", ""},
		{"no slash added", http.StatusMovedPermanently, "/base/dir", ""},
		{"another host", http.StatusMovedPermanently, "http://evil.example:8080/base/dir/", ""},
		{"another port", http.StatusMovedPermanently, "http://dav.example:9090/base/dir/", ""},
		{"another scheme", http.StatusMovedPermanently, "https://dav.example:8080/base/dir/", ""},
		{"credentials added", http.StatusMovedPermanently, "http://u:p@dav.example:8080/base/dir/", ""},
		{"query added", http.StatusMovedPermanently, "/base/dir/?x=1", ""},
		{"unparseable", http.StatusMovedPermanently, "http://[::1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{StatusCode: tc.status, Header: http.Header{}}
			if tc.location != "" {
				response.Header.Set("Location", tc.location)
			}
			target, ok := slashRedirect(requested, response)
			if tc.want == "" {
				if ok {
					t.Fatalf("followed to %s", target)
				}
				return
			}
			if !ok {
				t.Fatal("not followed")
			}
			if target.String() != tc.want {
				t.Fatalf("followed to %s, want %s", target, tc.want)
			}
		})
	}

	// Apache leaves the default port out of the Location it builds.
	for _, pair := range [][2]string{
		{"http://webdav:80/dir", "http://webdav/dir/"},
		{"http://webdav/dir", "http://webdav:80/dir/"},
		{"https://webdav:443/dir", "https://webdav/dir/"},
	} {
		from, _ := url.Parse(pair[0])
		response := &http.Response{StatusCode: http.StatusMovedPermanently, Header: http.Header{"Location": {pair[1]}}}
		if target, ok := slashRedirect(from, response); !ok || target.String() != pair[1] {
			t.Errorf("%s redirected to %s: followed %v to %v", pair[0], pair[1], ok, target)
		}
	}
	for _, pair := range [][2]string{
		{"http://webdav:8080/dir", "http://webdav/dir/"},
		{"https://webdav/dir", "https://webdav:80/dir/"},
	} {
		from, _ := url.Parse(pair[0])
		response := &http.Response{StatusCode: http.StatusMovedPermanently, Header: http.Header{"Location": {pair[1]}}}
		if target, ok := slashRedirect(from, response); ok {
			t.Errorf("%s redirected to %s was followed to %v on another port", pair[0], pair[1], target)
		}
	}

	slashed, _ := url.Parse("http://dav.example:8080/base/dir/")
	response := &http.Response{StatusCode: http.StatusMovedPermanently, Header: http.Header{"Location": {"/base/dir//"}}}
	if _, ok := slashRedirect(slashed, response); ok {
		t.Fatal("a request already ending in a slash was redirected to another slash")
	}
}

// The re-sent request carries the same method, headers and body, and any
// redirect other than the trailing slash goes back to the HTTP client.
func TestCollectionRedirectsResendsTheRequest(t *testing.T) {
	type seen struct{ method, path, depth, body string }
	var requests []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, seen{r.Method, r.URL.Path, r.Header.Get("Depth"), string(body)})
		switch r.URL.Path {
		case "/dir":
			http.Redirect(w, r, "/dir/", http.StatusMovedPermanently)
		case "/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
		default:
			w.WriteHeader(http.StatusMultiStatus)
		}
	}))
	defer server.Close()
	transport := &collectionRedirects{base: http.DefaultTransport}

	// A body the request cannot rewind, as gowebdav sends it.
	request, err := http.NewRequest("PROPFIND", server.URL+"/dir", io.NopCloser(strings.NewReader("<propfind/>")))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Depth", "0")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMultiStatus {
		t.Fatalf("status %d, want 207", response.StatusCode)
	}
	want := []seen{
		{"PROPFIND", "/dir", "0", "<propfind/>"},
		{"PROPFIND", "/dir/", "0", "<propfind/>"},
	}
	if len(requests) != len(want) || requests[0] != want[0] || requests[1] != want[1] {
		t.Fatalf("server saw %+v, want %+v", requests, want)
	}

	// A body the request can rewind, and no body at all.
	for _, body := range []io.Reader{strings.NewReader("<propfind/>"), nil} {
		requests = nil
		request, err = http.NewRequest("PROPFIND", server.URL+"/dir", body)
		if err != nil {
			t.Fatal(err)
		}
		response, err = transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusMultiStatus || len(requests) != 2 || requests[0].body != requests[1].body {
			t.Fatalf("status %d, server saw %+v", response.StatusCode, requests)
		}
	}

	// Any other redirect is handed back untouched.
	for _, method := range []string{"DELETE", http.MethodGet, http.MethodPut} {
		requests = nil
		path := "/moved"
		if method != "DELETE" {
			path = "/dir"
		}
		request, err = http.NewRequest(method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err = transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusMovedPermanently || len(requests) != 1 {
			t.Fatalf("%s %s: status %d, server saw %+v", method, path, response.StatusCode, requests)
		}
	}
}
