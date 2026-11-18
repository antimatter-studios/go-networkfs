package webdav

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/studio-b12/gowebdav"
)

func TestDigestChallengeParamsReadsQuotedListsAndOtherSchemes(t *testing.T) {
	header := `Basic realm="basic, realm", Digest realm="dav, the realm", qop="auth,auth-int", ` +
		`algorithm=MD5-sess, nonce="n\"1", stale=TRUE, Bearer realm="x"`
	params, ok := digestChallengeParams(header)
	if !ok {
		t.Fatalf("no Digest challenge found in %q", header)
	}
	want := map[string]string{
		"realm": "dav, the realm", "qop": "auth,auth-int", "algorithm": "MD5-sess",
		"nonce": `n"1`, "stale": "TRUE",
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("%s = %q, want %q", k, params[k], v)
		}
	}
	if len(params) != len(want) {
		t.Errorf("params %v hold another scheme's parameters", params)
	}
	if _, ok := digestChallengeParams(`Basic realm="digest"`); ok {
		t.Error("a Basic challenge whose realm says digest was read as Digest")
	}
}

func TestDigestAnswersAuthFromAQopList(t *testing.T) {
	auth, err := newDigestAuth("u", "p", `Digest realm="r", nonce="n", qop="auth-int, auth"`)
	if err != nil {
		t.Fatal(err)
	}
	rq, _ := http.NewRequest("GET", "http://host/f", nil)
	if err := auth.Authorize(nil, rq, "/f"); err != nil {
		t.Fatal(err)
	}
	got := rq.Header.Get("Authorization")
	if !strings.Contains(got, "qop=auth,") || strings.Contains(got, "algorithm=") {
		t.Errorf("Authorization %q: want qop=auth and no algorithm, the challenge named none", got)
	}
}

func TestDigestRefusesWhatItCannotAnswer(t *testing.T) {
	for _, challenge := range []string{
		`Digest realm="r", nonce="n", algorithm=SHA-256, qop="auth"`,
		`Digest realm="r", nonce="n", qop="auth-int"`,
		`Digest realm="r", qop="auth"`,
		`Basic realm="r"`,
	} {
		if _, err := newDigestAuth("u", "p", challenge); err == nil {
			t.Errorf("%s: answered, want refused so another scheme can be tried", challenge)
		}
	}
}

// The nonce count is shared by every clone, since gowebdav clones the
// authenticator for each request.
func TestDigestClonesShareTheNonceCount(t *testing.T) {
	auth, err := newDigestAuth("u", "p", `Digest realm="r", nonce="n", qop="auth", algorithm=MD5`)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"nc=00000001", "nc=00000002", "nc=00000003"} {
		rq, _ := http.NewRequest("GET", "http://host/", nil)
		if err := auth.Clone().Authorize(nil, rq, "/"); err != nil {
			t.Fatal(err)
		}
		if got := rq.Header.Get("Authorization"); !strings.Contains(got, want) {
			t.Errorf("request %d: %q, want %s", i+1, got, want)
		}
	}
}

func TestDigestVerifyRenegotiatesOnlyAStaleNonce(t *testing.T) {
	auth, err := newDigestAuth("u", "p", `Digest realm="r", nonce="n", qop="auth"`)
	if err != nil {
		t.Fatal(err)
	}
	response := func(challenge string) *http.Response {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Www-Authenticate": {challenge}}}
	}
	redo, err := auth.Verify(nil, response(`Digest realm="r", nonce="m", qop="auth", stale=true`), "/")
	if !redo || !errors.Is(err, gowebdav.ErrAuthChanged) {
		t.Errorf("stale nonce: redo %v, err %v; want a fresh challenge asked for", redo, err)
	}
	redo, err = auth.Verify(nil, response(`Digest realm="r", nonce="m", qop="auth"`), "/")
	if redo || err == nil || errors.Is(err, gowebdav.ErrAuthChanged) {
		t.Errorf("refused answer: redo %v, err %v; want a final error", redo, err)
	}
	if redo, err := auth.Verify(nil, &http.Response{StatusCode: http.StatusOK}, "/"); redo || err != nil {
		t.Errorf("200: redo %v, err %v", redo, err)
	}
}
