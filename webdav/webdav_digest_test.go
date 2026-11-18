package webdav

// HTTP Digest answers as RFC 7616 defines them (#100), checked by an
// in-process server. This is our own reading of the RFC, so it proves the
// header's shape and the arithmetic we expect; that the arithmetic is right is
// proved against lighttpd, which shares no code with us
// (webdav_digest_sess_integration_test.go).

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/webdav"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
)

const (
	digestTestRealm = "networkfs-digest"
	digestTestUser  = "digestuser"
	digestTestPass  = "digest password"
)

// ncValue is RFC 7616's nc-value: exactly eight hexadecimal digits.
var ncValue = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

// rfc7616Server is a WebDAV server behind Digest auth that offers one
// algorithm with qop=auth, and checks every answer to it. It never refuses an
// answer for being wrong in a way the driver could recover from by asking
// again: a wrong answer is recorded and refused 401 without stale=true, so the
// operation fails and the test reports why.
type rfc7616Server struct {
	algorithm string // as offered in the challenge
	nonce     string
	handler   http.Handler

	mu       sync.Mutex
	answers  []map[string]string
	problems []string
	lastNC   map[string]uint64 // per nonce, the highest nc seen
}

func startRFC7616Server(t *testing.T, algorithm string) (*rfc7616Server, string) {
	t.Helper()
	s := &rfc7616Server{
		algorithm: algorithm,
		nonce:     "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		handler:   &webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()},
		lastNC:    map[string]uint64{},
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *rfc7616Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	if authorization == "" {
		s.challenge(w)
		return
	}
	if problem := s.check(r, authorization); problem != "" {
		s.mu.Lock()
		s.problems = append(s.problems, fmt.Sprintf("%s %s: %s\n    Authorization: %s", r.Method, r.URL.RequestURI(), problem, authorization))
		s.mu.Unlock()
		s.challenge(w)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *rfc7616Server) challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Digest realm="%s", qop="auth", algorithm=%s, nonce="%s", opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
		digestTestRealm, s.algorithm, s.nonce))
	w.WriteHeader(http.StatusUnauthorized)
}

// check returns why an answer is not the one RFC 7616 asks for, or "".
func (s *rfc7616Server) check(r *http.Request, authorization string) string {
	scheme, rest, _ := strings.Cut(authorization, " ")
	if !strings.EqualFold(scheme, "Digest") {
		return "not a Digest answer"
	}
	p := parseTestDigestParams(rest)
	s.mu.Lock()
	s.answers = append(s.answers, p)
	s.mu.Unlock()

	if !strings.EqualFold(p["algorithm"], s.algorithm) {
		return fmt.Sprintf("algorithm is %q, but the challenge offered %s (RFC 7616 3.4: the answer names the algorithm it used)", p["algorithm"], s.algorithm)
	}
	if !ncValue.MatchString(p["nc"]) {
		return fmt.Sprintf("nc is %q, not eight hexadecimal digits (RFC 7616 3.4)", p["nc"])
	}
	nc, _ := strconv.ParseUint(p["nc"], 16, 64)
	s.mu.Lock()
	previous := s.lastNC[p["nonce"]]
	if nc > previous {
		s.lastNC[p["nonce"]] = nc
	}
	s.mu.Unlock()
	if nc <= previous {
		return fmt.Sprintf("nc %08x does not count up from %08x, the last sent with this nonce (RFC 7616 3.4)", nc, previous)
	}
	if p["qop"] != "auth" || p["cnonce"] == "" {
		return fmt.Sprintf("qop is %q and cnonce %q; the challenge offered qop=auth", p["qop"], p["cnonce"])
	}
	if p["nonce"] != s.nonce || p["realm"] != digestTestRealm || p["username"] != digestTestUser {
		return "nonce, realm or username is not the one challenged with"
	}
	if p["uri"] != r.URL.RequestURI() {
		return fmt.Sprintf("uri %q is not the request URI %q", p["uri"], r.URL.RequestURI())
	}
	if want := rfc7616Response(s.algorithm, r.Method, p); p["response"] != want {
		return fmt.Sprintf("response is %s, RFC 7616 gives %s", p["response"], want)
	}
	return ""
}

func (s *rfc7616Server) report(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.answers) == 0 {
		t.Fatal("the server saw no Digest answer at all")
	}
	for _, problem := range s.problems {
		t.Error(problem)
	}
}

// rfc7616Response is RFC 7616 3.4.1's response for qop=auth, with 3.4.2's A1:
// for an algorithm ending in -sess, H(user:realm:password) is hashed again
// with the server's nonce and the client's cnonce.
func rfc7616Response(algorithm, method string, p map[string]string) string {
	h := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) } //nolint:gosec // RFC 7616's MD5.
	ha1 := h(digestTestUser + ":" + digestTestRealm + ":" + digestTestPass)
	if strings.HasSuffix(strings.ToLower(algorithm), "-sess") {
		ha1 = h(ha1 + ":" + p["nonce"] + ":" + p["cnonce"])
	}
	ha2 := h(method + ":" + p["uri"])
	return h(strings.Join([]string{ha1, p["nonce"], p["nc"], p["cnonce"], p["qop"], ha2}, ":"))
}

// parseTestDigestParams splits name=value pairs, a value quoted or not.
func parseTestDigestParams(s string) map[string]string {
	params := map[string]string{}
	for _, m := range regexp.MustCompile(`([A-Za-z0-9_-]+)\s*=\s*("([^"\\]|\\.)*"|[^,\s]*)`).FindAllStringSubmatch(s, -1) {
		params[strings.ToLower(m[1])] = strings.Trim(m[2], `"`)
	}
	return params
}

// exerciseDigestMount writes, stats, reads and lists through a mount behind
// the server, so several requests are answered with one nonce.
func exerciseDigestMount(t *testing.T, url string) {
	t.Helper()
	d := &WebDAVDriver{}
	if err := d.Mount(1, map[string]string{"url": url, "user": digestTestUser, "pass": digestTestPass}); err != nil {
		t.Errorf("mount: %v", err)
		return
	}
	defer func() { _ = d.Unmount(1) }()

	if err := api.WriteFile(d, 1, "/digest.txt", []byte("digest")); err != nil {
		t.Errorf("write: %v", err)
		return
	}
	info, err := d.Stat(1, "/digest.txt")
	if err != nil {
		t.Errorf("stat: %v", err)
		return
	}
	if info.Size != int64(len("digest")) {
		t.Errorf("stat reported %d bytes, want %d", info.Size, len("digest"))
	}
	if _, err := d.ListDir(1, "/"); err != nil {
		t.Errorf("list: %v", err)
	}
}

// TestDigestMD5SessAnswer: a server that offers MD5-sess gets an MD5-sess
// answer -- named as such, with an eight-digit nc that counts each request
// sent with the nonce, and a session key over the server's nonce.
func TestDigestMD5SessAnswer(t *testing.T) {
	server, url := startRFC7616Server(t, "MD5-sess")
	defer server.report(t)
	exerciseDigestMount(t, url)
}

// TestDigestMD5Answer: plain MD5 is answered as before, but names its
// algorithm and carries a well-formed, counting nc.
func TestDigestMD5Answer(t *testing.T) {
	server, url := startRFC7616Server(t, "MD5")
	defer server.report(t)
	exerciseDigestMount(t, url)
}
