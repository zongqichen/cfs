package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv("CFS_CONFIG_FILE", path)
	want := Config{
		Version:    CurrentVersion,
		RealCFPath: "/opt/example/cf",
		ShimDir:    "/opt/example/shims",
		StateDir:   "/opt/example/state",
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadMissingConfiguration(t *testing.T) {
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := Load(); err != ErrNotConfigured {
		t.Fatalf("Load() error = %v, want ErrNotConfigured", err)
	}
}

func TestStateRootRejectsFilesystemRoot(t *testing.T) {
	root := filepath.VolumeName(string(os.PathSeparator)) + string(os.PathSeparator)
	if _, err := StateRoot(Config{StateDir: root}); err == nil {
		t.Fatal("StateRoot() error = nil, want unsafe path error")
	}
}
