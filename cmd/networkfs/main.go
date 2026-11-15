// cmd/networkfs/main.go - Unified C library exports for all drivers
//
// Builds as: go build -buildmode=c-archive -o libnetworkfs.a ./cmd/networkfs
// Exports: networkfs_version, networkfs_mount, networkfs_unmount,
// networkfs_stat, networkfs_listdir, networkfs_openfile, networkfs_writefile,
// networkfs_mkdir, networkfs_remove, networkfs_rename, networkfs_writefile_if,
// networkfs_writefile_if_revision, networkfs_remove_if, networkfs_rename_if,
// networkfs_same_revision,
// networkfs_get_thumbnail, networkfs_free.
//
// Unlike the per-driver libraries (libftp.a, libsftp.a, etc.), this archive
// links every registered driver and dispatches by driver_type at mount time.
// Callers pick the backend by passing driver_type to networkfs_mount:
//   1=FTP, 2=SFTP, 3=SMB, 4=Dropbox, 5=WebDAV, 6=GDrive, 7=S3.
//
// NOTE: cgo marshaling helpers are inlined here for the same reason as the
// per-driver mains — see ftp/cmd/ftp/main.go for the explanation.

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
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/antimatter-studios/go-networkfs/pkg/api"

	// Blank imports: each driver's init() registers itself with api.registry.
	_ "github.com/antimatter-studios/go-networkfs/dropbox"
	_ "github.com/antimatter-studios/go-networkfs/ftp"
	_ "github.com/antimatter-studios/go-networkfs/gdrive"
	_ "github.com/antimatter-studios/go-networkfs/onedrive"
	_ "github.com/antimatter-studios/go-networkfs/s3"
	_ "github.com/antimatter-studios/go-networkfs/sftp"
	_ "github.com/antimatter-studios/go-networkfs/smb"
	_ "github.com/antimatter-studios/go-networkfs/webdav"
)

var manager = api.NewMountManager()

// --- local cgo marshaling helpers ------------------------------------------

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

//export networkfs_version
func networkfs_version() *C.char {
	return stringToC("1.0.0")
}

// mountID: unique identifier for this mount instance
// driverType: 1=FTP, 2=SFTP, 3=SMB, 4=Dropbox, 5=WebDAV, 6=GDrive, 7=S3
// configJSON: driver-specific config, e.g. {"host":"...","user":"...",...}
// outErr: on non-zero return, receives a human-readable, classified error
// message (e.g. "cannot resolve host", "authentication failed", "remote path
// does not exist"). Caller must free with networkfs_free. On success, *outErr
// is left untouched; callers should initialise it to NULL.
// Returns: 0 on success, 1 on unknown driver type, 2 on mount failure, -1 on invalid JSON
//
//export networkfs_mount
func networkfs_mount(mountID C.int, driverType C.int, configJSON *C.char, outErr **C.char) C.int {
	config := make(map[string]string)
	if err := jsonFromC(configJSON, &config); err != nil {
		setOutString(outErr, "invalid config JSON: "+err.Error())
		return -1
	}
	if err := manager.Mount(int(mountID), int(driverType), config); err != nil {
		if err == api.ErrUnknownDriverType {
			setOutString(outErr, fmt.Sprintf("unknown driver type: %d", int(driverType)))
			return 1
		}
		setOutString(outErr, err.Error())
		return 2
	}
	return 0
}

//export networkfs_unmount
func networkfs_unmount(mountID C.int) C.int {
	if err := manager.Unmount(int(mountID)); err != nil {
		return 1
	}
	return 0
}

// Returns JSON FileInfo or error message (caller must free with networkfs_free).
// FileInfo includes "revision", the server's opaque revision, when the driver
// reports one; pass it back unchanged to the *_if exports.
//
//export networkfs_stat
func networkfs_stat(mountID C.int, path *C.char, outJSON **C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
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

// Returns JSON array of FileInfo (caller must free with networkfs_free)
//
//export networkfs_listdir
func networkfs_listdir(mountID C.int, path *C.char, outJSON **C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
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
	// Empty directories: drivers that return a nil slice (OneDrive,
	// possibly others) would otherwise marshal to JSON `null`, which
	// the Swift caller's `[RemoteFileInfo]` decoder rejects with
	// "Expected Array but found null". Normalise nil → [] so the C
	// ABI contract ("JSON array of FileInfo") holds for every driver.
	if entries == nil {
		entries = []api.FileInfo{}
	}
	*outJSON = jsonToC(entries)
	return 0
}

// Returns file data in out buffer (caller must free out.data with networkfs_free).
// On failure, and for an empty file, out.data is NULL and out.len is 0: there
// is nothing to free. The whole file is read before 0 is returned; a download
// that ends early returns 1, never a successful prefix.
//
//export networkfs_openfile
func networkfs_openfile(mountID C.int, path *C.char, out *C.ByteSlice) C.int {
	if out == nil {
		return 1
	}
	out.data = nil
	out.len = 0
	driver, release, ok := manager.Acquire(int(mountID))
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
//export networkfs_writefile
func networkfs_writefile(mountID C.int, path *C.char, data C.ByteSlice) C.int {
	goData, ok := bytesFromC(data)
	if !ok {
		return 1
	}
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := api.WriteFile(driver, int(mountID), stringFromC(path), goData); err != nil {
		return 1
	}
	return 0
}

//export networkfs_mkdir
func networkfs_mkdir(mountID C.int, path *C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := driver.Mkdir(int(mountID), stringFromC(path)); err != nil {
		return 1
	}
	return 0
}

//export networkfs_remove
func networkfs_remove(mountID C.int, path *C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return 1
	}
	defer release()
	if err := driver.Remove(int(mountID), stringFromC(path)); err != nil {
		return 1
	}
	return 0
}

//export networkfs_rename
func networkfs_rename(mountID C.int, oldPath *C.char, newPath *C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
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
// A revision is the opaque "revision" string from networkfs_stat or
// networkfs_listdir, passed back exactly as it was received, and compared
// with networkfs_same_revision, never with strcmp. The caller owns
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
// replaced. data follows networkfs_writefile's rules. A refused or failed write
// leaves the existing file and its directory as they were.
//
//export networkfs_writefile_if
func networkfs_writefile_if(mountID C.int, path *C.char, data C.ByteSlice, revision *C.char) C.int {
	goData, ok := bytesFromC(data)
	if !ok {
		return conditionalFailed
	}
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	return conditionalStatus(api.WriteFileIfMatch(driver, int(mountID), stringFromC(path), stringFromC(revision), goData))
}

// Writes exactly as networkfs_writefile_if does, and on success sets *newRevision
// to the revision of the state the write created: the revision to pass to the
// next conditional write, obtained without a stat that could report another
// client's later save instead. The string is allocated here and the caller
// frees it with networkfs_free.
//
// *newRevision is NULL on every non-zero return, and also on a successful
// write whose revision the driver could not establish. That NULL means the
// write went through but what is on the server now is unknown: treat the file
// as after a conflict -- read it again and reconcile -- and do not stat it for
// a revision to carry forward. newRevision may itself be NULL, and the call
// is then networkfs_writefile_if.
//
//export networkfs_writefile_if_revision
func networkfs_writefile_if_revision(mountID C.int, path *C.char, data C.ByteSlice, revision *C.char, newRevision **C.char) C.int {
	if newRevision != nil {
		*newRevision = nil
	}
	goData, ok := bytesFromC(data)
	if !ok {
		return conditionalFailed
	}
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	created, err := api.WriteFileIfMatchRevision(driver, int(mountID), stringFromC(path), stringFromC(revision), goData)
	if err == nil && created != "" && newRevision != nil {
		*newRevision = C.CString(created)
	}
	return conditionalStatus(err)
}

// Removes path (a directory with its contents) only if it is at revision,
// which may not be empty or NULL.
//
//export networkfs_remove_if
func networkfs_remove_if(mountID C.int, path *C.char, revision *C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
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
//export networkfs_rename_if
func networkfs_rename_if(mountID C.int, oldPath *C.char, newPath *C.char, oldRevision *C.char, newRevision *C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		return conditionalFailed
	}
	defer release()
	return conditionalStatus(api.RenameIfMatch(driver, int(mountID), stringFromC(oldPath), stringFromC(newPath),
		stringFromC(oldRevision), stringFromC(newRevision)))
}

// Returns 1 if revisions a and b, as networkfs_stat and networkfs_listdir report
// them, name the same state of a file, and 0 otherwise. Compare revisions
// with it, never with strcmp: a server can report one state as two
// different strings (a WebDAV entity tag marked weak, W/"x", and later the
// same tag strong, "x"). An empty or NULL revision names no state, so it is
// never the same as anything.
//
//export networkfs_same_revision
func networkfs_same_revision(a *C.char, b *C.char) C.int {
	if api.SameRevision(stringFromC(a), stringFromC(b)) {
		return 1
	}
	return 0
}

// Returns thumbnail bytes via out (caller must free out.data with
// networkfs_free). On non-zero return, *outErr carries a
// human-readable message (caller frees with networkfs_free).
//
//export networkfs_get_thumbnail
func networkfs_get_thumbnail(mountID C.int, path *C.char, sizePx C.int,
	out *C.ByteSlice, outErr **C.char) C.int {
	driver, release, ok := manager.Acquire(int(mountID))
	if !ok {
		setOutString(outErr, "mount not found")
		return 1
	}
	defer release()
	t, ok := driver.(api.Thumbnailer)
	if !ok {
		setOutString(outErr, "driver does not support thumbnails")
		return 2
	}
	data, err := t.GetThumbnail(int(mountID), stringFromC(path), int(sizePx))
	if err != nil {
		setOutString(outErr, err.Error())
		return 3
	}
	setOutBytes(&out.data, &out.len, data)
	return 0
}

// Returns JSON array of registered driver type IDs (caller must free with networkfs_free)
//
//export networkfs_drivers
func networkfs_drivers(outJSON **C.char) C.int {
	*outJSON = jsonToC(api.ListDriverTypes())
	return 0
}

// Returns a JSON object with this mount's transport byte/op counters
// (caller must free `outJSON` with networkfs_free):
//
//	{
//	  "bytes_read":    <uint64>,  // response body bytes received
//	  "bytes_written": <uint64>,  // request body bytes sent
//	  "ops_read":      <uint64>,  // completed responses
//	  "ops_written":   <uint64>   // issued requests
//	}
//
// Returns all-zero counters when:
//   - the mount doesn't exist (still returns 0, *not* an error, so the
//     1 Hz Swift poller doesn't spam errors during teardown), or
//   - the driver is non-HTTP (FTP/SFTP/SMB) and so doesn't implement
//     api.StatsProvider.
//
//export networkfs_get_stats
func networkfs_get_stats(mountID C.int32_t, outJSON **C.char) C.int {
	stats := manager.Stats(int(mountID))
	if stats == nil {
		*outJSON = jsonToC(map[string]uint64{
			"bytes_read":    0,
			"bytes_written": 0,
			"ops_read":      0,
			"ops_written":   0,
		})
		return 0
	}
	br, bw, or, ow := stats.Snapshot()
	*outJSON = jsonToC(map[string]uint64{
		"bytes_read":    br,
		"bytes_written": bw,
		"ops_read":      or,
		"ops_written":   ow,
	})
	return 0
}

// Frees memory allocated by the exports above. Safe to call with NULL.
//
//export networkfs_free
func networkfs_free(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}

func main() {}
