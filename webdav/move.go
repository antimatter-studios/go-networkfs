package webdav

import (
	"fmt"
	"net/http"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
	"github.com/studio-b12/gowebdav"
)

// An unconditional MOVE -- Rename, and the commit of CreateFile's save.
//
// gowebdav's Rename answers a 409 Conflict by creating the destination's
// parents and calling itself again, with no bound (#92). When there is nothing
// to create the same MOVE goes out again and again until the goroutine's stack
// is exhausted: a destination at the root of the URL space has no parent for
// it to create, and an MKCOL answered 405 -- the parent already exists, or the
// server refuses it for any other reason -- is reported to it as success.
//
// boundedMoveTransport keeps what the retry is for, a parent created where
// none existed, and nothing more: the MOVE is sent again only after an MKCOL
// created a collection, and only once. Every request after that is refused
// before it leaves, so the operation fails on the 409 the server gave.
type boundedMoveTransport struct {
	base http.RoundTripper
	// moves counts the MOVEs the server answered, status is the last one's.
	moves  int
	status int
	// created reports an MKCOL answered 201 since the last MOVE was answered.
	created bool
	// done is set once nothing further may be sent.
	done bool
}

// maxMoves is the MOVE and its one retry.
const maxMoves = 2

func (t *boundedMoveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == "MOVE" && t.moves > 0 && !t.created {
		t.done = true
	}
	if t.done {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, fmt.Errorf("webdav: MOVE answered %d: refused to follow it with %s %s, which would repeat it with nothing changed",
			t.status, request.Method, request.URL.Path)
	}
	response, err := t.base.RoundTrip(request)
	if response == nil {
		return response, err
	}
	switch request.Method {
	case "MOVE":
		// A request repeated for authentication (after 401) or to follow a
		// redirect is still the same MOVE, and is not counted.
		redirect := response.StatusCode >= 300 && response.StatusCode < 400
		if response.StatusCode != http.StatusUnauthorized && !redirect {
			t.moves++
			t.status = response.StatusCode
			t.created = false
			t.done = t.moves >= maxMoves
		}
	case "MKCOL":
		if response.StatusCode == http.StatusCreated {
			t.created = true
		}
	}
	return response, err
}

// parentConflict is the error for a MOVE to fullNew answered 409 Conflict,
// RFC 4918's answer when the destination's parent collection does not exist.
func parentConflict(fullNew string) error {
	return fmt.Errorf("webdav: MOVE to %s: %w: its parent collection does not exist (HTTP 409)", fullNew, api.ErrNotFound)
}

// successfulMove reports a MOVE answer gowebdav counts as a failure although
// it is success. gowebdav accepts 201 and 204, the two RFC 4918 lists, but any
// 2xx is success in HTTP, and lighttpd answers 200 to a MOVE of a collection.
func successfulMove(status int) bool {
	return status >= 200 && status < 300
}

// rename moves fullOld to fullNew, replacing whatever is there, through a
// client of its own whose transport bounds gowebdav's retry. That client
// shares the mount's authorizer and counting transport, and is used by one
// goroutine and then dropped.
func (d *WebDAVDriver) rename(fullOld, fullNew string) error {
	bounded := &boundedMoveTransport{base: d.transport}
	client := gowebdav.NewAuthClient(d.clientRoot, d.auth)
	client.SetTransport(bounded)
	err := client.Rename(fullOld, fullNew, true)
	if err != nil && bounded.status == http.StatusConflict {
		return parentConflict(fullNew)
	}
	if err != nil && successfulMove(bounded.status) {
		return nil
	}
	return err
}
