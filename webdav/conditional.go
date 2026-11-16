package webdav

// Revisions and conditional mutations (api.ConditionalDriver).
//
// A WebDAV resource's revision is its entity tag, reported by PROPFIND as
// DAV:getetag. A conditional mutation hands that tag back to the server in an
// RFC 4918 If header, one tagged list per resource the condition is about:
//
//	If: <http://host/dav/a.txt> (["etag-a"]) <http://host/dav/b.txt> (["etag-b"])
//
// The server evaluates the header while it applies the method and answers 412
// Precondition Failed when a resource is not at its tag, so the check and the
// change are one step on the server. Nothing here reads the remote state
// first and decides for itself.
//
// WHY THE If HEADER AND NOT If-Match. If-Match applies to the request URI
// alone, and a save is a MOVE of a temporary upload whose condition is about
// the destination, which only a tagged If list can name. If-Match also uses
// strong comparison, so a weak tag (Apache's for a file changed in the last
// second) would never match. A server that implements the If header for
// locks but cannot evaluate an entity tag in it -- golang.org/x/net/webdav is
// one -- answers 412 to every such list, so a server that cannot enforce the
// condition fails the operation rather than performing it unconditionally.
//
// "Absent" -- an empty revision for a save's target or a rename's destination
// -- is Overwrite: F on the MOVE, which RFC 4918 requires the server to refuse
// with 412 when the destination exists.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
	"github.com/studio-b12/gowebdav"
)

var _ api.ConditionalDriver = (*WebDAVDriver)(nil)

// entityTag is RFC 9110's entity-tag: an optional weak prefix and a quoted
// string of etagc characters. A revision is put inside an If header, so
// anything else -- a bracket, a quote, a control character -- is refused
// rather than sent.
var entityTag = regexp.MustCompile(`^(W/)?"[\x21\x23-\x7e\x80-\xff]*"$`)

// revisionOf returns the entity tag gowebdav parsed from a PROPFIND, or empty
// when the server reported none.
func revisionOf(fi os.FileInfo) string {
	if tagged, ok := fi.(interface{ ETag() string }); ok {
		return strings.TrimSpace(tagged.ETag())
	}
	return ""
}

func validRevision(revision string) error {
	if revision == "" {
		return api.ErrRevisionRequired
	}
	if !entityTag.MatchString(revision) {
		return &api.DriverError{Code: api.ErrRevisionRequired.Code, Message: fmt.Sprintf("webdav: revision %q is not an entity tag", revision)}
	}
	return nil
}

// resourceURL is the URL gowebdav sends for a full server path, as the
// request URI or as a MOVE's Destination. An If header's tag must name a
// resource the way the request does, or the server cannot match it.
func (d *WebDAVDriver) resourceURL(full string) string {
	return gowebdav.PathEscape(gowebdav.Join(gowebdav.FixSlash(d.baseURL), full))
}

// condition is one tagged list: the resource at full must be at revision.
type condition struct {
	full     string
	revision string
}

// conditionalTransport sends the one request a conditional operation makes
// and keeps the status of its response.
//
// gowebdav acts on some answers with requests of its own: a MOVE answered 409
// Conflict is followed by an MKCOL of the destination's parents and the MOVE
// again. No precondition covers those -- the MKCOL would recreate a directory
// another client removed (#85), and the repeated MOVE would succeed in the
// directory it recreated -- so once the server has answered the operation's
// request, every further request is refused before it leaves, as is any
// request of another method. The operation then fails on the status of the
// one it sent. A request repeated for authentication (after 401) or to follow
// a redirect is still that request, and is sent.
//
// gowebdav also reduces some statuses to success -- DELETE's 404 among them
// -- so the operation reads the status itself.
type conditionalTransport struct {
	base     http.RoundTripper
	method   string
	answered bool
	status   int
}

func (s *conditionalTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if s.answered || request.Method != s.method {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, fmt.Errorf("webdav: conditional %s answered %d: refused to follow it with %s %s, which no precondition covers",
			s.method, s.status, request.Method, request.URL.Path)
	}
	response, err := s.base.RoundTrip(request)
	if response != nil {
		s.status = response.StatusCode
		redirect := s.status >= 300 && s.status < 400
		s.answered = s.status != http.StatusUnauthorized && !redirect
	}
	return response, err
}

// conditionalClient returns a client for one conditional operation, which
// sends only requests of method and carries conditions as its If header. It
// shares the mount's authorizer and counting transport, and is used by one
// goroutine and then dropped, so the header is never seen by any other
// request.
func (d *WebDAVDriver) conditionalClient(method string, conditions ...condition) (*gowebdav.Client, *conditionalTransport) {
	recorder := &conditionalTransport{base: d.transport, method: method}
	client := gowebdav.NewAuthClient(d.baseURL, d.auth)
	client.SetTransport(recorder)
	if len(conditions) > 0 {
		lists := make([]string, 0, len(conditions))
		for _, c := range conditions {
			lists = append(lists, fmt.Sprintf("<%s> ([%s])", d.resourceURL(c.full), c.revision))
		}
		client.SetHeader("If", strings.Join(lists, " "))
	}
	return client, recorder
}

// conditionResult maps a conditional request's outcome. 412 is the server
// refusing the precondition. When the request URI is the resource the caller
// observed (observedSource), a 404 means it is no longer there, which is the
// same conflict; a save's request URI is its own temporary upload, whose
// absence is an ordinary failure.
func conditionResult(err error, recorder *conditionalTransport, observedSource bool) error {
	switch {
	case recorder.status == http.StatusPreconditionFailed,
		recorder.status == http.StatusNotFound && observedSource:
		return fmt.Errorf("%w (HTTP %d)", api.ErrPreconditionFailed, recorder.status)
	}
	return err
}

// move issues one MOVE of fullOld to fullNew. The source must be at
// oldRevision unless it is empty (a save's own temporary upload). With an
// empty newRevision the destination must not exist; otherwise it must exist
// at newRevision and is replaced.
//
// A 409 Conflict is RFC 4918's answer when the destination's parent
// collection does not exist. It is reported as ErrNotFound and nothing is
// created: that collection may be a directory another client removed.
func (d *WebDAVDriver) move(fullOld, fullNew, oldRevision, newRevision string) error {
	var conditions []condition
	if oldRevision != "" {
		conditions = append(conditions, condition{fullOld, oldRevision})
	}
	if newRevision != "" {
		conditions = append(conditions, condition{fullNew, newRevision})
	}
	client, recorder := d.conditionalClient("MOVE", conditions...)
	err := client.Rename(fullOld, fullNew, newRevision != "")
	if recorder.status == http.StatusConflict {
		return fmt.Errorf("webdav: MOVE to %s: %w: its parent collection does not exist (HTTP 409)", fullNew, api.ErrNotFound)
	}
	return conditionResult(err, recorder, oldRevision != "")
}

// CreateFileIfMatch returns a writer that uploads on Close, replacing the file
// at path only while it is at revision; an empty revision creates the file
// only if nothing is at path. The upload goes to a temporary resource first,
// as CreateFile's does, so a refused or failed save leaves the target and its
// directory exactly as they were.
func (d *WebDAVDriver) CreateFileIfMatch(mountID int, path, revision string) (io.WriteCloser, error) {
	if !d.connected || d.client == nil {
		return nil, api.ErrNotConnected
	}
	if revision != "" {
		if err := validRevision(revision); err != nil {
			return nil, err
		}
	}
	w := &webdavWriter{driver: d, path: path}
	w.commit = func(fullTemporary string) error {
		uploaded := d.uploadRevision(fullTemporary)
		if err := d.move(fullTemporary, d.fullPath(path), "", revision); err != nil {
			return err
		}
		w.revision = d.confirmRevision(d.fullPath(path), uploaded)
		return nil
	}
	return w, nil
}

// THE REVISION A SAVE RETURNS (#84). Neither the PUT nor the MOVE of a save
// reports the revision it created: Apache mod_dav answers both without an
// ETag, and RFC 4918 (section 15.6) makes a MOVE destination's getetag depend
// on its own final state, not on the source's. A Stat of the target after the
// MOVE can report another client's save that landed in between. So the
// revision is established in two steps, neither of which can see anyone
// else's write:
//
//  1. Before the MOVE, the temporary upload is stat'ed for its entity tag.
//     Its name is random and private to this save, so no other client has
//     written it.
//  2. After the MOVE, a HEAD of the target with If-None-Match: <that tag> asks
//     the server whether the target is now at it. 304 is yes (RFC 9110 section
//     13.1.2, weak comparison), and the tag is the revision of what this save
//     wrote. Anything else -- a server that gave the destination a new tag,
//     another client's save landing straight after the MOVE, a server that
//     ignores the header -- is "not confirmed", and the save returns no
//     revision. The answer is only ever the tag this save uploaded, or
//     nothing; never a value the target reported, which could be someone
//     else's.
//
// A failure of either step leaves the save itself successful, with no
// revision.

// uploadRevision is the entity tag of a save's temporary upload, or "".
func (d *WebDAVDriver) uploadRevision(fullTemporary string) string {
	info, err := d.client.Stat(fullTemporary)
	if err != nil {
		return ""
	}
	revision := revisionOf(info)
	if validRevision(revision) != nil {
		return ""
	}
	return revision
}

// confirmRevision returns candidate if the server confirms the resource at
// full is at it, and "" otherwise.
func (d *WebDAVDriver) confirmRevision(full, candidate string) string {
	if candidate == "" {
		return ""
	}
	header := http.Header{}
	header.Set("If-None-Match", candidate)
	// A cache between here and the server must not answer for it.
	header.Set("Cache-Control", "no-cache")
	status, err := d.head(full, header)
	if err != nil || status != http.StatusNotModified {
		return ""
	}
	return candidate
}

// head sends a HEAD for the resource at full with header, authorised the way
// gowebdav authorises its own requests, and returns the response status. A
// redirect is returned, not followed. gowebdav has no HEAD of its own.
func (d *WebDAVDriver) head(full string, header http.Header) (int, error) {
	client := &http.Client{
		Transport:     d.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	auth, _ := d.auth.NewAuthenticator(nil)
	defer auth.Close()
	for {
		request, err := http.NewRequest(http.MethodHead, d.resourceURL(full), nil)
		if err != nil {
			return 0, err
		}
		for key, values := range header {
			request.Header[key] = values
		}
		if err := auth.Authorize(client, request, full); err != nil {
			return 0, err
		}
		response, err := client.Do(request)
		if err != nil {
			return 0, err
		}
		_ = response.Body.Close()
		redo, err := auth.Verify(client, response, full)
		if err != nil {
			return 0, err
		}
		if !redo {
			return response.StatusCode, nil
		}
	}
}

// RemoveIfMatch deletes path only while it is at revision. A directory is
// deleted with its contents; its revision is whatever the server reports for
// the collection, which need not change when something deep inside it does.
func (d *WebDAVDriver) RemoveIfMatch(mountID int, path, revision string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[WebDAV][PANIC][RemoveIfMatch] %v\n%s\n", r, debug.Stack())
			err = &api.DriverError{Code: 25, Message: fmt.Sprintf("panic in RemoveIfMatch: %v", r)}
		}
	}()

	if !d.connected || d.client == nil {
		return api.ErrNotConnected
	}
	if err := validRevision(revision); err != nil {
		return err
	}
	full := d.fullPath(path)
	client, recorder := d.conditionalClient(http.MethodDelete, condition{full, revision})
	err = client.Remove(full)
	return conditionResult(err, recorder, true)
}

// RenameIfMatch moves oldPath to newPath only while oldPath is at oldRevision.
// An empty newRevision requires newPath to be free; otherwise newPath must be
// at newRevision and is replaced.
func (d *WebDAVDriver) RenameIfMatch(mountID int, oldPath, newPath, oldRevision, newRevision string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[WebDAV][PANIC][RenameIfMatch] %v\n%s\n", r, debug.Stack())
			err = &api.DriverError{Code: 26, Message: fmt.Sprintf("panic in RenameIfMatch: %v", r)}
		}
	}()

	if !d.connected || d.client == nil {
		return api.ErrNotConnected
	}
	if err := validRevision(oldRevision); err != nil {
		return err
	}
	if newRevision != "" {
		if err := validRevision(newRevision); err != nil {
			return err
		}
	}
	return d.move(d.fullPath(oldPath), d.fullPath(newPath), oldRevision, newRevision)
}
