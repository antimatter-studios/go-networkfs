package webdav

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type defaultPortRoundTripper func(*http.Request) (*http.Response, error)

func (transport defaultPortRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestCanonicalBaseURL(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"http://[::1]:80", "http://[::1]"},
		{"https://[::1]:443", "https://[::1]"},
		{"http://[::1]:8080", "http://[::1]:8080"},
		{"http://dav.example:80/dav%20root", "http://dav.example/dav%20root"},
		{"https://user:pass@dav.example:443/dav?key=value#fragment", "https://user:pass@dav.example/dav?key=value#fragment"},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := canonicalBaseURL(test.input)
			if err != nil || got != test.want {
				t.Errorf("canonicalBaseURL(%q) = %q, %v; want %q, nil", test.input, got, err, test.want)
			}
		})
	}
}

func TestMountRejectsMalformedURL(t *testing.T) {
	for _, input := range []string{"http://%gh", "http://dav.example:port", "http://dav.example/%zz"} {
		t.Run(input, func(t *testing.T) {
			driver := &WebDAVDriver{}
			if err := driver.Mount(1, map[string]string{"url": input}); err == nil {
				t.Fatal("malformed URL was accepted")
			}
		})
	}
}

func TestMountCanonicalizesDefaultPorts(t *testing.T) {
	for _, test := range []struct {
		name   string
		config map[string]string
		want   string
	}{
		{"HTTP default", map[string]string{"url": "http://dav.example:80"}, "http://dav.example"},
		{"HTTPS default", map[string]string{"url": "https://dav.example:443"}, "https://dav.example"},
		{"HTTP custom", map[string]string{"url": "http://dav.example:8080"}, "http://dav.example:8080"},
		{"HTTPS custom", map[string]string{"url": "https://dav.example:8443"}, "https://dav.example:8443"},
		{"HTTP port 443", map[string]string{"url": "http://dav.example:443"}, "http://dav.example:443"},
		{"HTTPS port 80", map[string]string{"url": "https://dav.example:80"}, "https://dav.example:80"},
		{"no port", map[string]string{"url": "https://dav.example"}, "https://dav.example"},
		{"HTTP host config", map[string]string{"host": "dav.example", "port": "80"}, "http://dav.example"},
		{"HTTPS host config", map[string]string{"host": "dav.example", "port": "443"}, "https://dav.example"},
		{"path prefix", map[string]string{"url": "http://dav.example:80/dav"}, "http://dav.example/dav"},
	} {
		t.Run(test.name, func(t *testing.T) {
			wantURL, err := url.Parse(test.want)
			if err != nil {
				t.Fatal(err)
			}
			var moves, taggedMoves int
			originalTransport := http.DefaultTransport
			http.DefaultTransport = defaultPortRoundTripper(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != wantURL.Host {
					t.Errorf("request authority = %q; want %q", request.URL.Host, wantURL.Host)
				}
				status := http.StatusOK
				if request.Method == "MOVE" {
					moves++
					if destination := request.Header.Get("Destination"); destination != test.want+"/to" {
						t.Errorf("Destination = %q; want %q", destination, test.want+"/to")
					}
					if condition := request.Header.Get("If"); condition != "" {
						taggedMoves++
						if !strings.Contains(condition, "<"+test.want+"/from>") {
							t.Errorf("If = %q; want canonical source %q", condition, test.want+"/from")
						}
					}
					status = http.StatusNoContent
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			driver := &WebDAVDriver{}
			if err := driver.Mount(1, test.config); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = driver.Unmount(1) })
			if driver.baseURL != test.want {
				t.Errorf("base URL = %q; want %q", driver.baseURL, test.want)
			}
			if err := driver.Rename(1, "/from", "/to"); err != nil {
				t.Fatal(err)
			}
			if err := driver.RenameIfMatch(1, "/from", "/to", `"current"`, ""); err != nil {
				t.Fatal(err)
			}
			if moves != 2 {
				t.Errorf("sent %d MOVE requests; want 2", moves)
			}
			if taggedMoves != 1 {
				t.Errorf("sent %d tagged MOVE requests; want 1", taggedMoves)
			}
		})
	}
}
