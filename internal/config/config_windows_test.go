//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDefaultDirectoriesUseLocalAppData(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("CFS_SHIM_DIR", "")
	t.Setenv("CFS_STATE_HOME", "")

	shimDir, err := DefaultShimDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(localAppData, "cfs", "shims"); shimDir != want {
		t.Fatalf("DefaultShimDir() = %q, want %q", shimDir, want)
	}

	stateRoot, err := StateRoot(Config{})
	if err != nil {
		t.Fatal(err)
	}
	canonicalLocalAppData, err := filepath.EvalSymlinks(localAppData)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(canonicalLocalAppData, "cfs"); stateRoot != want {
		t.Fatalf("StateRoot() = %q, want %q", stateRoot, want)
	}
	if _, err := os.Stat(localAppData); err != nil {
		t.Fatal(err)
	}
}
