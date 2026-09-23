package securefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirectoryMode = 0o700
	FileMode      = 0o600
)

func EnsureDirectory(path string) error {
	if err := os.MkdirAll(path, DirectoryMode); err != nil {
		return fmt.Errorf("create private directory %s: %w", path, err)
	}
	directory, err := openDirectory(path)
	if err != nil {
		return err
	}
	if err := directory.Chmod(DirectoryMode); err != nil {
		_ = directory.Close()
		return fmt.Errorf("set private permissions on %s: %w", path, err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close private directory %s: %w", path, err)
	}
	return nil
}

func ValidateDirectory(path string) error {
	directory, err := openDirectory(path)
	if err != nil {
		return err
	}
	info, statErr := directory.Stat()
	if statErr != nil {
		_ = directory.Close()
		return fmt.Errorf("inspect private directory %s: %w", path, statErr)
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = directory.Close()
		return fmt.Errorf("private directory permissions are too broad on %s: %04o", path, info.Mode().Perm())
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close private directory %s: %w", path, err)
	}
	return nil
}

func RejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symbolic link: %s", path)
	}
	return nil
}

// CanonicalPath resolves every existing path component and preserves a missing
// suffix. Callers can create the returned path without traversing a parent
// symbolic link.
func CanonicalPath(path string) (string, error) {
	current := filepath.Clean(path)
	missing := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve path %s: %w", path, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve path %s: no existing ancestor", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func WriteJSONAtomic(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	raw = append(raw, '\n')

	directory := filepath.Dir(path)
	if err := EnsureDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "cfs-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(FileMode); err != nil {
		return errors.Join(fmt.Errorf("set permissions on %s: %w", temporaryPath, err), closeFile(temporary))
	}
	if _, err := temporary.Write(raw); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", temporaryPath, err), closeFile(temporary))
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func closeFile(file *os.File) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", file.Name(), err)
	}
	return nil
}
