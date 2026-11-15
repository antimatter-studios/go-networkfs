package webdav

// HTTP authentication for the driver's requests, with a Digest answer as
// RFC 7616 defines it (#100).
//
// gowebdav v0.12.0's NewAutoAuth negotiates Basic, Digest and Passport from a
// server's challenge, but its Digest answer departs from RFC 7616:
//
//   - for algorithm=MD5-sess it hashes the nonce count, not the server's
//     nonce, into the session key (section 3.4.2 says nonce);
//   - it never names the algorithm it used, so a server reads every answer as
//     plain MD5;
//   - it sends nc=1 on every request, where section 3.4 asks for exactly
//     eight hexadecimal digits counting the requests sent with the nonce.
//
// So a server that offers MD5-sess cannot be mounted, and a strict server can
// refuse every request. newAuthorizer is NewAutoAuth with the same three
// schemes in the same order, and this file's Digest authenticator in place of
// gowebdav's. The digest uri is still the path gowebdav hands an
// authenticator; signing for the request URI on the wire is #99's.

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/studio-b12/gowebdav"
)

// newAuthorizer returns the authorizer every request of a mount goes through.
func newAuthorizer(user, password string) gowebdav.Authorizer {
	authorizer := gowebdav.NewEmptyAuth()
	authorizer.AddAuthenticator("basic", func(*http.Client, *http.Response, string) (gowebdav.Authenticator, error) {
		return &basicAuth{user: user, password: password}, nil
	})
	authorizer.AddAuthenticator("digest", func(_ *http.Client, rs *http.Response, _ string) (gowebdav.Authenticator, error) {
		return newDigestAuth(user, password, rs.Header.Get("Www-Authenticate"))
	})
	authorizer.AddAuthenticator("passport1.4", func(c *http.Client, rs *http.Response, _ string) (gowebdav.Authenticator, error) {
		return gowebdav.NewPassportAuth(c, user, password, rs.Request.URL.String(), &rs.Header)
	})
	return authorizer
}

// basicAuth is gowebdav's BasicAuth, which cannot be built outside gowebdav
// except through NewAutoAuth.
type basicAuth struct{ user, password string }

func (b *basicAuth) Authorize(_ *http.Client, rq *http.Request, _ string) error {
	rq.SetBasicAuth(b.user, b.password)
	return nil
}

func (b *basicAuth) Verify(_ *http.Client, rs *http.Response, path string) (bool, error) {
	if rs.StatusCode == http.StatusUnauthorized {
		return false, gowebdav.NewPathError("Authorize", path, rs.StatusCode)
	}
	return false, nil
}

func (b *basicAuth) Clone() gowebdav.Authenticator { return b }
func (b *basicAuth) Close() error                  { return nil }

// digestSession is what one Digest challenge establishes, shared by every
// request answered under it: gowebdav clones the authenticator for each
// request, and the nonce count has to count across all of them.
type digestSession struct {
	realm, nonce, opaque string
	// algorithm is the challenge's own spelling, echoed in each answer; ""
	// when the challenge named none, which means MD5.
	algorithm string
	// qop is "auth" when the challenge offered it, "" when it offered no qop
	// at all (RFC 2069's answer, without nc).
	qop string
	// sess: the algorithm is MD5-sess, so ha1 is a session key.
	sess bool
	// cnonce is the client nonce for the whole session, and ha1 is
	// H(user:realm:password), or for -sess the session key
	// H(H(user:realm:password):nonce:cnonce) (RFC 7616 3.4.2).
	cnonce, ha1 string
	// nc is the number of requests answered with this nonce so far.
	nc atomic.Uint64
}

type digestAuth struct {
	user    string
	session *digestSession
}

// newDigestAuth answers the Digest challenge in header. An algorithm other
// than MD5 and MD5-sess, or a qop list without "auth", is refused rather than
// answered wrongly, so negotiation can fall to another scheme the server
// offered.
func newDigestAuth(user, password, header string) (gowebdav.Authenticator, error) {
	params, ok := digestChallengeParams(header)
	if !ok {
		return nil, fmt.Errorf("webdav: no Digest challenge in %q", header)
	}
	s := &digestSession{
		realm:     params["realm"],
		nonce:     params["nonce"],
		opaque:    params["opaque"],
		algorithm: params["algorithm"],
	}
	if s.nonce == "" {
		return nil, fmt.Errorf("webdav: Digest challenge without a nonce: %q", header)
	}
	switch strings.ToLower(s.algorithm) {
	case "", "md5":
	case "md5-sess":
		s.sess = true
	default:
		return nil, fmt.Errorf("webdav: Digest algorithm %q is not supported", s.algorithm)
	}
	if offered, present := params["qop"]; present {
		for _, q := range strings.Split(offered, ",") {
			if strings.EqualFold(strings.TrimSpace(q), "auth") {
				s.qop = "auth"
			}
		}
		if s.qop == "" {
			return nil, fmt.Errorf("webdav: Digest qop %q offers no \"auth\"", offered)
		}
	}
	cnonce := make([]byte, 16)
	if _, err := rand.Read(cnonce); err != nil {
		return nil, err
	}
	s.cnonce = hex.EncodeToString(cnonce)
	s.ha1 = md5Hex(user + ":" + s.realm + ":" + password)
	if s.sess {
		s.ha1 = md5Hex(s.ha1 + ":" + s.nonce + ":" + s.cnonce)
	}
	return &digestAuth{user: user, session: s}, nil
}

func (d *digestAuth) Authorize(_ *http.Client, rq *http.Request, path string) error {
	s := d.session
	ha2 := md5Hex(rq.Method + ":" + path)
	fields := []string{
		"username=" + quote(d.user),
		"realm=" + quote(s.realm),
		"nonce=" + quote(s.nonce),
		"uri=" + quote(path),
	}
	if s.algorithm != "" {
		fields = append(fields, "algorithm="+s.algorithm)
	}
	var response string
	if s.qop == "" {
		response = md5Hex(s.ha1 + ":" + s.nonce + ":" + ha2)
		if s.sess {
			fields = append(fields, "cnonce="+quote(s.cnonce))
		}
	} else {
		nc := fmt.Sprintf("%08x", s.nc.Add(1))
		response = md5Hex(strings.Join([]string{s.ha1, s.nonce, nc, s.cnonce, s.qop, ha2}, ":"))
		fields = append(fields, "qop="+s.qop, "nc="+nc, "cnonce="+quote(s.cnonce))
	}
	fields = append(fields, "response="+quote(response))
	if s.opaque != "" {
		fields = append(fields, "opaque="+quote(s.opaque))
	}
	rq.Header.Set("Authorization", "Digest "+strings.Join(fields, ", "))
	return nil
}

// Verify asks for a fresh challenge when the server calls the nonce stale,
// as gowebdav's own Digest authenticator does; any other 401 is final.
func (d *digestAuth) Verify(_ *http.Client, rs *http.Response, path string) (bool, error) {
	if rs.StatusCode != http.StatusUnauthorized {
		return false, nil
	}
	if params, ok := digestChallengeParams(rs.Header.Get("Www-Authenticate")); ok && strings.EqualFold(params["stale"], "true") {
		return true, gowebdav.ErrAuthChanged
	}
	return false, gowebdav.NewPathError("Authorize", path, rs.StatusCode)
}

// Clone shares the session, so the nonce count runs across every request.
func (d *digestAuth) Clone() gowebdav.Authenticator {
	return &digestAuth{user: d.user, session: d.session}
}

func (d *digestAuth) Close() error { return nil }

func (d *digestAuth) String() string { return "DigestAuth login: " + d.user }

// digestChallengeParams returns the parameters of the Digest challenge in a
// WWW-Authenticate value, names lower-cased and quoted strings unquoted, and
// whether there was one. Another scheme's challenge in the same value, before
// or after it, is ignored.
func digestChallengeParams(header string) (map[string]string, bool) {
	var params map[string]string
	rest := header
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t,")
		name := rest[:tokenEnd(rest)]
		if name == "" {
			break
		}
		rest = strings.TrimLeft(rest[len(name):], " \t")
		if !strings.HasPrefix(rest, "=") {
			// A bare token starts a challenge: the scheme.
			if params != nil {
				break // the next scheme's challenge
			}
			if strings.EqualFold(name, "Digest") {
				params = map[string]string{}
			}
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t")
		var value string
		if strings.HasPrefix(rest, `"`) {
			value, rest = unquote(rest)
		} else {
			end := strings.IndexAny(rest, ", \t")
			if end < 0 {
				end = len(rest)
			}
			value, rest = rest[:end], rest[end:]
		}
		if params != nil {
			params[strings.ToLower(name)] = value
		}
	}
	return params, params != nil
}

// tokenEnd is the length of the token s starts with (RFC 9110 5.6.2).
func tokenEnd(s string) int {
	for i, r := range s {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`"(),/:;<=>?@[\]{}`, r) {
			return i
		}
	}
	return len(s)
}

// unquote reads the quoted-string s starts with and returns its value and
// what follows it.
func unquote(s string) (string, string) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		case '"':
			return b.String(), s[i+1:]
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), ""
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // HTTP Digest's MD5, the algorithm the server chose.
	return hex.EncodeToString(sum[:])
}
