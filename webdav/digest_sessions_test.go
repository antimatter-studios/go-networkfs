package webdav

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// The authenticator signs for the request URI the request goes out with, not
// the path gowebdav hands it (#107).
func TestDigestAuthorizeSignsTheRequestURI(t *testing.T) {
	auth, err := newDigestAuth("u", "p", `Digest realm="r", nonce="n", algorithm=MD5-sess, qop="auth"`)
	if err != nil {
		t.Fatal(err)
	}
	rq, _ := http.NewRequest("PROPFIND", "http://host/root/a%20b", nil)
	if err := auth.Authorize(nil, rq, "/a b"); err != nil {
		t.Fatal(err)
	}
	if got := rq.Header.Get("Authorization"); !strings.Contains(got, `uri="/root/a%20b"`) {
		t.Errorf("Authorization %q: want uri=\"/root/a%%20b\"", got)
	}
}

// A request re-sent to another URI is answered under the session it was
// answered with, with that session's next nonce count, not the one already
// sent; an answer already signed for its URI, or under another session, is
// not the latest session's to answer.
func TestDigestSessionsResignTakesTheNextNonceCount(t *testing.T) {
	sessions := &digestSessions{}
	authorizer := func(challenge string) *digestAuth {
		auth, err := newDigestAuth("u", "p", challenge)
		if err != nil {
			t.Fatal(err)
		}
		return auth.(*digestAuth)
	}
	older := authorizer(`Digest realm="r", nonce="n", algorithm=MD5-sess, qop="auth"`)
	latest := authorizer(`Digest realm="r", nonce="n", algorithm=MD5-sess, qop="auth"`)
	sessions.remember(latest)

	first := latest.answer("MOVE", "/dir")
	got, ok := sessions.resign(first, "MOVE", "/dir/")
	if !ok || !strings.Contains(got, `uri="/dir/"`) || !strings.Contains(got, "nc=00000002") {
		t.Errorf("re-sent to /dir/: %q, %v; want uri=\"/dir/\" and nc=00000002", got, ok)
	}
	if got, ok := sessions.resign(first, "MOVE", "/dir"); ok || got != first {
		t.Errorf("already signed for /dir: %q, %v; want it unchanged", got, ok)
	}
	if _, ok := sessions.resign(older.answer("MOVE", "/dir"), "MOVE", "/dir/"); ok {
		t.Error("an answer under another client nonce was answered under the latest session")
	}
	if _, ok := sessions.resign("Basic dTpw", "MOVE", "/dir/"); ok {
		t.Error("Basic credentials were answered as Digest")
	}
}

// An MD5-sess answer the mount no longer holds the session of is signed again
// from what it carries: the session key from its nonce and client nonce, its
// nonce count unchanged (RFC 7616 3.4.2). The expected response is computed
// here from the RFC's definitions.
func TestSignDigestForMD5Sess(t *testing.T) {
	hash := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	const authorization = `Digest username="u", realm="r", nonce="n", uri="/x", algorithm=MD5-sess, ` +
		`qop=auth, nc=00000007, cnonce="c", response="00000000000000000000000000000000"`
	session := hash(hash("u:r:p") + ":n:c")
	response := hash(session + ":n:00000007:c:auth:" + hash("GET:/dir/x"))
	want := `Digest username="u", realm="r", nonce="n", uri="/dir/x", algorithm=MD5-sess, ` +
		`qop=auth, nc=00000007, cnonce="c", response="` + response + `"`
	if got := signDigestFor(authorization, http.MethodGet, "/dir/x", "p"); got != want {
		t.Errorf("signed for /dir/x:\n got %s\nwant %s", got, want)
	}
	// Without a client nonce there is no session key to compute.
	without := `Digest username="u", realm="r", nonce="n", uri="/x", algorithm=MD5-sess, response="0"`
	if got := signDigestFor(without, http.MethodGet, "/dir/x", "p"); got != without {
		t.Errorf("an MD5-sess answer without cnonce came back as %q", got)
	}
}
