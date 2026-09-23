package executable

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/pathutil"
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
	return pathutil.Equal(first, second)
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
