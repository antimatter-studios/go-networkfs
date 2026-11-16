package api

import (
	"errors"
	"io"
	"testing"
)

// conditionalDriver records the conditional calls it receives.
type conditionalDriver struct {
	fakeDriver
	writer *uploadWriter
	calls  []string
	err    error
}

func (d *conditionalDriver) CreateFileIfMatch(_ int, path, revision string) (io.WriteCloser, error) {
	d.calls = append(d.calls, "create "+path+" "+revision)
	if d.err != nil {
		return nil, d.err
	}
	return d.writer, nil
}

func (d *conditionalDriver) RemoveIfMatch(_ int, path, revision string) error {
	d.calls = append(d.calls, "remove "+path+" "+revision)
	return d.err
}

func (d *conditionalDriver) RenameIfMatch(_ int, oldPath, newPath, oldRevision, newRevision string) error {
	d.calls = append(d.calls, "rename "+oldPath+" "+newPath+" "+oldRevision+" "+newRevision)
	return d.err
}

// A driver that cannot have a condition enforced is refused, and none of its
// unconditional operations is called in its place.
func TestConditionalOperationsRefuseDriversWithoutSupport(t *testing.T) {
	driver := &uploadDriver{err: errors.New("CreateFile must not be called")}
	for what, err := range map[string]error{
		"write":  WriteFileIfMatch(driver, 1, "/a", `"r"`, []byte("x")),
		"remove": RemoveIfMatch(driver, 1, "/a", `"r"`),
		"rename": RenameIfMatch(driver, 1, "/a", "/b", `"r"`, ""),
	} {
		if err != ErrConditionNotSupported {
			t.Errorf("%s: got %v, want ErrConditionNotSupported", what, err)
		}
	}
}

func TestConditionalOperationsReachTheDriver(t *testing.T) {
	writer := &uploadWriter{written: 1}
	driver := &conditionalDriver{writer: writer}
	if err := WriteFileIfMatch(driver, 1, "/a", `"r1"`, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfMatch(driver, 1, "/b", `"r2"`); err != nil {
		t.Fatal(err)
	}
	if err := RenameIfMatch(driver, 1, "/c", "/d", `"r3"`, `"r4"`); err != nil {
		t.Fatal(err)
	}
	want := []string{`create /a "r1"`, `remove /b "r2"`, `rename /c /d "r3" "r4"`}
	if len(driver.calls) != len(want) {
		t.Fatalf("calls %q; want %q", driver.calls, want)
	}
	for i := range want {
		if driver.calls[i] != want[i] {
			t.Errorf("call %d = %q; want %q", i, driver.calls[i], want[i])
		}
	}
	if writer.closes != 1 {
		t.Errorf("writer closed %d times; want 1", writer.closes)
	}
}

// A refusal reported by Close -- where a WebDAV save's MOVE happens -- comes
// back from WriteFileIfMatch intact, as does one from opening the writer.
func TestWriteFileIfMatchReportsRefusals(t *testing.T) {
	writer := &uploadWriter{written: 1, closeErr: ErrPreconditionFailed}
	if err := WriteFileIfMatch(&conditionalDriver{writer: writer}, 1, "/a", `"r"`, []byte("x")); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("close refusal: got %v", err)
	}
	if err := WriteFileIfMatch(&conditionalDriver{err: ErrRevisionRequired}, 1, "/a", `"r"`, []byte("x")); err != ErrRevisionRequired {
		t.Errorf("open refusal: got %v", err)
	}
	short := &uploadWriter{written: 0}
	if err := WriteFileIfMatch(&conditionalDriver{writer: short}, 1, "/a", `"r"`, []byte("x")); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("short write: got %v", err)
	}
}
