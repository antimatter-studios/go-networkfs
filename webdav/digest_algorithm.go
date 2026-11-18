package webdav

// The hash algorithms of HTTP Digest auth, as RFC 7616 section 3.2 names
// them, and the arithmetic of a response under each (#108).
//
// Everything that computes a Digest response goes through digestAlgorithm:
// the authenticator that answers a challenge (digest_auth.go), and the
// transports that sign an answer again for another URI (digest_uri.go,
// redirect.go). One table, so a server that offers an algorithm is answered
// with it, or refused, the same way at every point.

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"strings"
)

// digestAlgorithm is one algorithm a Digest challenge can name.
type digestAlgorithm struct {
	// name is RFC 7616's spelling, which an answer echoes.
	name string
	// newHash is H.
	newHash func() hash.Hash
	// sess: the -sess variant, whose A1 is a session key over the server's
	// nonce and the client's cnonce (RFC 7616 3.4.2).
	sess bool
}

// digestAlgorithmNamed returns the algorithm a challenge or an answer names,
// and whether it is one this driver computes. No name at all means MD5 (RFC
// 7616 3.3). Names are matched without regard to case.
func digestAlgorithmNamed(name string) (digestAlgorithm, bool) {
	switch strings.ToLower(name) {
	case "", "md5":
		return digestAlgorithm{name: "MD5", newHash: md5.New}, true
	case "md5-sess":
		return digestAlgorithm{name: "MD5-sess", newHash: md5.New, sess: true}, true
	case "sha-256":
		return digestAlgorithm{name: "SHA-256", newHash: sha256.New}, true
	case "sha-256-sess":
		return digestAlgorithm{name: "SHA-256-sess", newHash: sha256.New, sess: true}, true
	case "sha-512-256":
		// SHA-512/256 of FIPS 180-4: its own initial values, not SHA-512 cut
		// short (RFC 7616 3.2).
		return digestAlgorithm{name: "SHA-512-256", newHash: sha512.New512_256}, true
	case "sha-512-256-sess":
		return digestAlgorithm{name: "SHA-512-256-sess", newHash: sha512.New512_256, sess: true}, true
	}
	return digestAlgorithm{}, false
}

// h is H(s), in lower-case hex.
func (a digestAlgorithm) h(s string) string {
	sum := a.newHash()
	sum.Write([]byte(s))
	return hex.EncodeToString(sum.Sum(nil))
}

// ha1 is H(A1) (RFC 7616 3.4.2): H(user:realm:password), and for a -sess
// algorithm that hashed again with the server's nonce and the client's cnonce.
func (a digestAlgorithm) ha1(user, realm, password, nonce, cnonce string) string {
	ha1 := a.h(user + ":" + realm + ":" + password)
	if a.sess {
		ha1 = a.h(ha1 + ":" + nonce + ":" + cnonce)
	}
	return ha1
}

// response is the request-digest for method and uri under ha1 (RFC 7616
// 3.4.1). qop "" is the form without one, RFC 2069's, which has no nc or
// cnonce; otherwise qop is "auth", the only quality of protection answered.
func (a digestAlgorithm) response(ha1, method, uri, nonce, nc, cnonce, qop string) string {
	ha2 := a.h(method + ":" + uri)
	if qop == "" {
		return a.h(ha1 + ":" + nonce + ":" + ha2)
	}
	return a.h(strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":"))
}
