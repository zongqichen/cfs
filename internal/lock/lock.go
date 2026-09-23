package lock

import (
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrBusy = errors.New("workspace is busy")

const retryInterval = 50 * time.Millisecond

func writeMetadata(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate lock file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek lock file: %w", err)
	}
	if _, err := fmt.Fprintf(file, "pid=%d\nstarted_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("write lock metadata: %w", err)
	}
	return nil
}

func closeLockFile(file *os.File) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close workspace lock: %w", err)
	}
	return nil
}
