//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package executable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCanonicalizesExecutableSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "cf-real")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "cf")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRejectsNonExecutablePaths(t *testing.T) {
	nonExecutable := filepath.Join(t.TempDir(), "cf")
	if err := os.WriteFile(nonExecutable, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"directory":      t.TempDir(),
		"non-executable": nonExecutable,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(path); err == nil {
				t.Fatalf("Resolve(%q) error = nil", path)
			}
		})
	}
}
