package cfhome

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/pathutil"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/securefs"
)

const (
	configDirectory = ".cf"
	configFileName  = "config.json"
	configReadLimit = 4 * 1024 * 1024
)

func Default() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Abs(home)
}

func ConfigPath(home string) string {
	return filepath.Join(home, configDirectory, configFileName)
}

func HasConfig(home string) (bool, error) {
	path := ConfigPath(home)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect CF configuration: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing unsafe CF configuration: %s", path)
	}
	return true, nil
}

func HasTarget(home string) (bool, error) {
	raw, err := readConfig(ConfigPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var config struct {
		Target string
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return false, fmt.Errorf("parse CF configuration: %w", err)
	}
	return strings.TrimSpace(config.Target) != "", nil
}

func Import(sourceHome, destinationHome string) error {
	sourcePath := ConfigPath(sourceHome)
	destinationPath := ConfigPath(destinationHome)
	if pathutil.Equal(sourcePath, destinationPath) {
		return errors.New("source and destination CF homes are the same")
	}

	raw, err := readConfig(sourcePath)
	if err != nil {
		return err
	}
	if err := securefs.WriteFileAtomic(destinationPath, raw); err != nil {
		return fmt.Errorf("write workspace CF configuration: %w", err)
	}
	return nil
}

func readConfig(path string) ([]byte, error) {
	file, err := securefs.OpenRegularFile(path)
	if err != nil {
		return nil, fmt.Errorf("open CF configuration: %w", err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect CF configuration: %w", statErr)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("CF configuration permissions are too broad: %04o", info.Mode().Perm())
	}

	raw, readErr := io.ReadAll(io.LimitReader(file, configReadLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, errors.Join(fmt.Errorf("read CF configuration: %w", readErr), closeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close CF configuration: %w", closeErr)
	}
	if len(raw) > configReadLimit {
		return nil, fmt.Errorf("CF configuration exceeds %d bytes", configReadLimit)
	}
	if !json.Valid(raw) {
		return nil, errors.New("CF configuration is not valid JSON")
	}
	return raw, nil
}
