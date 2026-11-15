package api

import (
	"errors"
	"io"
)

func WriteFile(driver Driver, mountID int, path string, data []byte) error {
	writer, err := driver.CreateFile(mountID, path)
	if err != nil {
		return err
	}
	written, writeErr := writer.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, writer.Close())
}
