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

// An MD5-sess Digest answer signed for the URI each request goes out with
// (#107). The server here offers algorithm=MD5-sess and checks every answer
// as RFC 7616 defines it: the uri parameter names the request-target (section
// 3.4.6), the response hashes it under the session key
// H(H(user:realm:password):nonce:cnonce) (section 3.4.2), and no nonce count is
// answered twice under one client nonce (section 3.4, a replay otherwise). The
// mount's url holds a path, so every request goes out for a URI other than
// the path gowebdav hands an authenticator, and "/root/dir" is redirected to
// "/root/dir/", so the request re-sent there needs an answer of its own. The
// independent oracle is lighttpd: TestIntegrationDigestMD5SessMountWithPathInURL
// and TestIntegrationDigestMD5SessDirectoryWithoutTrailingSlash.
func TestDigestMD5SessSignedForTheRequestURI(t *testing.T) {
	const user, password, realm, nonce = "testuser", "testpass", "md5-sess", "c2Vzcy1ub25jZQ"
	hash := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	var (
		mu       sync.Mutex
		rejected []string
		resent   []string
		counted  = map[string]bool{} // cnonce + ":" + nc already answered
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, rest, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if scheme != "Digest" {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=MD5-sess, qop="auth"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := map[string]string{}
		for _, field := range strings.Split(rest, ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(field), "=")
			p[k] = strings.Trim(v, `"`)
		}
		session := hash(hash(user+":"+realm+":"+password) + ":" + nonce + ":" + p["cnonce"])
		want := hash(strings.Join([]string{
			session, nonce, p["nc"], p["cnonce"], "auth", hash(r.Method + ":" + r.RequestURI),
		}, ":"))
		mu.Lock()
		defer mu.Unlock()
		replay := counted[p["cnonce"]+":"+p["nc"]]
		counted[p["cnonce"]+":"+p["nc"]] = true
		switch {
		case p["algorithm"] != "MD5-sess":
			rejected = append(rejected, fmt.Sprintf("%s %s answered with algorithm %q", r.Method, r.RequestURI, p["algorithm"]))
		case p["uri"] != r.RequestURI:
			rejected = append(rejected, fmt.Sprintf("%s %s signed for %q", r.Method, r.RequestURI, p["uri"]))
		case p["response"] != want:
			rejected = append(rejected, fmt.Sprintf("%s %s with a response not signed for it", r.Method, r.RequestURI))
		case replay:
			rejected = append(rejected, fmt.Sprintf("%s %s answered again with nc %s", r.Method, r.RequestURI, p["nc"]))
		default:
			switch r.URL.Path {
			case "/root/dir":
				http.Redirect(w, r, "/root/dir/", http.StatusMovedPermanently)
			default:
				if r.URL.Path == "/root/dir/" {
					resent = append(resent, r.Method)
				}
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>`+
					`<D:multistatus xmlns:D="DAV:"><D:response><D:href>%s</D:href><D:propstat><D:prop>`+
					`<D:resourcetype><D:collection/></D:resourcetype>`+
					`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`,
					r.URL.Path)
			}
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": server.URL + "/root", "user": user, "pass": password}); err != nil {
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
		t.Errorf("the server refused requests: %v", rejected)
	}
	if len(resent) != 1 || resent[0] != "PROPFIND" {
		t.Errorf("/root/dir/ was requested by %v, want one PROPFIND", resent)
	}
}
