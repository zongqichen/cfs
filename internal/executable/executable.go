package executable

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func Name(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func Current() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	return canonical(path)
}

func Resolve(path string) (string, error) {
	real, err := canonical(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect executable %s: %w", real, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("executable is not a regular file: %s", real)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("file is not executable: %s", real)
	}
	return real, nil
}

func Same(first, second string) bool {
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	if firstErr != nil || secondErr != nil {
		return false
	}
	firstAbs = filepath.Clean(firstAbs)
	secondAbs = filepath.Clean(secondAbs)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(firstAbs, secondAbs)
	}
	return firstAbs == secondAbs
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks for %s: %w", abs, err)
	}
	return filepath.Clean(real), nil
}
