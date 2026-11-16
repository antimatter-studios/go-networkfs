// webdav/cmd/webdav/main.go - WebDAV driver C library exports
//
// Builds as: go build -buildmode=c-archive -o libwebdav.a ./webdav/cmd/webdav
// Exports: webdav_version, webdav_mount, webdav_unmount, webdav_stat,
// webdav_listdir, webdav_openfile, webdav_writefile, webdav_mkdir,
// webdav_remove, webdav_rename, webdav_writefile_if, webdav_remove_if,
// webdav_rename_if, webdav_free.
//
// NOTE: cgo marshaling helpers (stringFromC, jsonToC, setOutString, …)
// are intentionally inlined here rather than imported from
// github.com/antimatter-studios/go-networkfs/pkg/api/cgo. When cgo helpers
// are defined in a separate package, their `C.char` becomes a named
// type scoped to that package — not assignable to `*C.char` in any
// other package's main. The only portable way to share cgo glue is
// to copy it per-binary; that's what we do here.

package main

/*
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

typedef struct {
	char* data;
	size_t len;
} ByteSlice;
*/
import "C"

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"unsafe"

	"github.com/antimatter-studios/go-networkfs/pkg/api"
	"github.com/antimatter-studios/go-networkfs/webdav"
)

// mounts holds one WebDAV driver per mount ID. Each driver is its own client
// and connection, so a call names exactly one server, and unmounting one ID
// leaves every other ID connected. An ID that is not mounted names nothing.
var mounts = api.NewMountManager()

// --- local cgo marshaling helpers (see NOTE above) -------------------------

func stringFromC(cstr *C.char) string {
	if cstr == nil {
		return ""
	}
	return C.GoString(cstr)
}

func jsonFromC(cstr *C.char, v interface{}) error {
	return json.Unmarshal([]byte(stringFromC(cstr)), v)
}

func stringToC(s string) *C.char {
	return C.CString(s)
}

func jsonToC(v interface{}) *C.char {
	data, err := json.Marshal(v)
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		return C.CString(string(errData))
	}
	return C.CString(string(data))
}

func errorToC(err error) *C.char {
	if err == nil {
		return nil
	}
	return C.CString(err.Error())
}

func setOutString(out **C.char, s string) {
	*out = C.CString(s)
}

func setOutBytes(outData **C.char, outLen *C.size_t, data []byte) {
	if len(data) == 0 {
		*outData = nil
		*outLen = 0
		return
	}
	*outData = (*C.char)(C.CBytes(data))
	*outLen = C.size_t(len(data))
}

// maxWriteLen is the longest buffer a write accepts: C.GoBytes takes its
// length as a C int, and a write is buffered in memory whole.
const maxWriteLen = math.MaxInt32

// bytesFromC copies a caller's buffer into Go memory, after checking that its
// length can be represented and that a nonzero length comes with a pointer.
// It reads nothing when either check fails. A zero length is a valid, empty
// buffer whatever the pointer holds.
func bytesFromC(data C.ByteSlice) ([]byte, bool) {
	if data.len == 0 {
		return []byte{}, true
	}
	if data.data == nil || uint64(data.len) > maxWriteLen {
		return nil, false
	}
	return C.GoBytes(unsafe.Pointer(data.data), C.int(data.len)), true
}

// --- exports ---------------------------------------------------------------

//export webdav_version
func webdav_version() *C.char {
	return stringToC("1.0.0")
}

// mountID: unique identifier for this mount instance
// configJSON: {"url":"...","host":"...","port":"443","user":"...","pass":"...","path":"/prefix"}
// Returns: 0 on success, error code on failure
//
//export webdav_mount
func webdav_mount(mountID C.int, configJSON *C.char) C.int {
	config := make(map[string]string)
	if err := jsonFromC(configJSON, &config); err != nil {
		return -1 // Invalid JSON
	}
	if err := mounts.Mount(int(mountID), webdav.DriverTypeID, config); err != nil {
		return 1 // Mount failed
	}
	return 0
}

//export webdav_unmount
func webdav_unmount(mountID C.int) C.int {
	if err := mounts.Unmount(int(mountID)); err != nil {
		return 1
	}
	return 0
}

// Returns JSON FileInfo or error (caller must free with webdav_free). FileInfo
// includes "revision", the entity tag, when the server reports one; pass it
// back unchanged to the *_if exports.
//
//export webdav_stat
func webdav_stat(mountID C.int, path *C.char, outJSON **C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		setOutString(outJSON, "mount not found")
		return 1
	}
	defer release()
	info, err := driver.Stat(int(mountID), stringFromC(path))
	if err != nil {
		setOutString(outJSON, C.GoString(errorToC(err)))
		return 1
	}
	*outJSON = jsonToC(info)
	return 0
}

// Returns JSON array of FileInfo (caller must free with webdav_free)
//
//export webdav_listdir
func webdav_listdir(mountID C.int, path *C.char, outJSON **C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		setOutString(outJSON, "mount not found")
		return 1
	}
	defer release()
	entries, err := driver.ListDir(int(mountID), stringFromC(path))
	if err != nil {
		setOutString(outJSON, C.GoString(errorToC(err)))
		return 1
	}
	*outJSON = jsonToC(entries)
	return 0
}

// Returns file data in out buffer (caller must free out.data with webdav_free).
// On failure, and for an empty file, out.data is NULL and out.len is 0: there
// is nothing to free. The whole file is read before 0 is returned; a download
// that ends early returns 1, never a successful prefix.
//
//export webdav_openfile
func webdav_openfile(mountID C.int, path *C.char, out *C.ByteSlice) C.int {
	if out == nil {
		return 1
	}
	out.data = nil
	out.len = 0
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	reader, err := driver.OpenFile(int(mountID), stringFromC(path))
	if err != nil {
		return 1
	}
	// Only io.EOF ends a read cleanly; io.ReadAll does not report it. Any
	// other error, io.ErrUnexpectedEOF from a body cut short of its
	// Content-Length included, fails the call rather than handing back a
	// prefix of the file as if it were all of it.
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return 1
	}

	setOutBytes(&out.data, &out.len, data)
	return 0
}

// data: file contents to write. data.len is at most 2^31 - 1, and a nonzero
// data.len needs a non-NULL data.data; a buffer that breaks either rule is
// refused before it is read, and nothing is sent. A zero data.len writes an
// empty file whatever data.data holds.
// Returns: 0 on success
//
//export webdav_writefile
func webdav_writefile(mountID C.int, path *C.char, data C.ByteSlice) C.int {
	goData, ok := bytesFromC(data)
	if !ok {
		return 1
	}
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := api.WriteFile(driver, int(mountID), stringFromC(path), goData); err != nil {
		return 1
	}
	return 0
}

//export webdav_mkdir
func webdav_mkdir(mountID C.int, path *C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := driver.Mkdir(int(mountID), stringFromC(path)); err != nil {
		return 1
	}
	return 0
}

//export webdav_remove
func webdav_remove(mountID C.int, path *C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := driver.Remove(int(mountID), stringFromC(path)); err != nil {
		return 1
	}
	return 0
}

//export webdav_rename
func webdav_rename(mountID C.int, oldPath *C.char, newPath *C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := driver.Rename(int(mountID), stringFromC(oldPath), stringFromC(newPath)); err != nil {
		return 1
	}
	return 0
}

// --- conditional mutations -------------------------------------------------
//
// A revision is the opaque "revision" string from webdav_stat or
// webdav_listdir, passed back exactly as it was received. The caller owns
// every string it passes; nothing is retained after the call returns. A NULL
// revision is the same as an empty one.
//
// The server enforces the condition while it applies the change; nothing is
// checked beforehand. Every conditional export returns:
//
//	0  success
//	1  failure: bad arguments, unmounted ID, or a transport/server error
//	2  precondition conflict: the remote file was not at the given revision
//	   (another client changed, replaced, created or removed it), and nothing
//	   at the named paths was changed
//	3  the driver cannot have the condition enforced by its server; nothing
//	   was sent, and the operation is never performed unconditionally instead
const (
	conditionalOK          = 0
	conditionalFailed      = 1
	conditionalConflict    = 2
	conditionalUnsupported = 3
)

func conditionalStatus(err error) C.int {
	switch {
	case err == nil:
		return conditionalOK
	case errors.Is(err, api.ErrPreconditionFailed):
		return conditionalConflict
	case errors.Is(err, api.ErrConditionNotSupported):
		return conditionalUnsupported
	}
	return conditionalFailed
}

// Writes data to path only if the file there is at revision. An empty or NULL
// revision writes only if nothing exists at path, so an existing file is never
// replaced. data follows webdav_writefile's rules. A refused or failed write
// leaves the existing file and its directory as they were.
//
//export webdav_writefile_if
func webdav_writefile_if(mountID C.int, path *C.char, data C.ByteSlice, revision *C.char) C.int {
	goData, ok := bytesFromC(data)
	if !ok {
		return conditionalFailed
	}
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	return conditionalStatus(api.WriteFileIfMatch(driver, int(mountID), stringFromC(path), stringFromC(revision), goData))
}

// Removes path (a directory with its contents) only if it is at revision,
// which may not be empty or NULL.
//
//export webdav_remove_if
func webdav_remove_if(mountID C.int, path *C.char, revision *C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	return conditionalStatus(api.RemoveIfMatch(driver, int(mountID), stringFromC(path), stringFromC(revision)))
}

// Moves oldPath to newPath only if oldPath is at oldRevision, which may not be
// empty or NULL. An empty or NULL newRevision requires that nothing exist at
// newPath; otherwise newPath must be at newRevision and is replaced.
//
//export webdav_rename_if
func webdav_rename_if(mountID C.int, oldPath *C.char, newPath *C.char, oldRevision *C.char, newRevision *C.char) C.int {
	driver, release, ok := mounts.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	return conditionalStatus(api.RenameIfMatch(driver, int(mountID), stringFromC(oldPath), stringFromC(newPath),
		stringFromC(oldRevision), stringFromC(newRevision)))
}

// Frees memory allocated by webdav_stat, webdav_listdir, webdav_openfile,
// webdav_writefile, webdav_version. Safe to call with NULL.
//
//export webdav_free
func webdav_free(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}

func main() {}
