// Package privatefile bounds reads of local configuration and session files.
package privatefile

import (
	"fmt"
	"io"
	"os"
)

// Read rejects links and special files on Unix without blocking on a FIFO.
func Read(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d MiB", limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d MiB", limit>>20)
	}
	return data, nil
}
