package api

import (
	"errors"
	"io"
)

// Revisions and conditional mutations.
//
// A revision is the server's opaque identifier for one state of a file or
// directory: for WebDAV it is the resource's entity tag (getetag), quotes and
// any W/ prefix included. FileInfo.Revision carries it from Stat and ListDir.
// A caller must treat it as an opaque string: compare it for equality, store
// it, hand it back, and never parse it or derive it from size and time. Two
// states of a file with the same size and modification time can have
// different revisions, and that is the point of having them.
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

// ConditionalDriver is implemented by drivers whose server can enforce a
// revision precondition on a mutation. The dispatcher type-asserts on it; a
// driver that does not implement it has every conditional operation refused
// with ErrConditionNotSupported, never performed unconditionally.
type ConditionalDriver interface {
	// CreateFileIfMatch returns a writer like CreateFile's, whose Close
	// replaces the file at path only if the file there is still at revision.
	// An empty revision requires that nothing exist at path, so a new file is
	// created and an existing one is never replaced.
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
// when the driver cannot enforce the condition.
func WriteFileIfMatch(driver Driver, mountID int, path, revision string, data []byte) error {
	conditional, ok := driver.(ConditionalDriver)
	if !ok {
		return ErrConditionNotSupported
	}
	writer, err := conditional.CreateFileIfMatch(mountID, path, revision)
	if err != nil {
		return err
	}
	written, writeErr := writer.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, writer.Close())
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
