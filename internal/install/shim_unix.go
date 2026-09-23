//go:build !windows

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/executable"
)

func installShim(path, target string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		if err := validateShim(path, target); err != nil {
			return false, err
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect shim path: %w", err)
	}

	if err := os.Symlink(target, path); err != nil {
		return false, fmt.Errorf("install shim: %w", err)
	}
	return true, nil
}

func removeShim(path, target string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect shim: %w", err)
	}
	if err := validateShim(path, target); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove shim: %w", err)
	}
	return nil
}

func validateShim(path, target string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect shim: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("configured shim is not a symbolic link: %s", path)
	}
	existing, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve shim target: %w", err)
	}
	if !executable.Same(existing, target) {
		return fmt.Errorf("configured shim does not point to this cfs executable: %s", path)
	}
	return nil
}

func isManagedShim(string) (bool, error) {
	return false, nil
}

func matchesShimExecutable(path, target string) (bool, error) {
	return executable.Same(path, target), nil
}
