//go:build windows

package lock

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/zongqichen/cfs/internal/securefs"
	"golang.org/x/sys/windows"
)

const lockedBytes = 1

type Lock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func Acquire(path string, timeout time.Duration) (*Lock, error) {
	file, err := securefs.OpenPrivateFile(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	lock := &Lock{file: file}
	deadline := time.Now().Add(timeout)
	for {
		err = windows.LockFileEx(
			windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			lockedBytes,
			0,
			&lock.overlapped,
		)
		if err == nil {
			if metadataErr := writeMetadata(file); metadataErr != nil {
				return nil, errors.Join(metadataErr, lock.releaseAndClose())
			}
			return lock, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.Join(fmt.Errorf("acquire workspace lock: %w", err), closeLockFile(file))
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return nil, errors.Join(ErrBusy, closeLockFile(file))
		}
		time.Sleep(retryInterval)
	}
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.releaseAndClose()
	l.file = nil
	return err
}

func (l *Lock) releaseAndClose() error {
	unlockErr := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, lockedBytes, 0, &l.overlapped)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("release workspace lock: %w", unlockErr)
	}
	return errors.Join(unlockErr, closeLockFile(l.file))
}
