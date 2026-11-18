//go:build webdav_integration

// HTTP Digest auth with algorithm=SHA-256 against a server that is not ours
// (#108): lighttpd's mod_webdav behind its own mod_auth, the container
// scripts/servers.sh starts as `lighttpd-sha256` (.github/docker/lighttpd-sha256).
// lighttpd recomputes a SHA-256 answer from the password and checks its uri,
// nc, cnonce and qop, so a client that miscomputes it cannot pass.
//
// The container has two listeners. One offers SHA-256 alone. The other offers
// SHA-256 and then MD5, as two WWW-Authenticate headers in order of preference
// (RFC 7616 3.7); lighttpd takes an answer to either, so that test also
// records which one the driver chose.
//
// Run with `chore test:webdav`, or by hand with the server up:
//
//	WEBDAV_LIGHTTPD_SHA256_URL=http://127.0.0.1:8086 WEBDAV_LIGHTTPD_SHA256_MD5_URL=http://127.0.0.1:8087 \
//	  WEBDAV_USER=testuser WEBDAV_PASS=testpass \
//	  go test -tags=webdav_integration -run IntegrationDigestSHA256 ./webdav/...

package webdav

import (
	"crypto/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var (
	lighttpdSHA256Server    = integrationServer{name: "lighttpd-sha256", urlVar: "WEBDAV_LIGHTTPD_SHA256_URL"}
	lighttpdSHA256MD5Server = integrationServer{name: "lighttpd-sha256 (SHA-256|MD5)", urlVar: "WEBDAV_LIGHTTPD_SHA256_MD5_URL"}
)

func TestIntegrationDigestSHA256WriteStatRemove(t *testing.T) {
	exerciseDigestAlgorithm(t, lighttpdSHA256Server)
}

// The server prefers SHA-256 and also takes MD5: the driver answers SHA-256,
// the first it is offered.
func TestIntegrationDigestSHA256OrMD5AnswersSHA256(t *testing.T) {
	algorithms := exerciseDigestAlgorithm(t, lighttpdSHA256MD5Server)
	if len(algorithms) != 1 || !strings.EqualFold(algorithms[0], "SHA-256") {
		t.Errorf("answered with %v, want SHA-256 alone, the server's first challenge", algorithms)
	}
}

// exerciseDigestAlgorithm writes, stats and removes a file through a mount of
// server, and returns every algorithm the mount's answers named after the
// mount itself, in the order first seen.
func exerciseDigestAlgorithm(t *testing.T, server integrationServer) []string {
	t.Helper()
	cfg := integrationConfig(t, server)
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	t.Cleanup(func() { _ = d.Unmount(1) })
	recorder := &digestAlgorithmRecorder{base: d.transport}
	d.client.SetTransport(recorder)

	dir := "/sha256-" + strings.ToLower(rand.Text()[:10])
	if err := d.Mkdir(1, dir); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = d.Remove(1, dir) })

	path := dir + "/digest.txt"
	body := []byte("answered with SHA-256")
	save(t, d, 1, path, body)
	if info := stat(t, d, 1, path); info.Size != int64(len(body)) || info.IsDir {
		t.Fatalf("stat %s: size %d, dir %v; want a %d-byte file", path, info.Size, info.IsDir, len(body))
	}
	if got := contents(t, d, 1, path); string(got) != string(body) {
		t.Fatalf("read %s: %q, want %q", path, got, body)
	}
	if err := d.Remove(1, path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	absent(t, d, 1, path)
	return recorder.named()
}

// digestAlgorithmRecorder notes the algorithm each Digest answer names on its
// way out.
type digestAlgorithmRecorder struct {
	base http.RoundTripper
	mu   sync.Mutex
	seen []string
}

var answeredAlgorithm = regexp.MustCompile(`(?i)[\s,]algorithm="?([A-Za-z0-9-]+)`)

func (r *digestAlgorithmRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	authorization := request.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authorization), "digest ") {
		algorithm := "MD5 (unnamed)"
		if m := answeredAlgorithm.FindStringSubmatch(authorization); m != nil {
			algorithm = m[1]
		}
		r.mu.Lock()
		if !containsFold(r.seen, algorithm) {
			r.seen = append(r.seen, algorithm)
		}
		r.mu.Unlock()
	}
	return r.base.RoundTrip(request)
}

func (r *digestAlgorithmRecorder) named() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}
