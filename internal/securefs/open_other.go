//go:build !darwin && !linux

package securefs

import (
	"fmt"
	"os"
)

func OpenRegularFile(path string) (*os.File, error) {
	return openFileNoFollow(path, os.O_RDONLY, 0)
}

func OpenPrivateFile(path string, flags int) (*os.File, error) {
	file, err := openFileNoFollow(path, flags, FileMode)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(FileMode); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set private permissions on %s: %w", path, err)
	}
	return file, nil
}

func openFileNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	if err := RejectSymlink(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, flags, mode)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedPath(path, file, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openDirectory(path string) (*os.File, error) {
	if err := RejectSymlink(path); err != nil {
		return nil, err
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open private directory %s: %w", path, err)
	}
	if err := validateOpenedPath(path, directory, true); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func validateOpenedPath(path string, file *os.File, wantDirectory bool) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened path %s: %w", path, err)
	}
	correctType := openedInfo.Mode().IsRegular()
	if wantDirectory {
		correctType = openedInfo.IsDir()
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !correctType || !os.SameFile(pathInfo, openedInfo) {
		return fmt.Errorf("refusing unsafe path: %s", path)
	}
	return nil
}
