package webdav

// Each Digest algorithm's arithmetic against values this code did not compute
// (#108). MD5 and SHA-256 are RFC 7616's own worked example (section 3.9.1).
// The RFC gives no -sess example, and the response in its SHA-512-256 example
// (3.9.2) does not reproduce under Python's hashlib, so those four are the
// same 3.9.1 inputs computed by Python's hashlib, which is OpenSSL, following
// 3.4.1 and 3.4.2 term by term:
//
//	H = lambda s: hashlib.new("sha512_256", s.encode()).hexdigest()
//	ha1 = H("Mufasa:http-auth@example.org:Circle of Life")
//	ha1 = H(ha1 + ":" + nonce + ":" + cnonce)   # -sess only
//	H(":".join([ha1, nonce, "00000001", cnonce, "auth", H("GET:/dir/index.html")]))

import "testing"

// rfc7616Example is section 3.9.1's request: GET /dir/index.html by Mufasa.
var rfc7616Example = struct {
	user, realm, password, method, uri, nonce, nc, cnonce, qop string
}{
	user: "Mufasa", realm: "http-auth@example.org", password: "Circle of Life",
	method: "GET", uri: "/dir/index.html",
	nonce: "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc: "00000001",
	cnonce: "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop: "auth",
}

func TestDigestAlgorithmsAnswerRFC7616Example(t *testing.T) {
	e := rfc7616Example
	for _, c := range []struct{ name, response string }{
		{"MD5", "8ca523f5e9506fed4657c9700eebdbec"},
		{"SHA-256", "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"},
		{"MD5-sess", "e783283f46242139c486a698fec7211d"},
		{"SHA-256-sess", "2fd51b3a77ad75bad6afad6003e818d767133c46d9e2749e7f5232ae1ea3efd7"},
		{"SHA-512-256", "430d05014cecc49cab6fbe03176d41a1da86cbfe24a16580e22aaad928d960d0"},
		{"SHA-512-256-sess", "3f2a34f923c38b0fb26dce2fdfc2ce326c23cecf86fbb1444f3e51fbbc2cb92e"},
	} {
		a, ok := digestAlgorithmNamed(c.name)
		if !ok {
			t.Errorf("%s: not an algorithm the driver answers", c.name)
			continue
		}
		if a.name != c.name {
			t.Errorf("%s: named %q in an answer, want RFC 7616's spelling", c.name, a.name)
		}
		ha1 := a.ha1(e.user, e.realm, e.password, e.nonce, e.cnonce)
		if got := a.response(ha1, e.method, e.uri, e.nonce, e.nc, e.cnonce, e.qop); got != c.response {
			t.Errorf("%s: response %s, want %s", c.name, got, c.response)
		}
	}
}

// Without a qop the response is RFC 2069's H(HA1:nonce:HA2); SHA-256 by the
// same Python computation.
func TestDigestAlgorithmAnswersWithoutQop(t *testing.T) {
	e := rfc7616Example
	for _, c := range []struct{ name, response string }{
		{"MD5", "7b2cc3b30e75b4777ea31027084363fd"},
		{"SHA-256", "a1306b0595a6c7fe96c448631fb5cfbd5107bd1fe1da729d978dd7446b812363"},
	} {
		a, ok := digestAlgorithmNamed(c.name)
		if !ok {
			t.Errorf("%s: not an algorithm the driver answers", c.name)
			continue
		}
		ha1 := a.ha1(e.user, e.realm, e.password, e.nonce, e.cnonce)
		if got := a.response(ha1, e.method, e.uri, e.nonce, "", "", ""); got != c.response {
			t.Errorf("%s: response %s, want %s", c.name, got, c.response)
		}
	}
}

func TestDigestAlgorithmNamesIgnoreCaseAndDefaultToMD5(t *testing.T) {
	for given, want := range map[string]string{
		"": "MD5", "md5": "MD5", "sha-256": "SHA-256", "Sha-512-256-Sess": "SHA-512-256-sess",
	} {
		if a, ok := digestAlgorithmNamed(given); !ok || a.name != want {
			t.Errorf("%q: %q, %v; want %s", given, a.name, ok, want)
		}
	}
	// SHA-512 in full, SHA-1 and the like are not RFC 7616 algorithms.
	for _, name := range []string{"SHA-512", "SHA-1", "SHA-256-session", "token"} {
		if _, ok := digestAlgorithmNamed(name); ok {
			t.Errorf("%q: answered, want refused", name)
		}
	}
}
