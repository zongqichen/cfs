package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const CurrentVersion = 1

var ErrNotConfigured = errors.New("cfs is not configured")

type Config struct {
	Version    int    `json:"version"`
	RealCFPath string `json:"real_cf_path"`
	ShimDir    string `json:"shim_dir"`
	StateDir   string `json:"state_dir,omitempty"`
}

func FilePath() (string, error) {
	if value := os.Getenv("CFS_CONFIG_FILE"); value != "" {
		return filepath.Abs(value)
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "cfs", "config.json"), nil
}

func StateRoot(cfg Config) (string, error) {
	if cfg.StateDir != "" {
		root, err := filepath.Abs(cfg.StateDir)
		if err != nil {
			return "", err
		}
		return validateStateRoot(root)
	}
	if value := os.Getenv("CFS_STATE_HOME"); value != "" {
		root, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		return validateStateRoot(root)
	}

	var root string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		root = filepath.Join(home, "Library", "Application Support", "cfs")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			var err error
			base, err = os.UserConfigDir()
			if err != nil {
				return "", fmt.Errorf("resolve local application data: %w", err)
			}
		}
		root = filepath.Join(base, "cfs")
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve user home: %w", err)
			}
			base = filepath.Join(home, ".local", "state")
		}
		root = filepath.Join(base, "cfs")
	}

	return validateStateRoot(root)
}

func validateStateRoot(root string) (string, error) {
	clean := filepath.Clean(root)
	volumeRoot := filepath.Clean(filepath.VolumeName(clean) + string(os.PathSeparator))
	if clean == volumeRoot {
		return "", fmt.Errorf("refusing to use filesystem root as cfs state directory: %s", clean)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		home, _ = filepath.Abs(home)
		if clean == filepath.Clean(home) {
			return "", fmt.Errorf("refusing to use the user home as cfs state directory: %s", clean)
		}
	}
	if temporary := filepath.Clean(os.TempDir()); clean == temporary {
		return "", fmt.Errorf("refusing to use the shared temporary directory as cfs state directory: %s", clean)
	}
	return clean, nil
}

func DefaultShimDir() (string, error) {
	if value := os.Getenv("CFS_SHIM_DIR"); value != "" {
		return filepath.Abs(value)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "cfs", "shims"), nil
}

func Load() (Config, error) {
	path, err := FilePath()
	if err != nil {
		return Config{}, err
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotConfigured
	}
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}
	if cfg.Version != CurrentVersion {
		return Config{}, fmt.Errorf("unsupported configuration version %d", cfg.Version)
	}
	if cfg.RealCFPath == "" {
		return Config{}, errors.New("configuration does not contain real_cf_path")
	}
	return cfg, nil
}

func Save(cfg Config) error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set configuration permissions: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}
