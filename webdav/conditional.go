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

// statusRecorder keeps the status of the last response a conditional client
// received. gowebdav reduces some statuses to success -- DELETE's 404 among
// them -- so the operation reads the status itself.
type statusRecorder struct {
	base   http.RoundTripper
	status int
}

func (s *statusRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := s.base.RoundTrip(request)
	if response != nil {
		s.status = response.StatusCode
	}
	return response, err
}

// conditionalClient returns a client for one conditional operation, carrying
// conditions as its If header. It shares the mount's authorizer and counting
// transport, and is used by one goroutine and then dropped, so the header is
// never seen by any other request.
func (d *WebDAVDriver) conditionalClient(conditions ...condition) (*gowebdav.Client, *statusRecorder) {
	recorder := &statusRecorder{base: d.transport}
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
func conditionResult(err error, recorder *statusRecorder, observedSource bool) error {
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
func (d *WebDAVDriver) move(fullOld, fullNew, oldRevision, newRevision string) error {
	var conditions []condition
	if oldRevision != "" {
		conditions = append(conditions, condition{fullOld, oldRevision})
	}
	if newRevision != "" {
		conditions = append(conditions, condition{fullNew, newRevision})
	}
	client, recorder := d.conditionalClient(conditions...)
	err := client.Rename(fullOld, fullNew, newRevision != "")
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
		return d.move(fullTemporary, d.fullPath(path), "", revision)
	}
	return w, nil
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
	client, recorder := d.conditionalClient(condition{full, revision})
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
