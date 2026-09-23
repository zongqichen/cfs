//go:build windows

package lock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsLockPreventsConcurrentOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.lock")
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
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}
