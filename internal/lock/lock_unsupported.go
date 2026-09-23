//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows)

package lock

import (
	"time"
)

type Lock struct{}

func Acquire(_ string, _ time.Duration) (*Lock, error) {
	return nil, errors.New("workspace locking is not supported on this platform")
}

func (l *Lock) Release() error {
	return nil
}
