package webdav

// A mount on an IPv6 literal authority keeps its brackets (#116).
//
// gowebdav v0.12.0 builds every request URL, and a MOVE's Destination, as
// PathEscape(Join(root, path)), and PathEscape escapes each "/"-separated
// segment of the whole URL, the authority among them. The brackets of an IPv6
// literal become %5B and %5D -- http://[::1]:8080/ is sent as
// http://%5B::1%5D:8080/ -- which net/url refuses to parse, so a mount on one
// failed before its first request left.
//
// So gowebdav is given a root URL whose host is a name with nothing to escape,
// standInHost, keeping the scheme, port and path, and the mount's outermost
// transport puts the real authority back in the request URL, the Host header
// and a Destination before anything else sees the request. The driver's own
// URLs -- an If header's tagged lists, a HEAD -- are built the same way and
// restored the same way, so they name a resource exactly as the request does.
// A mount on any other authority is given its own URL, and nothing changes.

import (
	"net/http"
	"strings"
)

// standInHost replaces an IPv6 literal in the root URL gowebdav is given. The
// .invalid top-level domain (RFC 6761) never resolves, so a request that
// somehow left with it could not reach any host.
const standInHost = "ipv6-literal.invalid"

// authority maps the stand-in authority back to the real one.
type authority struct {
	// standIn and real are URL host forms, port included: the one gowebdav
	// is given and the one the server is at. They are equal when the real
	// one needs no stand-in.
	standIn, real string
}

// standInRoot returns the root URL to give gowebdav for root, the URL
// clientRoot built, and the authority that maps it back. A root whose host is
// not an IPv6 literal is returned as it is. root is taken apart as text, not
// parsed again: its path is already unescaped (#117), so it need not parse.
func standInRoot(root string) (string, authority) {
	prefix, host, rest, ok := splitHost(root)
	if !ok || !strings.HasPrefix(host, "[") {
		return root, authority{}
	}
	standIn := standInHost
	if end := strings.LastIndex(host, "]"); end >= 0 && strings.HasPrefix(host[end+1:], ":") {
		standIn += host[end+1:]
	}
	return prefix + standIn + rest, authority{standIn: standIn, real: host}
}

// splitHost splits a URL into what comes before its host (the scheme and any
// user information), its host with any port, and what follows it. User
// information as net/url writes it holds no "/" or "@", so the host runs from
// the last "@" of the authority, or its start, to the first "/".
func splitHost(raw string) (prefix, host, rest string, ok bool) {
	scheme := strings.Index(raw, "://")
	if scheme < 0 {
		return "", "", "", false
	}
	start := scheme + len("://")
	end := len(raw)
	if slash := strings.IndexByte(raw[start:], '/'); slash >= 0 {
		end = start + slash
	}
	if at := strings.LastIndexByte(raw[start:end], '@'); at >= 0 {
		start += at + 1
	}
	return raw[:start], raw[start:end], raw[end:], true
}

// restore returns a URL gowebdav built with its authority put back.
func (a authority) restore(raw string) string {
	if a.standIn == a.real {
		return raw
	}
	prefix, host, rest, ok := splitHost(raw)
	if !ok || host != a.standIn {
		return raw
	}
	return prefix + a.real + rest
}

// restoredAuthority is the transport that puts the real authority back in
// every request gowebdav sends. See the file comment.
type restoredAuthority struct {
	base      http.RoundTripper
	authority authority
}

func (r *restoredAuthority) RoundTrip(request *http.Request) (*http.Response, error) {
	a := r.authority
	if a.standIn == a.real {
		return r.base.RoundTrip(request)
	}
	destination := request.Header.Get("Destination")
	restored := a.restore(destination)
	if request.URL.Host != a.standIn && request.Host != a.standIn && restored == destination {
		return r.base.RoundTrip(request)
	}
	// A RoundTripper must not change the request it was given.
	request = request.Clone(request.Context())
	if request.URL.Host == a.standIn {
		request.URL.Host = a.real
	}
	if request.Host == a.standIn {
		request.Host = a.real
	}
	if restored != destination {
		request.Header.Set("Destination", restored)
	}
	return r.base.RoundTrip(request)
}
