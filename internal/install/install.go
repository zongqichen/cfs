package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/config"
	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/executable"
)

const shimDirectoryMode = 0o755

type SetupOptions struct {
	RealCFPath string
	ShimDir    string
	StateDir   string
}

type SetupResult struct {
	RealCFPath string `json:"real_cf_path"`
	ShimPath   string `json:"shim_path"`
	ConfigPath string `json:"config_path"`
	PathReady  bool   `json:"path_ready"`
}

func Setup(options SetupOptions) (SetupResult, error) {
	self, err := executable.Current()
	if err != nil {
		return SetupResult{}, err
	}

	realCF, err := resolveRealCF(options.RealCFPath, self)
	if err != nil {
		return SetupResult{}, err
	}

	shimDir := options.ShimDir
	if shimDir == "" {
		shimDir, err = config.DefaultShimDir()
		if err != nil {
			return SetupResult{}, err
		}
	}
	shimDir, err = filepath.Abs(shimDir)
	if err != nil {
		return SetupResult{}, fmt.Errorf("resolve shim directory: %w", err)
	}
	if err := os.MkdirAll(shimDir, shimDirectoryMode); err != nil {
		return SetupResult{}, fmt.Errorf("create shim directory: %w", err)
	}

	stateDir := options.StateDir
	if stateDir != "" {
		stateDir, err = config.StateRoot(config.Config{StateDir: stateDir})
		if err != nil {
			return SetupResult{}, fmt.Errorf("resolve state directory: %w", err)
		}
	}

	shimPath := filepath.Join(shimDir, executable.Name("cf"))
	shimCreated, err := installShim(shimPath, self)
	if err != nil {
		return SetupResult{}, err
	}

	cfg := config.Config{
		Version:    config.CurrentVersion,
		RealCFPath: realCF,
		ShimDir:    shimDir,
		StateDir:   stateDir,
	}
	if err := config.Save(cfg); err != nil {
		if shimCreated {
			_ = removeShim(shimPath, self)
		}
		return SetupResult{}, err
	}
	configPath, err := config.FilePath()
	if err != nil {
		return SetupResult{}, err
	}

	return SetupResult{
		RealCFPath: realCF,
		ShimPath:   shimPath,
		ConfigPath: configPath,
		PathReady:  pathSelectsShim(shimPath),
	}, nil
}

func Uninstall() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	shimPath := filepath.Join(cfg.ShimDir, executable.Name("cf"))
	self, err := executable.Current()
	if err != nil {
		return "", err
	}
	if err := removeShim(shimPath, self); err != nil {
		return "", err
	}
	return shimPath, nil
}

func resolveRealCF(explicit, self string) (string, error) {
	candidate := explicit
	if candidate == "" {
		var err error
		candidate, err = exec.LookPath("cf")
		if err != nil {
			if cfg, loadErr := config.Load(); loadErr == nil {
				candidate = cfg.RealCFPath
			} else {
				return "", errors.New("official CF CLI not found; install it or pass --real-cf")
			}
		}
	}

	real, err := executable.Resolve(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve official CF CLI: %w", err)
	}
	managedShim, managedErr := isManagedShim(real)
	currentShim, currentErr := matchesShimExecutable(real, self)
	if currentErr != nil {
		return "", fmt.Errorf("inspect discovered cf command: %w", currentErr)
	}
	if managedErr != nil && !currentShim {
		return "", fmt.Errorf("inspect discovered cf command: %w", managedErr)
	}
	if currentShim || managedShim {
		if cfg, loadErr := config.Load(); loadErr == nil && !executable.Same(cfg.RealCFPath, self) {
			return validateExecutable(cfg.RealCFPath)
		}
		return "", errors.New("the discovered cf command is the cfs shim; pass --real-cf with the official CLI path")
	}
	return validateExecutable(real)
}

func validateExecutable(path string) (string, error) {
	real, err := executable.Resolve(path)
	if err != nil {
		return "", fmt.Errorf("validate official CF CLI: %w", err)
	}
	return real, nil
}

func ValidateShim(path, target string) error {
	return validateShim(path, target)
}

func pathSelectsShim(shimPath string) bool {
	resolved, err := exec.LookPath("cf")
	return err == nil && executable.Same(resolved, shimPath)
}
