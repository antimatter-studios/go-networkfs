package api

import (
	"errors"
	"io"
	"testing"
)

type uploadDriver struct {
	Driver
	writer io.WriteCloser
	err    error
}

func (driver uploadDriver) CreateFile(int, string) (io.WriteCloser, error) {
	return driver.writer, driver.err
}

type uploadWriter struct {
	written  int
	writeErr error
	closeErr error
	closes   int
}

func (writer *uploadWriter) Write([]byte) (int, error) {
	return writer.written, writer.writeErr
}

func (writer *uploadWriter) Close() error {
	writer.closes++
	return writer.closeErr
}

func TestWriteFileReportsUploadFailures(t *testing.T) {
	failed := errors.New("upload rejected")
	for _, scenario := range []struct {
		name   string
		writer uploadWriter
		want   error
	}{
		{"success", uploadWriter{written: 4}, nil},
		{"close failure", uploadWriter{written: 4, closeErr: failed}, failed},
		{"short write", uploadWriter{written: 2}, io.ErrShortWrite},
		{"write failure", uploadWriter{written: 2, writeErr: failed}, failed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			writer := scenario.writer
			err := WriteFile(uploadDriver{writer: &writer}, 1, "/save", []byte("data"))
			if !errors.Is(err, scenario.want) {
				t.Fatalf("got %v, want %v", err, scenario.want)
			}
			if writer.closes != 1 {
				t.Fatalf("closed %d times, want once", writer.closes)
			}
		})
	}
}
