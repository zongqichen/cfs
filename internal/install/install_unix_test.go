//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupAndUninstallShim(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(root, "config", "config.json"))
	t.Setenv("CFS_STATE_HOME", filepath.Join(root, "state"))
	shimDir := filepath.Join(root, "shims")
	fakeCF := filepath.Join(root, "cf-real")
	if err := os.WriteFile(fakeCF, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := Setup(SetupOptions{RealCFPath: fakeCF, ShimDir: shimDir})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(result.ShimPath); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("shim mode = %s, want symlink", info.Mode())
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed != result.ShimPath {
		t.Fatalf("removed = %q, want %q", removed, result.ShimPath)
	}
	if _, err := os.Lstat(result.ShimPath); !os.IsNotExist(err) {
		t.Fatalf("shim still exists: %v", err)
	}
}

func TestSetupRefusesToReplaceRegularFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(root, "config.json"))
	shimDir := filepath.Join(root, "shims")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(shimDir, "cf")
	if err := os.WriteFile(shimPath, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeCF := filepath.Join(root, "cf-real")
	if err := os.WriteFile(fakeCF, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := Setup(SetupOptions{RealCFPath: fakeCF, ShimDir: shimDir}); err == nil {
		t.Fatal("Setup() error = nil, want conflict error")
	}
	raw, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "do not replace" {
		t.Fatalf("existing file was modified: %q", raw)
	}
}
