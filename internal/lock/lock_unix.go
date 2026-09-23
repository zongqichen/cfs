//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/securefs"
)

type Lock struct {
	file *os.File
}

func Acquire(path string, timeout time.Duration) (*Lock, error) {
	file, err := securefs.OpenPrivateFile(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if metadataErr := writeMetadata(file); metadataErr != nil {
				return nil, errors.Join(metadataErr, unlockAndClose(file))
			}
			return &Lock{file: file}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
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
	err := unlockAndClose(l.file)
	l.file = nil
	return err
}

func unlockAndClose(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("release workspace lock: %w", unlockErr)
	}
	return errors.Join(unlockErr, closeLockFile(file))
}
