package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zongqichen/cfs/internal/envvar"
	"github.com/zongqichen/cfs/internal/securefs"
)

const (
	CurrentVersion  = 1
	applicationName = "cfs"
	configFileName  = "config.json"
	configReadLimit = 64 * 1024
)

var ErrNotConfigured = errors.New("cfs is not configured")

type Config struct {
	Version    int    `json:"version"`
	RealCFPath string `json:"real_cf_path"`
	ShimDir    string `json:"shim_dir"`
	StateDir   string `json:"state_dir,omitempty"`
}

func FilePath() (string, error) {
	if value := os.Getenv(envvar.ConfigFile); value != "" {
		return filepath.Abs(value)
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, applicationName, configFileName), nil
}

func StateRoot(cfg Config) (string, error) {
	if cfg.StateDir != "" {
		root, err := filepath.Abs(cfg.StateDir)
		if err != nil {
			return "", err
		}
		return validateStateRoot(root)
	}
	if value := os.Getenv(envvar.StateHome); value != "" {
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
		root = filepath.Join(home, "Library", "Application Support", applicationName)
	case "windows":
		base := os.Getenv(envvar.LocalAppData)
		if base == "" {
			var err error
			base, err = os.UserConfigDir()
			if err != nil {
				return "", fmt.Errorf("resolve local application data: %w", err)
			}
		}
		root = filepath.Join(base, applicationName)
	default:
		base := os.Getenv(envvar.XDGStateHome)
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve user home: %w", err)
			}
			base = filepath.Join(home, ".local", "state")
		}
		root = filepath.Join(base, applicationName)
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
	if err := securefs.RejectSymlink(clean); err != nil {
		return "", fmt.Errorf("refusing unsafe cfs state directory: %w", err)
	}
	canonical, err := securefs.CanonicalPath(clean)
	if err != nil {
		return "", fmt.Errorf("resolve cfs state directory: %w", err)
	}
	return canonical, nil
}

func DefaultShimDir() (string, error) {
	if value := os.Getenv(envvar.ShimDir); value != "" {
		return filepath.Abs(value)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".local", "share", applicationName, "shims"), nil
}

func Load() (Config, error) {
	path, err := FilePath()
	if err != nil {
		return Config{}, err
	}

	file, err := securefs.OpenPrivateFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotConfigured
	}
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, configReadLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return Config{}, errors.Join(fmt.Errorf("read configuration: %w", readErr), closeErr)
	}
	if closeErr != nil {
		return Config{}, fmt.Errorf("close configuration: %w", closeErr)
	}
	if len(raw) > configReadLimit {
		return Config{}, fmt.Errorf("configuration exceeds %d bytes", configReadLimit)
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

	if err := securefs.WriteJSONAtomic(path, cfg); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	return nil
}
