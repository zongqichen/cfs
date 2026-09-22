package securefs

import (
	"encoding/json"
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
	if err := os.Chmod(path, DirectoryMode); err != nil {
		return fmt.Errorf("set private permissions on %s: %w", path, err)
	}
	return nil
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
		temporary.Close()
		return fmt.Errorf("set permissions on %s: %w", temporaryPath, err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
