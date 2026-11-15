package webdav

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// SHA-256 and SHA-512-256 Digest answers signed again for the request URI
// (#111), as MD5 and MD5-sess answers are (#99, #107).
//
// RFC 7616 section 3.9.1's SHA-256 request, with its uri changed: signing it
// for "/dir/index.html", the URI the example requests, must give back the
// response the RFC prints. The RFC has no example for the other three, so
// their responses are the same inputs computed by Python's hashlib, which is
// OpenSSL, following sections 3.4.1 and 3.4.2 term by term:
//
//	H = lambda s: hashlib.new("sha512_256", s.encode()).hexdigest()
//	ha1 = H("Mufasa:http-auth@example.org:Circle of Life")
//	ha1 = H(ha1 + ":" + nonce + ":" + cnonce)   # -sess only
//	H(":".join([ha1, nonce, "00000001", cnonce, "auth", H("GET:/dir/index.html")]))
//	H(ha1 + ":" + nonce + ":" + H("GET:/dir/index.html"))   # without qop
func TestSignDigestForSHAAlgorithms(t *testing.T) {
	for _, tc := range []struct {
		algorithm, withQop, withoutQop string
	}{
		{"SHA-256",
			"753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1",
			"a1306b0595a6c7fe96c448631fb5cfbd5107bd1fe1da729d978dd7446b812363"},
		{"SHA-256-sess",
			"2fd51b3a77ad75bad6afad6003e818d767133c46d9e2749e7f5232ae1ea3efd7",
			"30bc0742357e535c85094121d22cf826b794be0667e19537beb7b28f2251c274"},
		{"SHA-512-256",
			"430d05014cecc49cab6fbe03176d41a1da86cbfe24a16580e22aaad928d960d0",
			"93e9122020ea0624d2ca801426d5bfbeb93d10ff4cc24cb410af3920fa4730e7"},
		{"SHA-512-256-sess",
			"3f2a34f923c38b0fb26dce2fdfc2ce326c23cecf86fbb1444f3e51fbbc2cb92e",
			"7c70dbd9f483cbccedbd3e92ac246a9f2b712bed77914b52c3ae2254fdb2edba"},
	} {
		t.Run(tc.algorithm, func(t *testing.T) {
			const head = `Digest username="Mufasa", realm="http-auth@example.org", `
			const nonce = `nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v"`
			const cnonce = `cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ"`
			const opaque = `opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`
			answer := func(uri, response string) string {
				return head + `uri="` + uri + `", algorithm=` + tc.algorithm + ", " + nonce +
					", nc=00000001, " + cnonce + `, qop=auth, response="` + response + `", ` + opaque
			}
			got := signDigestFor(answer("/index.html", strings.Repeat("0", 64)), http.MethodGet, "/dir/index.html", "Circle of Life")
			if want := answer("/dir/index.html", tc.withQop); got != want {
				t.Errorf("signed for /dir/index.html:\n got %s\nwant %s", got, want)
			}

			// RFC 2069's form, without qop or nc. A -sess answer still carries
			// the client nonce its session key was computed with.
			without := func(uri, response string) string {
				s := head + `uri="` + uri + `", algorithm=` + tc.algorithm + ", " + nonce
				if strings.HasSuffix(tc.algorithm, "-sess") {
					s += ", " + cnonce
				}
				return s + `, response="` + response + `"`
			}
			got = signDigestFor(without("/index.html", "0"), http.MethodGet, "/dir/index.html", "Circle of Life")
			if want := without("/dir/index.html", tc.withoutQop); got != want {
				t.Errorf("signed without qop for /dir/index.html:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// The algorithm is matched without regard to case, and echoed as it was sent.
func TestSignDigestForSHA256NamedInLowerCase(t *testing.T) {
	const authorization = `Digest username="Mufasa", realm="http-auth@example.org", uri="/index.html", ` +
		`algorithm=sha-256, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc=00000001, ` +
		`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop=auth, response="0"`
	want := strings.NewReplacer(`uri="/index.html"`, `uri="/dir/index.html"`,
		`response="0"`, `response="753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"`).Replace(authorization)
	if got := signDigestFor(authorization, http.MethodGet, "/dir/index.html", "Circle of Life"); got != want {
		t.Errorf("signed for /dir/index.html:\n got %s\nwant %s", got, want)
	}
}

// A -sess answer without a client nonce has no session key to compute, and
// goes out as it came, whatever its hash.
func TestSignDigestForSHASessWithoutCnonce(t *testing.T) {
	for _, algorithm := range []string{"SHA-256-sess", "SHA-512-256-sess"} {
		authorization := `Digest username="u", realm="r", nonce="n", uri="/x", algorithm=` + algorithm + `, response="0"`
		if got := signDigestFor(authorization, http.MethodGet, "/dir/x", "p"); got != authorization {
			t.Errorf("a %s answer without cnonce came back as %q", algorithm, got)
		}
	}
}

// A mount of a SHA-256 or SHA-512-256-sess server whose url holds a path, and
// a directory it redirects: as TestDigestMD5SessSignedForTheRequestURI, with
// the expected responses computed by Go's own SHA-256 and SHA-512/256. The
// independent oracle is lighttpd: TestIntegrationDigestSHA256MountWithPathInURL
// and TestIntegrationDigestSHA256DirectoryWithoutTrailingSlash.
func TestDigestSHA256SignedForTheRequestURI(t *testing.T) {
	checkDigestSignedForTheRequestURI(t, "SHA-256", func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	})
}

func TestDigestSHA512256SessSignedForTheRequestURI(t *testing.T) {
	checkDigestSignedForTheRequestURI(t, "SHA-512-256-sess", func(s string) string {
		sum := sha512.Sum512_256([]byte(s))
		return hex.EncodeToString(sum[:])
	})
}

// The transport signs a SHA-256 answer for the URI its request goes out with,
// from what the answer carries: the path an answer under a session the mount
// no longer holds takes, as an MD5 one does (TestSignDigestForMD5Sess).
func TestDigestRequestURIResignsSHA256(t *testing.T) {
	const authorization = `Digest username="Mufasa", realm="http-auth@example.org", uri="/index.html", ` +
		`algorithm=SHA-256, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc=00000001, ` +
		`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop=auth, response="0"`
	var sent string
	transport := &digestRequestURI{
		base: roundTripFunc(func(rq *http.Request) (*http.Response, error) {
			sent = rq.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: rq}, nil
		}),
		password: "Circle of Life",
	}
	rq, _ := http.NewRequest(http.MethodGet, "http://host/dir/index.html", nil)
	rq.Header.Set("Authorization", authorization)
	if _, err := transport.RoundTrip(rq); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, `uri="/dir/index.html"`) ||
		!strings.Contains(sent, `response="753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"`) {
		t.Errorf("sent %s, want the RFC 7616 3.9.1 SHA-256 answer for /dir/index.html", sent)
	}
}

// roundTripFunc is a RoundTripper that is a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(rq *http.Request) (*http.Response, error) { return f(rq) }
