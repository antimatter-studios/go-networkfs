//go:build webdav_integration

// A Digest mount reached through a forward HTTP proxy (#110): Squid, the
// `squid` fixture (.github/docker/squid, WEBDAV_PROXY_URL), in front of
// Apache's mod_auth_digest (`webdav`, plain MD5) and lighttpd's mod_auth
// (`lighttpd-sess`, MD5-sess).
//
// Go sends a plain http:// request through a proxy with the absolute-form
// request-target, "PUT http://webdav:81/dir/file HTTP/1.1", and the driver
// signs each Digest answer for the origin form, "/dir/file". RFC 7616 3.4.6
// names the request-target as the uri parameter, so the two look as if they
// disagree. They do not where it matters: the proxy forwards the request to
// the origin server in origin form (RFC 9112 3.2.1), and the origin server is
// the one that checks the answer, against the request line it received.
// Signing for the absolute form instead would break a server that compares
// the two literally: lighttpd 1.4.85 answers such a request 400 through this
// proxy, so the lighttpd-sess half of this test fails if the driver ever
// signs for the URL the proxy saw rather than the URI the server sees.
//
// The proxy is the one a user configures, HTTP_PROXY, which net/http reads
// once per process. So the test runs itself again in a process of its own
// with HTTP_PROXY set, and that process does the work. The servers are named
// by their aliases on the test network, which only the proxy can resolve from
// the host, and every answer is checked to have come through it.

package webdav

import (
	"crypto/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// proxiedChildVar marks the process that runs the proxied half of the test.
const proxiedChildVar = "GO_NETWORKFS_WEBDAV_PROXIED_CHILD"

// proxiedServers are the Digest servers reached through the proxy, by the
// variables that hold their addresses on the test network.
var proxiedServers = []struct{ name, urlVar string }{
	{"apache-md5", "WEBDAV_PROXIED_DIGEST_URL"},
	{"lighttpd-md5-sess", "WEBDAV_PROXIED_LIGHTTPD_SESS_URL"},
}

func TestIntegrationDigestThroughForwardProxy(t *testing.T) {
	vars := []string{"WEBDAV_PROXY_URL", "WEBDAV_USER", "WEBDAV_PASS"}
	for _, s := range proxiedServers {
		vars = append(vars, s.urlVar)
	}
	var missing []string
	for _, v := range vars {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		// A FAILURE, NOT A SKIP: see integrationConfig.
		t.Fatalf("WebDAV Digest through a proxy cannot run: %s unset. Run `chore test:webdav` "+
			"(or `chore test`), which starts the servers and the squid proxy and exports them.",
			strings.Join(missing, ", "))
	}
	if os.Getenv(proxiedChildVar) == "" {
		runProxiedChild(t)
		return
	}

	for _, server := range proxiedServers {
		t.Run(server.name, func(t *testing.T) {
			root := os.Getenv(server.urlVar)
			requireThroughProxy(t, root)
			digestMountThroughProxy(t, root)
		})
	}
}

// runProxiedChild runs this test again in a process whose environment sends
// every plain HTTP request through the proxy, and fails with its output if
// that process fails.
func runProxiedChild(t *testing.T) {
	t.Helper()
	env := []string{proxiedChildVar + "=1", "HTTP_PROXY=" + os.Getenv("WEBDAV_PROXY_URL")}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		// Whatever proxy settings the caller has would decide the route
		// instead; REQUEST_METHOD makes net/http ignore HTTP_PROXY (CGI).
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD", proxiedChildVar:
			continue
		}
		env = append(env, kv)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestIntegrationDigestThroughForwardProxy$", "-test.count=1")
	child.Env = env
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the proxied run failed (%v):\n%s", err, out)
	}
}

// requireThroughProxy fails unless a request to root from this process is
// answered through the squid fixture, which names itself in Via.
func requireThroughProxy(t *testing.T, root string) {
	t.Helper()
	rs, err := http.Get(root)
	if err != nil {
		t.Fatalf("GET %s through %s: %v", root, os.Getenv("WEBDAV_PROXY_URL"), err)
	}
	_ = rs.Body.Close()
	if via := rs.Header.Get("Via"); !strings.Contains(via, "go-networkfs-squid") {
		t.Fatalf("GET %s was not answered through the squid proxy: Via %q, status %d", root, via, rs.StatusCode)
	}
}

// digestMountThroughProxy writes, reads, stats, renames and removes through a
// mount of root and through a mount whose url holds a path under it (#99),
// including a directory named without its trailing slash, which the server
// redirects (#95).
func digestMountThroughProxy(t *testing.T, root string) {
	cfg := map[string]string{"url": root, "user": os.Getenv("WEBDAV_USER"), "pass": os.Getenv("WEBDAV_PASS")}
	top := &WebDAVDriver{}
	if err := top.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", root, err)
	}
	dir := "/proxied-" + strings.ToLower(rand.Text()[:10])
	if err := top.Mkdir(1, dir+"/"); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = top.Remove(1, dir+"/")
		_ = top.Unmount(1)
	})

	save(t, top, 1, dir+"/top.txt", []byte("through the proxy"))
	if info := stat(t, top, 1, dir+"/top.txt"); info.Size != int64(len("through the proxy")) || info.IsDir {
		t.Errorf("stat %s/top.txt: size %d, dir %v", dir, info.Size, info.IsDir)
	}

	cfg["url"] = strings.TrimSuffix(root, "/") + dir
	d := &WebDAVDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("mount %s: %v", cfg["url"], err)
	}
	t.Cleanup(func() { _ = d.Unmount(1) })

	if err := d.Mkdir(1, "/from/"); err != nil {
		t.Fatalf("mkdir /from under %s: %v", cfg["url"], err)
	}
	for _, name := range []string{"/from/file.txt", "/from/with space.txt"} {
		save(t, d, 1, name, []byte("proxied"))
		if info := stat(t, d, 1, name); info.Size != int64(len("proxied")) || info.IsDir {
			t.Errorf("stat %s: size %d, dir %v", name, info.Size, info.IsDir)
		}
		if got := contents(t, d, 1, name); string(got) != "proxied" {
			t.Errorf("read %s: %q", name, got)
		}
	}
	if err := d.Remove(1, "/from/with space.txt"); err != nil {
		t.Fatalf("remove /from/with space.txt: %v", err)
	}
	absent(t, d, 1, "/from/with space.txt")

	// Named without the trailing slash, so the server redirects each request.
	info, err := d.Stat(1, "/from")
	if err != nil {
		t.Fatalf("stat /from: %v", err)
	}
	if !info.IsDir {
		t.Fatalf("stat /from reported a file")
	}
	if err := d.Rename(1, "/from", "/to"); err != nil {
		t.Fatalf("rename /from to /to: %v", err)
	}
	absent(t, d, 1, "/from")
	if got := contents(t, top, 1, dir+"/to/file.txt"); string(got) != "proxied" {
		t.Errorf("the root mount read %q after the rename", got)
	}
	if err := d.Remove(1, "/to"); err != nil {
		t.Fatalf("remove /to: %v", err)
	}
	absent(t, d, 1, "/to")
}
