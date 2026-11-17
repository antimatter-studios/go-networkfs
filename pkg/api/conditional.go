package api

import (
	"errors"
	"io"
	"strings"
)

// Revisions and conditional mutations.
//
// A revision is the server's opaque identifier for one state of a file or
// directory: for WebDAV it is the resource's entity tag (getetag), quotes and
// any W/ prefix included. FileInfo.Revision carries it from Stat and ListDir.
// A caller must treat it as an opaque string: store it, hand it back exactly
// as received, compare it with SameRevision, and never parse it or derive it
// from size and time. Two states of a file with the same size and
// modification time can have different revisions, and that is the point of
// having them. The converse also holds: one state can be reported as two
// different strings -- a WebDAV entity tag marked weak, W/"x", and later the
// same tag strong, "x" -- so two revisions that differ as strings need not
// name different states. SameRevision knows which differences matter;
// string equality does not.
//
// An empty revision means the driver or server reported none. A file without
// a revision cannot be the subject of a conditional mutation: an empty
// revision given to one of the operations below means "nothing exists at this
// path", and is never a request to skip the check.
//
// A conditional mutation sends the revision to the server as a precondition,
// and the server decides whether it still holds at the moment it applies the
// change. There is no separate check before the mutation, so there is no
// window between a check and a write for another client's edit to fall into.
// When the precondition does not hold, nothing at the named paths changes and
// the operation returns ErrPreconditionFailed.
//
// A conditional write returns the revision of the state it wrote
// (WriteFileIfMatchRevision), which is the revision to give the next
// conditional write. Never learn it by a Stat after the write: another
// client's save can land between the two, the Stat then reports that save's
// revision, and a write made at it replaces that client's edit without a
// conflict. A driver returns the revision only when it can establish it
// without that risk. When it cannot, the write still succeeded and the
// revision is "": the caller does not know what is on the server now, and
// treats the file as it would after a conflict -- read it again and
// reconcile. Handing "" back to a conditional write asks that nothing exist
// at the path, so a forgotten unknown revision is refused, never mistaken for
// a known one.

// ConditionalDriver is implemented by drivers whose server can enforce a
// revision precondition on a mutation. The dispatcher type-asserts on it; a
// driver that does not implement it has every conditional operation refused
// with ErrConditionNotSupported, never performed unconditionally.
type ConditionalDriver interface {
	// CreateFileIfMatch returns a writer like CreateFile's, whose Close
	// replaces the file at path only if the file there is still at revision.
	// An empty revision requires that nothing exist at path, so a new file is
	// created and an existing one is never replaced. A driver that can report
	// the revision of the state Close wrote returns a RevisionWriter.
	CreateFileIfMatch(mountID int, path, revision string) (io.WriteCloser, error)

	// RemoveIfMatch removes the file or directory at path only if it is still
	// at revision. revision may not be empty.
	RemoveIfMatch(mountID int, path, revision string) error

	// RenameIfMatch moves oldPath to newPath only if oldPath is still at
	// oldRevision, which may not be empty. An empty newRevision requires that
	// nothing exist at newPath; a non-empty one requires that newPath exist at
	// that revision, and the move then replaces it.
	RenameIfMatch(mountID int, oldPath, newPath, oldRevision, newRevision string) error
}

// RevisionWriter is the writer CreateFileIfMatch returns when its driver can
// report the revision of the state the write created.
type RevisionWriter interface {
	io.WriteCloser
	// Revision returns, after a Close that returned nil, the revision of the
	// state that Close wrote. It is "" when the driver could not establish
	// it without the risk of reporting another client's later write instead.
	Revision() string
}

// Errors returned by the conditional operations.
var (
	// ErrPreconditionFailed reports that the remote file was not at the
	// revision the caller gave: another client changed, replaced, created or
	// removed it. Nothing at the named paths was changed.
	ErrPreconditionFailed = &DriverError{Code: 7, Message: "precondition failed: the remote revision changed"}

	// ErrConditionNotSupported reports that the driver cannot have a
	// precondition enforced by its server. The operation was not attempted.
	ErrConditionNotSupported = &DriverError{Code: 8, Message: "conditional mutation not supported by this driver"}

	// ErrRevisionRequired reports an empty revision where one is required.
	ErrRevisionRequired = &DriverError{Code: 9, Message: "a revision is required"}
)

// WriteFileIfMatch writes data to path through driver, replacing the file
// only if it is still at revision (or, with an empty revision, only if no file
// exists there). It returns ErrConditionNotSupported, having sent nothing,
// when the driver cannot enforce the condition. WriteFileIfMatchRevision is
// the same write, and also returns the revision it created.
func WriteFileIfMatch(driver Driver, mountID int, path, revision string, data []byte) error {
	_, err := WriteFileIfMatchRevision(driver, mountID, path, revision, data)
	return err
}

// WriteFileIfMatchRevision is WriteFileIfMatch, returning on success the
// revision of the state the write created: the revision to hand the next
// conditional write. It is "" when the driver cannot establish it (see the
// package notes above ConditionalDriver), and always "" with an error.
func WriteFileIfMatchRevision(driver Driver, mountID int, path, revision string, data []byte) (string, error) {
	conditional, ok := driver.(ConditionalDriver)
	if !ok {
		return "", ErrConditionNotSupported
	}
	writer, err := conditional.CreateFileIfMatch(mountID, path, revision)
	if err != nil {
		return "", err
	}
	written, writeErr := writer.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, writer.Close()); err != nil {
		return "", err
	}
	if reporter, ok := writer.(RevisionWriter); ok {
		return reporter.Revision(), nil
	}
	return "", nil
}

// RemoveIfMatch removes path through driver only if it is still at revision.
func RemoveIfMatch(driver Driver, mountID int, path, revision string) error {
	conditional, ok := driver.(ConditionalDriver)
	if !ok {
		return ErrConditionNotSupported
	}
	return conditional.RemoveIfMatch(mountID, path, revision)
}

// RenameIfMatch moves oldPath to newPath through driver under the conditions
// ConditionalDriver.RenameIfMatch describes.
func RenameIfMatch(driver Driver, mountID int, oldPath, newPath, oldRevision, newRevision string) error {
	conditional, ok := driver.(ConditionalDriver)
	if !ok {
		return ErrConditionNotSupported
	}
	return conditional.RenameIfMatch(mountID, oldPath, newPath, oldRevision, newRevision)
}

// SameRevision reports whether revisions a and b name the same state of a
// file. Compare revisions with it, never with ==: a server can report one
// state as two different strings.
//
// A WebDAV revision is an entity tag, and a server may mark one state's tag
// weak at one moment and strong at the next. Apache marks a file's tag weak
// while its modification time is within a second of the request, so a file
// nobody touches reports W/"7-66055e976a900" straight after a write and
// "7-66055e976a900" a second later (#94). SameRevision is therefore RFC 9110's
// weak comparison (section 8.8.3.2) when both revisions are entity tags: they
// are the same when their opaque-tags match, whether or not either is marked
// weak. That is also how the server compares the tags a conditional mutation
// sends it. A revision that is not an entity tag is compared exactly.
//
// An empty revision names no state, so it is never the same as anything,
// another empty revision included: two files that both report no revision
// are not known to be unchanged.
func SameRevision(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	tagA, aIsTag := opaqueTag(a)
	tagB, bIsTag := opaqueTag(b)
	if aIsTag && bIsTag {
		return tagA == tagB
	}
	return a == b
}

// opaqueTag returns an entity tag's quoted opaque-tag without its weak
// indicator (RFC 9110 section 8.8.3: entity-tag = [ "W/" ] opaque-tag), and
// whether revision has that shape at all.
func opaqueTag(revision string) (string, bool) {
	tag := strings.TrimPrefix(revision, "W/")
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' || strings.Contains(tag[1:len(tag)-1], `"`) {
		return "", false
	}
	return tag, true
}
