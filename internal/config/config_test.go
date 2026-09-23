package config

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestLoadRejectsSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CFS_CONFIG_FILE", link)

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want symbolic-link rejection")
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o644 {
		t.Fatalf("target permissions = %o, want 644", info.Mode().Perm())
	}
}

func TestStateRootRejectsFilesystemRoot(t *testing.T) {
	root := filepath.VolumeName(string(os.PathSeparator)) + string(os.PathSeparator)
	if _, err := StateRoot(Config{StateDir: root}); err == nil {
		t.Fatal("StateRoot() error = nil, want unsafe path error")
	}
}

func TestStateRootRejectsSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := StateRoot(Config{StateDir: link}); err == nil {
		t.Fatal("StateRoot() error = nil, want symbolic-link rejection")
	}
}

func TestStateRootCanonicalizesParentSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation requires additional privileges on Windows")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := StateRoot(Config{StateDir: filepath.Join(link, "state")})
	if err != nil {
		t.Fatal(err)
	}
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalTarget, "state")
	if got != want {
		t.Fatalf("StateRoot() = %q, want %q", got, want)
	}
}
