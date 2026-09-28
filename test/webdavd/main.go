// Command webdavd is the WebDAV server the webdav driver's tests run against.
//
// WHY IT IS HERE AND NOT PULLED. The image this replaced, bytemark/webdav, is
// published for linux/amd64 alone, so on an arm64 host it exited with
// "exec format error" and the suite that needs it could not run at all — and
// the consumer of these archives is a macOS application, so an Apple Silicon
// workstation is exactly where a developer runs this (issues #16 and #20).
// Built from source it is multi-arch by construction, it is pinned by this
// repository's own commit rather than by somebody else's :latest, and it
// cannot drift from what the driver expects.
//
// It is golang.org/x/net/webdav's handler and nothing else: that package
// already implements PROPFIND, PROPPATCH, MKCOL, COPY, MOVE, LOCK and the
// ordinary GET/PUT/DELETE, which is the whole of what webdav/webdav.go calls
// through gowebdav. The only thing wrapped around it is HTTP Basic auth,
// because the driver sends credentials and a server that ignored them would
// let a broken credential path pass.
//
// THIS IS A TEST SERVER. It stores everything under one directory, has one
// account, speaks plain HTTP and compares the password without hashing it. It
// is started by scripts/servers.sh, listens inside a throwaway container and
// is never shipped.
package main

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/webdav"
)

func main() {
	addr := env("WEBDAVD_ADDR", ":80")
	root := env("WEBDAVD_ROOT", "/data")
	user := env("WEBDAVD_USER", "testuser")
	pass := env("WEBDAVD_PASS", "testpass")

	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatalf("webdavd: %s: %v", root, err)
	}

	handler := &webdav.Handler{
		FileSystem: webdav.Dir(root),
		LockSystem: webdav.NewMemLS(),
		// A failing WebDAV method is the hardest thing to diagnose from the
		// driver's side, because gowebdav reports the status and not what the
		// server made of the request. Logging every one of them means the
		// container's log answers "did it even arrive".
		Logger: func(r *http.Request, err error) {
			if err != nil {
				log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
			}
		},
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: basicAuth(user, pass, handler),
	}

	log.Printf("webdavd: serving %s on %s as %s", root, addr, user)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("webdavd: %v", err)
	}
}

// basicAuth refuses every request that does not carry the one account's
// credentials. subtle.ConstantTimeCompare rather than == is habit rather than
// a threat model here: there is nothing behind this server worth timing.
func basicAuth(user, pass string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="webdavd"`)
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
