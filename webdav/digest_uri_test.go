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

	"github.com/antimatter-studios/go-networkfs/pkg/api"
	dav "golang.org/x/net/webdav"
)

// A Digest response signed for the request URI actually sent (#99).

// The worked examples of RFC 2617, section 3.5, and RFC 7616, section 3.9.1,
// with their uri changed: signing them for the URI each example requests must
// give back the response each RFC prints.
func TestSignDigestForReproducesTheRFCExamples(t *testing.T) {
	for _, tc := range []struct {
		name, authorization, password, want string
	}{
		{
			name: "RFC 2617",
			authorization: `Digest username="Mufasa", realm="testrealm@host.com", ` +
				`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", uri="/index.html", qop=auth, ` +
				`nc=00000001, cnonce="0a4f113b", response="00000000000000000000000000000000", ` +
				`opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
			password: "Circle Of Life",
			want: `Digest username="Mufasa", realm="testrealm@host.com", ` +
				`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", uri="/dir/index.html", qop=auth, ` +
				`nc=00000001, cnonce="0a4f113b", response="6629fae49393a05397450978507c4ef1", ` +
				`opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
		},
		{
			name: "RFC 7616 MD5",
			authorization: `Digest username="Mufasa", realm="http-auth@example.org", uri="/index.html", ` +
				`algorithm=MD5, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc=00000001, ` +
				`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop=auth, ` +
				`response="00000000000000000000000000000000", ` +
				`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`,
			password: "Circle of Life",
			want: `Digest username="Mufasa", realm="http-auth@example.org", uri="/dir/index.html", ` +
				`algorithm=MD5, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc=00000001, ` +
				`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop=auth, ` +
				`response="8ca523f5e9506fed4657c9700eebdbec", ` +
				`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := signDigestFor(tc.authorization, http.MethodGet, "/dir/index.html", tc.password); got != tc.want {
				t.Errorf("signed for /dir/index.html:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// What the signer does not compute, or need not, goes out as it came.
func TestSignDigestForLeavesOtherAuthorizationsAlone(t *testing.T) {
	for _, authorization := range []string{
		"",
		"Basic dGVzdHVzZXI6dGVzdHBhc3M=",
		`Digest username="u", realm="r", nonce="n", uri="/x", algorithm=SHA-256, qop=auth, nc=00000001, cnonce="c", response="r"`,
		`Digest username="u", realm="r", nonce="n", uri="/x", qop=auth-int, nc=00000001, cnonce="c", response="r"`,
		`Digest username="u", realm="r", nonce="n", uri="/root/x", qop=auth, nc=00000001, cnonce="c", response="r"`,
	} {
		if got := signDigestFor(authorization, http.MethodGet, "/root/x", "p"); got != authorization {
			t.Errorf("%q came back as %q", authorization, got)
		}
	}
}

// digestServer is golang.org/x/net/webdav under prefix behind a Digest check
// written from RFC 7616, section 3.4: the uri parameter must be the request
// URI, and the response must be the one computed for it. A request failing
// either is answered 400, as Apache's mod_auth_digest answers it.
type digestServer struct {
	URL string

	mu       sync.Mutex
	rejected []string
}

func startDigestServer(t *testing.T, prefix, user, password string) *digestServer {
	t.Helper()
	const realm, nonce = "test", "bm9uY2U"
	s := &digestServer{}
	handler := &dav.Handler{Prefix: prefix, FileSystem: dav.Dir(t.TempDir()), LockSystem: dav.NewMemLS()}
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
		hash := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
		want := hash(strings.Join([]string{
			hash(user + ":" + realm + ":" + password), nonce, p["nc"], p["cnonce"], "auth",
			hash(r.Method + ":" + r.RequestURI),
		}, ":"))
		if p["uri"] != r.RequestURI || p["response"] != want {
			s.mu.Lock()
			s.rejected = append(s.rejected, fmt.Sprintf("%s %s signed for %q", r.Method, r.RequestURI, p["uri"]))
			s.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

func (s *digestServer) noneRejected(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rejected) > 0 {
		t.Errorf("the server refused requests signed for another URI: %v", s.rejected)
	}
}

// A mount whose url holds the server path stats, writes and removes a file,
// one whose name needs percent-encoding among them.
func TestDigestMountWithPathInURL(t *testing.T) {
	server := startDigestServer(t, "/remote/dav", "testuser", "testpass")
	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": server.URL + "/remote/dav", "user": "testuser", "pass": "testpass"}); err != nil {
		server.noneRejected(t)
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = d.Unmount(1) })

	for _, name := range []string{"/plain.txt", "/with space.txt"} {
		if err := api.WriteFile(d, 1, name, []byte("digest")); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		info, err := d.Stat(1, name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Size != int64(len("digest")) {
			t.Errorf("stat %s: size %d", name, info.Size)
		}
		if err := d.Remove(1, name); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
		if _, err := d.Stat(1, name); err == nil {
			t.Errorf("%s survived its removal", name)
		}
	}
	server.noneRejected(t)
}
