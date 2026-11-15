package api

import (
	"io"
	"sync"
	"testing"
)

// statsDriver is a Driver whose instances share nothing, so any race the
// detector reports below is in MountManager, not in the driver.
type statsDriver struct{ stats MountStats }

func (d *statsDriver) Name() string                                { return "stats" }
func (d *statsDriver) Mount(int, map[string]string) error          { return nil }
func (d *statsDriver) Unmount(int) error                           { return nil }
func (d *statsDriver) Stat(int, string) (FileInfo, error)          { return FileInfo{}, nil }
func (d *statsDriver) ListDir(int, string) ([]FileInfo, error)     { return nil, nil }
func (d *statsDriver) OpenFile(int, string) (io.ReadCloser, error) { return nil, nil }
func (d *statsDriver) CreateFile(int, string) (io.WriteCloser, error) {
	return nil, nil
}
func (d *statsDriver) Mkdir(int, string) error          { return nil }
func (d *statsDriver) Remove(int, string) error         { return nil }
func (d *statsDriver) Rename(int, string, string) error { return nil }
func (d *statsDriver) Stats() *MountStats               { return &d.stats }

// Mount state is shared by every mount ID (#69).
//
// The unified C ABI keeps one MountManager, and a host calls it from many
// threads at once. Mount and Unmount of one ID while another ID is read must
// not race: run under -race, as every test tier here is, the detector reports
// any unsynchronised access to the mount table. ID 1 must stay mounted and
// readable throughout, and ID 2 must end unmounted.
func TestMountManagerLifecycleIsConcurrencySafe(t *testing.T) {
	resetRegistry(t)
	const driverType = 9069
	RegisterDriver(driverType, func() Driver { return &statsDriver{} })

	m := NewMountManager()
	if err := m.Mount(1, driverType, nil); err != nil {
		t.Fatal(err)
	}

	const rounds = 2000
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := m.Mount(2, driverType, nil); err != nil {
				t.Errorf("mount 2: %v", err)
				return
			}
			if err := m.Unmount(2); err != nil {
				t.Errorf("unmount 2: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			d, ok := m.Get(1)
			if !ok {
				t.Error("mount 1 vanished while mount 2 cycled")
				return
			}
			if _, err := d.Stat(1, "/"); err != nil {
				t.Errorf("stat through mount 1: %v", err)
				return
			}
			if m.Stats(1) == nil {
				t.Error("mount 1 lost its stats while mount 2 cycled")
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			m.Get(2)
			m.Stats(2)
		}
	}()
	wg.Wait()

	if _, ok := m.Get(1); !ok {
		t.Error("mount 1 is gone")
	}
	if _, ok := m.Get(2); ok {
		t.Error("mount 2 is still mounted after its last unmount")
	}
}
