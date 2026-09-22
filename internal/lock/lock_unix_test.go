//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package lock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquirePreventsConcurrentOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := Acquire(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	if _, err := Acquire(path, 20*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("Acquire() error = %v, want ErrBusy", err)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path, 0)
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	second.Release()
}
