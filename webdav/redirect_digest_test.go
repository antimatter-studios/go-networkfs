package webdav

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A collection redirect behind HTTP Digest auth (#95). A digest response
// hashes the request URI and must name it in its uri parameter (RFC 7616,
// sections 3.4.1 and 3.4.6), so the request re-sent to "/dir/" after the
// server's 301 needs a response signed for "/dir/", not the one gowebdav
// signed for "/dir". The server here checks both, after authentication and
// before redirecting, in that order, as Apache's mod_auth_digest and mod_dir
// do; the Apache oracle is TestIntegrationDirectoryWithoutTrailingSlashBehindDigestAuth.
func TestDigestCollectionRedirect(t *testing.T) {
	const user, password, realm, nonce = "testuser", "testpass", "test", "bm9uY2U"
	hash := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	var (
		mu       sync.Mutex
		rejected []string
		resent   []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, rest, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if scheme != "Digest" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := map[string]string{}
		for _, field := range strings.Split(rest, ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(field), "=")
			p[k] = strings.Trim(v, `"`)
		}
		want := hash(strings.Join([]string{
			hash(user + ":" + realm + ":" + password), nonce, p["nc"], p["cnonce"], "auth",
			hash(r.Method + ":" + r.RequestURI),
		}, ":"))
		mu.Lock()
		defer mu.Unlock()
		if p["uri"] != r.RequestURI || p["response"] != want {
			rejected = append(rejected, fmt.Sprintf("%s %s signed for %q", r.Method, r.RequestURI, p["uri"]))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/dir":
			http.Redirect(w, r, "/dir/", http.StatusMovedPermanently)
		case "/dir/":
			resent = append(resent, r.Method)
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>`+
				`<D:multistatus xmlns:D="DAV:"><D:response><D:href>/dir/</D:href><D:propstat><D:prop>`+
				`<D:resourcetype><D:collection/></D:resourcetype>`+
				`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": server.URL, "user": user, "pass": password}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	defer func() { _ = d.Unmount(1) }()

	info, err := d.Stat(1, "/dir")
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !info.IsDir {
		t.Errorf("stat /dir: %+v, %v", info, err)
	}
	if len(rejected) > 0 {
		t.Errorf("the server refused requests signed for another URI: %v", rejected)
	}
	if len(resent) != 1 || resent[0] != "PROPFIND" {
		t.Errorf("/dir/ was requested by %v, want one PROPFIND", resent)
	}
}
