//go:build darwin || linux

package securefs

import (
	"fmt"
	"os"
	"syscall"
)

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
	file, err := openNoFollow(path, flags|syscall.O_CLOEXEC|syscall.O_NONBLOCK, mode)
	if err != nil {
		return nil, fmt.Errorf("open file without following links %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("refusing unsafe file: %s", path)
	}
	return file, nil
}

func openDirectory(path string) (*os.File, error) {
	directory, err := openNoFollow(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open private directory %s: %w", path, err)
	}
	return directory, nil
}

func openNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	descriptor, err := syscall.Open(path, flags|syscall.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), path), nil
}
