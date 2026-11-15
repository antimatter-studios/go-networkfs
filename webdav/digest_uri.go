package webdav

// A Digest response signed for the request URI actually sent (#99).
//
// A Digest response hashes the request URI (RFC 7616, section 3.4.1), and the
// uri parameter that carries it must name the request-target of the request
// line (section 3.4.6). gowebdav v0.12.0 fills uri with the path it was asked
// for, relative to the client's root URL, not the URI it sends. A mount whose
// url holds a path -- https://host/remote.php/dav/files/user, say -- sends
// "PROPFIND /remote.php/dav/files/user/dir/" signed for "/dir/", and a server
// that checks the two refuses every request: Apache answers 400 Bad Request,
// so gowebdav never sees a challenge it could answer. A name that needs
// percent-encoding differs the same way, root path or not.
//
// So the transport, at the wire, signs a Digest authorization again for the
// request URI it is about to send whenever the two differ. The driver's own
// authenticator signs for the request URI already (#107), so what still
// differs is a request re-sent to another URI: a collection's
// slash-terminated URL, after the server's redirect (#95). An answer of the
// mount's latest Digest session is answered afresh under that session, with
// its next nonce count (digestSessions). Any other is signed again with the
// nonce, client nonce and nonce count it carries; only uri and response
// change, and for MD5-sess the session key is computed from that nonce and
// client nonce. Basic credentials, other algorithms and other qualities of
// protection go out as they came.

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
)

// digestRequestURI signs every Digest authorization for the request URI the
// request carries. See the file comment.
type digestRequestURI struct {
	// base is the transport the request goes out on; nil means
	// http.DefaultTransport.
	base http.RoundTripper
	// password computes the response afresh.
	password string
	// sessions answers a request afresh under the Digest session it was
	// answered with, when that session is the mount's latest; nil means none.
	sessions *digestSessions
}

func (d *digestRequestURI) RoundTrip(request *http.Request) (*http.Response, error) {
	base := d.base
	if base == nil {
		base = http.DefaultTransport
	}
	authorization, uri := request.Header.Get("Authorization"), request.URL.RequestURI()
	signed, ok := authorization, false
	if d.sessions != nil {
		signed, ok = d.sessions.resign(authorization, request.Method, uri)
	}
	if !ok {
		signed = signDigestFor(authorization, request.Method, uri, d.password)
	}
	if signed != authorization {
		// A RoundTripper must not change the request it was given.
		request = request.Clone(request.Context())
		request.Header.Set("Authorization", signed)
	}
	return base.RoundTrip(request)
}

// signDigestFor returns a Digest authorization like the one given, signed for
// uri: its uri parameter replaced and its response computed for that uri and
// method under password (RFC 7616, section 3.4.1). An authorization already
// signed for uri, another scheme, or an algorithm or quality of protection
// this does not compute comes back as it was.
func signDigestFor(authorization, method, uri, password string) string {
	scheme, rest, _ := strings.Cut(authorization, " ")
	if !strings.EqualFold(scheme, "Digest") {
		return authorization
	}
	params := digestAuthParams(rest)
	value := map[string]string{}
	for _, p := range params {
		value[p.key] = p.value
	}
	if value["uri"] == uri {
		return authorization
	}
	ha1 := digestMD5(value["username"] + ":" + value["realm"] + ":" + password)
	switch a := value["algorithm"]; {
	case a == "" || strings.EqualFold(a, "MD5"):
	case strings.EqualFold(a, "MD5-sess"):
		// The session key, from the nonce and client nonce the answer
		// already carries (RFC 7616 3.4.2).
		if value["cnonce"] == "" {
			return authorization
		}
		ha1 = digestMD5(ha1 + ":" + value["nonce"] + ":" + value["cnonce"])
	default:
		return authorization
	}
	ha2 := digestMD5(method + ":" + uri)
	var response string
	switch value["qop"] {
	case "":
		response = digestMD5(ha1 + ":" + value["nonce"] + ":" + ha2)
	case "auth":
		response = digestMD5(ha1 + ":" + value["nonce"] + ":" + value["nc"] + ":" +
			value["cnonce"] + ":auth:" + ha2)
	default:
		return authorization
	}

	var out strings.Builder
	out.WriteString(scheme)
	for i, p := range params {
		switch p.key {
		case "uri":
			p.value = uri
		case "response":
			p.value = response
		}
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(" " + p.name + "=")
		if p.quoted {
			out.WriteString(`"` + digestQuote.Replace(p.value) + `"`)
		} else {
			out.WriteString(p.value)
		}
	}
	return out.String()
}

func digestMD5(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // HTTP Digest auth's MD5 algorithm, not a choice made here.
	return hex.EncodeToString(sum[:])
}

// digestQuote escapes a value for an HTTP quoted-string (RFC 9110, section
// 5.6.4), the inverse of what digestAuthParams undoes.
var digestQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// digestAuthParam is one parameter of a Digest authorization, in the order
// sent: its name as written, its key lower-cased, and its value unquoted.
type digestAuthParam struct {
	name, key, value string
	quoted           bool
}

// digestAuthParams splits the comma-separated parameters of a Digest
// authorization. A quoted value may hold commas and backslash escapes.
func digestAuthParams(s string) []digestAuthParam {
	var params []digestAuthParam
	for {
		s = strings.TrimLeft(s, " \t,")
		if s == "" {
			return params
		}
		name, rest, ok := strings.Cut(s, "=")
		if !ok {
			return params
		}
		name = strings.TrimSpace(name)
		p := digestAuthParam{name: name, key: strings.ToLower(name)}
		rest = strings.TrimLeft(rest, " \t")
		if strings.HasPrefix(rest, `"`) {
			p.quoted = true
			var value strings.Builder
			i := 1
			for ; i < len(rest) && rest[i] != '"'; i++ {
				if rest[i] == '\\' && i+1 < len(rest) {
					i++
				}
				value.WriteByte(rest[i])
			}
			p.value = value.String()
			s = rest[min(i+1, len(rest)):]
		} else {
			value, after, _ := strings.Cut(rest, ",")
			p.value = strings.TrimSpace(value)
			s = after
		}
		params = append(params, p)
	}
}
