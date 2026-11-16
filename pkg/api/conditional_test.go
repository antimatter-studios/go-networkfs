package api

import (
	"errors"
	"io"
	"testing"
)

// conditionalDriver records the conditional calls it receives.
type conditionalDriver struct {
	fakeDriver
	writer io.WriteCloser
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

// revisionWriter is a writer whose Close reports the revision of the state it
// wrote, as a driver's CreateFileIfMatch writer does when it can.
type revisionWriter struct {
	uploadWriter
	revision string
}

func (w *revisionWriter) Revision() string { return w.revision }

// A conditional write returns the revision its writer reports for the state
// it wrote, so the caller never has to stat for it and risk adopting another
// client's later edit (#84).
func TestWriteFileIfMatchRevisionReturnsTheWritersRevision(t *testing.T) {
	writer := &revisionWriter{uploadWriter: uploadWriter{written: 1}, revision: `"r2"`}
	revision, err := WriteFileIfMatchRevision(&conditionalDriver{writer: writer}, 1, "/a", `"r1"`, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if revision != `"r2"` {
		t.Fatalf("revision %q; want the writer's \"r2\"", revision)
	}
	if writer.closes != 1 {
		t.Fatalf("writer closed %d times; want 1", writer.closes)
	}
}

// A revision that is not known is empty, never guessed: a writer that reports
// none, a refused or failed write, and a driver that cannot enforce the
// condition all return "".
func TestWriteFileIfMatchRevisionReturnsNoneItCannotEstablish(t *testing.T) {
	plain := &uploadWriter{written: 1}
	if revision, err := WriteFileIfMatchRevision(&conditionalDriver{writer: plain}, 1, "/a", `"r"`, []byte("x")); err != nil || revision != "" {
		t.Errorf("writer without a revision: got %q, %v; want \"\", nil", revision, err)
	}
	refused := &revisionWriter{uploadWriter: uploadWriter{written: 1, closeErr: ErrPreconditionFailed}, revision: `"r2"`}
	if revision, err := WriteFileIfMatchRevision(&conditionalDriver{writer: refused}, 1, "/a", `"r"`, []byte("x")); !errors.Is(err, ErrPreconditionFailed) || revision != "" {
		t.Errorf("refused write: got %q, %v; want \"\", ErrPreconditionFailed", revision, err)
	}
	short := &revisionWriter{uploadWriter: uploadWriter{written: 0}, revision: `"r2"`}
	if revision, err := WriteFileIfMatchRevision(&conditionalDriver{writer: short}, 1, "/a", `"r"`, []byte("x")); !errors.Is(err, io.ErrShortWrite) || revision != "" {
		t.Errorf("short write: got %q, %v; want \"\", io.ErrShortWrite", revision, err)
	}
	if revision, err := WriteFileIfMatchRevision(&uploadDriver{}, 1, "/a", `"r"`, []byte("x")); err != ErrConditionNotSupported || revision != "" {
		t.Errorf("unsupported driver: got %q, %v; want \"\", ErrConditionNotSupported", revision, err)
	}
}
