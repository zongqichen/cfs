//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

var ErrBusy = errors.New("workspace is busy")

type Lock struct {
	file *os.File
}

func Acquire(path string, timeout time.Duration) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("set lock permissions: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if truncateErr := file.Truncate(0); truncateErr != nil {
				syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				file.Close()
				return nil, fmt.Errorf("truncate lock file: %w", truncateErr)
			}
			if _, seekErr := file.Seek(0, 0); seekErr != nil {
				syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				file.Close()
				return nil, fmt.Errorf("seek lock file: %w", seekErr)
			}
			if _, writeErr := fmt.Fprintf(file, "pid=%d\nstarted_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano)); writeErr != nil {
				syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				file.Close()
				return nil, fmt.Errorf("write lock metadata: %w", writeErr)
			}
			return &Lock{file: file}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return nil, fmt.Errorf("acquire workspace lock: %w", err)
		}
		if timeout <= 0 || time.Now().After(deadline) {
			file.Close()
			return nil, ErrBusy
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("release workspace lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close workspace lock: %w", closeErr)
	}
	return nil
}
