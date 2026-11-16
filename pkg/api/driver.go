// pkg/api/driver.go - Shared filesystem driver interface
//
// This package defines the common interface that all network filesystem
// drivers must implement. It provides the abstraction layer between
// protocol-specific implementations (FTP, SFTP, SMB, etc.) and the
// unified C API exported via cgo.

package api

import (
	"io"
	"sync"
)

// FileInfo represents metadata about a file or directory
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime int64  `json:"mod_time"`
	Mode    uint32 `json:"mode"`
	// Revision is the server's opaque identifier for this state of the
	// file, or empty when the driver reports none. See conditional.go.
	Revision string `json:"revision,omitempty"`
}

// Driver is the interface all network filesystem backends implement
type Driver interface {
	// Name returns the driver type identifier (e.g., "ftp", "sftp", "smb")
	Name() string

	// Mount establishes a connection to the remote filesystem
	// config contains driver-specific connection parameters
	Mount(mountID int, config map[string]string) error

	// Unmount closes the connection and cleans up resources
	Unmount(mountID int) error

	// Stat retrieves file/directory metadata
	Stat(mountID int, path string) (FileInfo, error)

	// ListDir returns entries in a directory
	ListDir(mountID int, path string) ([]FileInfo, error)

	// OpenFile returns a ReadCloser for file contents
	OpenFile(mountID int, path string) (io.ReadCloser, error)

	// CreateFile creates/truncates a file for writing
	CreateFile(mountID int, path string) (io.WriteCloser, error)

	// Mkdir creates a directory
	Mkdir(mountID int, path string) error

	// Remove deletes a file or directory
	Remove(mountID int, path string) error

	// Rename moves/renames a file or directory
	Rename(mountID int, oldPath, newPath string) error
}

// Thumbnailer is an optional capability — drivers that can produce
// a thumbnail image for a remote file (cloud providers with
// thumbnail HTTP endpoints) implement this. The dispatcher type-
// asserts on it; drivers that don't implement it return a clear
// "not supported" from the C ABI.
type Thumbnailer interface {
	// GetThumbnail returns a JPEG/PNG byte slice for `path`,
	// sized so that the long edge is approximately `sizePx` px.
	// Implementations should pick the smallest provider-supported
	// bucket >= sizePx (e.g. Dropbox's 32/64/128/256/480/640/960
	// /1024/2048 buckets).
	GetThumbnail(mountID int, path string, sizePx int) ([]byte, error)
}

// DriverFactory creates new Driver instances
type DriverFactory func() Driver

// Registry maintains the mapping of driver types to implementations
var registry = make(map[int]DriverFactory)

// RegisterDriver registers a driver type with its factory
// driverType: 1=FTP, 2=SFTP, 3=SMB, 4=Dropbox, 5=WebDAV, 6=GDrive, 7=S3, etc.
func RegisterDriver(driverType int, factory DriverFactory) {
	registry[driverType] = factory
}

// GetDriver creates a new Driver instance for the given type
func GetDriver(driverType int) (Driver, bool) {
	factory, ok := registry[driverType]
	if !ok {
		return nil, false
	}
	return factory(), true
}

// ListDriverTypes returns all registered driver type IDs
func ListDriverTypes() []int {
	types := make([]int, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}

// MountManager tracks active driver instances by mountID.
//
// It is safe for concurrent use. A host calls it from many threads at once,
// so the mount table is guarded by its own lock, which is held only while the
// table is read or changed and never across a network call: an operation on
// one mount never waits for another mount's I/O.
//
// An operation's lifetime is a lease from Acquire. Unmount, and a Mount that
// replaces an ID, take the mount out of the table at once, so no new lease is
// granted, then wait for the leases already granted to be released before
// they call the old driver's Unmount. A driver is therefore never torn down
// under an operation that is still using it.
type MountManager struct {
	mu     sync.RWMutex
	mounts map[int]*mountEntry
}

// mountEntry is one mounted driver. Operations hold inUse for reading for as
// long as they use the driver; teardown holds it for writing.
type mountEntry struct {
	mountID int
	driver  Driver
	inUse   sync.RWMutex
}

// retire waits for every lease on the entry to be released, then unmounts
// its driver. The entry is already out of the table, so no lease can follow.
func (e *mountEntry) retire() error {
	e.inUse.Lock()
	defer e.inUse.Unlock()
	return e.driver.Unmount(e.mountID)
}

func NewMountManager() *MountManager {
	return &MountManager{
		mounts: make(map[int]*mountEntry),
	}
}

// Mount creates a driver of driverType, mounts it, and records it under
// mountID. A mount already recorded under mountID is replaced, and then
// unmounted once its in-flight operations finish.
func (m *MountManager) Mount(mountID, driverType int, config map[string]string) error {
	driver, ok := GetDriver(driverType)
	if !ok {
		return ErrUnknownDriverType
	}
	if err := driver.Mount(mountID, config); err != nil {
		return err
	}
	m.mu.Lock()
	old := m.mounts[mountID]
	m.mounts[mountID] = &mountEntry{mountID: mountID, driver: driver}
	m.mu.Unlock()
	if old != nil {
		_ = old.retire()
	}
	return nil
}

// Unmount removes mountID, waits for its in-flight operations to finish, and
// unmounts its driver. Other mounts are not affected.
func (m *MountManager) Unmount(mountID int) error {
	m.mu.Lock()
	entry, ok := m.mounts[mountID]
	delete(m.mounts, mountID)
	m.mu.Unlock()
	if !ok {
		return ErrMountNotFound
	}
	return entry.retire()
}

// Acquire leases the driver mounted under mountID for one operation. The
// caller must call release when the operation is finished; until then the
// driver is not unmounted. ok is false when nothing is mounted under mountID,
// and release is then a no-op.
func (m *MountManager) Acquire(mountID int) (driver Driver, release func(), ok bool) {
	m.mu.RLock()
	entry := m.mounts[mountID]
	m.mu.RUnlock()
	if entry == nil {
		return nil, func() {}, false
	}
	entry.inUse.RLock()
	// The entry may have been retired between the lookup and the lease. Only
	// the entry still in the table may be used.
	m.mu.RLock()
	current := m.mounts[mountID] == entry
	m.mu.RUnlock()
	if !current {
		entry.inUse.RUnlock()
		return nil, func() {}, false
	}
	return entry.driver, entry.inUse.RUnlock, true
}

// Get returns the driver mounted under mountID without leasing it. Nothing
// stops it being unmounted while the caller uses it; an operation should use
// Acquire instead.
func (m *MountManager) Get(mountID int) (Driver, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.mounts[mountID]
	if !ok {
		return nil, false
	}
	return entry.driver, true
}

// Stats returns the per-mount transport counters for `mountID`, or
// nil when the mount doesn't exist or its driver doesn't implement
// StatsProvider (FTP/SFTP/SMB today). Callers should treat a nil
// return as "all zeros" — the C ABI does this so non-HTTP mounts
// surface harmless empty snapshots instead of errors. The counters are
// atomic, so reading them needs no lease.
func (m *MountManager) Stats(mountID int) *MountStats {
	d, ok := m.Get(mountID)
	if !ok {
		return nil
	}
	sp, ok := d.(StatsProvider)
	if !ok {
		return nil
	}
	return sp.Stats()
}

// Common errors
var (
	ErrUnknownDriverType = &DriverError{Code: 1, Message: "unknown driver type"}
	ErrMountNotFound     = &DriverError{Code: 2, Message: "mount not found"}
	ErrNotConnected      = &DriverError{Code: 3, Message: "not connected"}
	ErrPermissionDenied  = &DriverError{Code: 4, Message: "permission denied"}
	ErrNotFound          = &DriverError{Code: 5, Message: "file not found"}
	ErrExists            = &DriverError{Code: 6, Message: "file exists"}
)

// DriverError represents filesystem operation errors
type DriverError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
//
// Uses a value receiver so both `DriverError{...}` and `&DriverError{...}`
// satisfy `error`. A pointer receiver silently broke callers that returned
// a value type — the struct literal compiled fine at the call site but
// produced a non-error value.
func (e DriverError) Error() string {
	return e.Message
}
