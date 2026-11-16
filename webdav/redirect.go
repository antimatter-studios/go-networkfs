package webdav

// A collection named without its trailing slash (#83).
//
// A server may answer a request for a collection named without its trailing
// slash -- "PROPFIND /dir" -- with a redirect to the slash-terminated URL.
// Apache's mod_dir does it for every method unless DirectorySlash is turned
// off. Go's HTTP client follows a 301 or 302 for any method but GET and HEAD
// as a GET, and a 307 or 308 only when it can rewind the body, which gowebdav's
// request bodies never let it. So the PROPFIND, DELETE or MOVE the driver sent
// either reaches the server as a GET or not at all, and the operation fails.
//
// The driver cannot add the slash itself: whether a path names a collection is
// what a Stat would find out, and the Stat is one of the requests redirected.
// So the transport follows exactly that redirect and nothing wider: one whose
// target differs from the request only by a trailing slash on its path, on
// the same scheme and host. The request is re-sent as it was -- method,
// headers, credentials and body -- to the slash-terminated URL. Every other
// response, any other redirect included, goes back to the HTTP client as it
// came.
//
// The credentials re-sent are those authorised for the first URL. Basic
// credentials do not depend on the URL; a digest response does, so a server
// that both redirects collections and demands digest authentication would
// reject the re-sent request.

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// collectionRedirects follows a redirect that only adds a trailing slash,
// keeping the request's method. See the file comment.
type collectionRedirects struct {
	base http.RoundTripper
}

// drainLimit bounds how much of a redirect's body is read so the connection
// can be reused; a longer body is dropped with its connection instead.
const drainLimit = 64 << 10

func (c *collectionRedirects) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		// The HTTP client already follows these with their own method.
		return c.base.RoundTrip(request)
	case http.MethodPut:
		// A PUT's body is a whole file, and a file is never a collection.
		return c.base.RoundTrip(request)
	}

	first, body, err := rewindable(request)
	if err != nil {
		return nil, err
	}
	response, err := c.base.RoundTrip(first)
	if err != nil {
		return nil, err
	}
	target, ok := slashRedirect(first.URL, response)
	if !ok {
		return response, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, drainLimit))
	_ = response.Body.Close()

	second := first.Clone(first.Context())
	second.URL = target
	if second.Body, err = body(); err != nil {
		return nil, err
	}
	return c.base.RoundTrip(second)
}

// rewindable returns the request to send first and a function that returns
// its body afresh for the re-sent request. A body the request cannot rewind
// itself -- gowebdav's PROPFIND XML -- is read into memory; PUT, the one
// method whose body is a file's contents, never comes here.
func rewindable(request *http.Request) (*http.Request, func() (io.ReadCloser, error), error) {
	if request.Body == nil || request.Body == http.NoBody {
		return request, func() (io.ReadCloser, error) { return request.Body, nil }, nil
	}
	if request.GetBody != nil {
		return request, request.GetBody, nil
	}
	data, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	fresh := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	first := request.Clone(request.Context())
	first.Body, _ = fresh()
	first.GetBody = fresh
	first.ContentLength = int64(len(data))
	return first, fresh, nil
}

// slashRedirect reports the target of response when it is a redirect from
// requested to the same URL with a trailing slash added to its path.
func slashRedirect(requested *url.URL, response *http.Response) (*url.URL, bool) {
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return nil, false
	}
	location := response.Header.Get("Location")
	if location == "" || strings.HasSuffix(requested.Path, "/") {
		return nil, false
	}
	target, err := requested.Parse(location)
	if err != nil {
		return nil, false
	}
	if !sameOrigin(requested, target) ||
		target.User != nil ||
		target.RawQuery != requested.RawQuery ||
		target.Path != requested.Path+"/" {
		return nil, false
	}
	return target, true
}

// sameOrigin reports whether a and b name the same scheme, host and port. A
// port left out is the scheme's default: a server reached at "host:80" may
// redirect to "http://host/...", which Apache does whenever the port is the
// default.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		portOf(a) == portOf(b)
}

func portOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}
