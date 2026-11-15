package api

import (
	"sync/atomic"
	"testing"
	"time"
)

// leaseDriver records when it is unmounted.
type leaseDriver struct {
	statsDriver
	unmounted atomic.Bool
}

func (d *leaseDriver) Unmount(int) error {
	d.unmounted.Store(true)
	return nil
}

func mountLeaseDrivers(t *testing.T, ids ...int) (*MountManager, map[int]*leaseDriver) {
	t.Helper()
	resetRegistry(t)
	const driverType = 9070
	made := make(chan *leaseDriver, len(ids)+1)
	RegisterDriver(driverType, func() Driver {
		d := &leaseDriver{}
		made <- d
		return d
	})
	m := NewMountManager()
	drivers := make(map[int]*leaseDriver)
	for _, id := range ids {
		if err := m.Mount(id, driverType, nil); err != nil {
			t.Fatal(err)
		}
		drivers[id] = <-made
	}
	return m, drivers
}

// Unmount waits for an operation in flight on the same mount, and refuses new
// ones at once, while other mounts carry on untouched.
func TestUnmountWaitsForLeasedOperations(t *testing.T) {
	m, drivers := mountLeaseDrivers(t, 1, 2)

	_, release, ok := m.Acquire(1)
	if !ok {
		t.Fatal("acquire 1")
	}

	done := make(chan error, 1)
	go func() { done <- m.Unmount(1) }()

	// Unmount takes the mount out of the table before it waits.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := m.Get(1); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unmount never removed mount 1 from the table")
		}
		time.Sleep(time.Millisecond)
	}
	if _, rel, ok := m.Acquire(1); ok {
		rel()
		t.Fatal("a new lease was granted on a mount being unmounted")
	}

	// A different mount is not held up by mount 1's teardown.
	if _, rel, ok := m.Acquire(2); !ok {
		t.Fatal("mount 2 could not be leased while mount 1 was unmounting")
	} else {
		rel()
	}

	select {
	case <-done:
		t.Fatal("unmount finished while an operation still held the mount")
	case <-time.After(50 * time.Millisecond):
	}
	if drivers[1].unmounted.Load() {
		t.Fatal("the driver was unmounted under a leased operation")
	}

	release()
	if err := <-done; err != nil {
		t.Fatalf("unmount: %v", err)
	}
	if !drivers[1].unmounted.Load() {
		t.Fatal("the driver was not unmounted after the lease was released")
	}
	if drivers[2].unmounted.Load() {
		t.Fatal("unmounting mount 1 unmounted mount 2")
	}
}

// Mounting an ID again replaces its driver, and the old driver is unmounted
// once its operations finish.
func TestRemountReplacesAndRetiresTheOldDriver(t *testing.T) {
	m, drivers := mountLeaseDrivers(t, 1)
	old, release, ok := m.Acquire(1)
	if !ok {
		t.Fatal("acquire 1")
	}

	done := make(chan error, 1)
	go func() { done <- m.Mount(1, 9070, nil) }()

	select {
	case <-done:
		t.Fatal("remount finished while the old driver was still leased")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !drivers[1].unmounted.Load() {
		t.Fatal("the replaced driver was not unmounted")
	}
	current, rel, ok := m.Acquire(1)
	if !ok {
		t.Fatal("ID 1 is not mounted after the remount")
	}
	rel()
	if current == old {
		t.Fatal("the remount kept the old driver")
	}
}

func TestAcquireUnknownMountFails(t *testing.T) {
	m := NewMountManager()
	_, release, ok := m.Acquire(42)
	if ok {
		t.Fatal("acquired a mount that does not exist")
	}
	release()
	if err := m.Unmount(42); err != ErrMountNotFound {
		t.Fatalf("unmount of an unknown ID: %v", err)
	}
}
