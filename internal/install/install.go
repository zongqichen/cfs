package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zongqichen/cfs/internal/config"
)

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
	self, err := canonicalExecutable()
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
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return SetupResult{}, fmt.Errorf("create shim directory: %w", err)
	}

	stateDir := options.StateDir
	if stateDir != "" {
		stateDir, err = filepath.Abs(stateDir)
		if err != nil {
			return SetupResult{}, fmt.Errorf("resolve state directory: %w", err)
		}
	}

	shimName := "cf"
	if runtime.GOOS == "windows" {
		shimName = "cf.exe"
	}
	shimPath := filepath.Join(shimDir, shimName)
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
			_ = os.Remove(shimPath)
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
	shimName := "cf"
	if runtime.GOOS == "windows" {
		shimName = "cf.exe"
	}
	shimPath := filepath.Join(cfg.ShimDir, shimName)

	info, err := os.Lstat(shimPath)
	if errors.Is(err, os.ErrNotExist) {
		return shimPath, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect shim: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("refusing to remove non-symlink at %s", shimPath)
	}
	target, err := filepath.EvalSymlinks(shimPath)
	if err != nil {
		return "", fmt.Errorf("resolve shim target: %w", err)
	}
	self, err := canonicalExecutable()
	if err != nil {
		return "", err
	}
	if !samePath(target, self) {
		return "", fmt.Errorf("refusing to remove shim owned by another executable: %s", shimPath)
	}
	if err := os.Remove(shimPath); err != nil {
		return "", fmt.Errorf("remove shim: %w", err)
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

	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve official CF CLI path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve official CF CLI: %w", err)
	}
	if samePath(real, self) {
		if cfg, loadErr := config.Load(); loadErr == nil && !samePath(cfg.RealCFPath, self) {
			return validateExecutable(cfg.RealCFPath)
		}
		return "", errors.New("the discovered cf command is the cfs shim; pass --real-cf with the official CLI path")
	}
	return validateExecutable(real)
}

func validateExecutable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve executable %s: %w", abs, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect executable %s: %w", real, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("official CF CLI is not a regular file: %s", real)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("official CF CLI is not executable: %s", real)
	}
	return real, nil
}

func installShim(path, target string) (bool, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return false, fmt.Errorf("refusing to replace existing non-symlink: %s", path)
		}
		existing, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr == nil && samePath(existing, target) {
			return false, nil
		}
		return false, fmt.Errorf("refusing to replace existing shim: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect shim path: %w", err)
	}

	if err := os.Symlink(target, path); err != nil {
		return false, fmt.Errorf("install shim: %w", err)
	}
	return true, nil
}

func canonicalExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve cfs executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve cfs executable symlinks: %w", err)
	}
	return filepath.Clean(real), nil
}

func samePath(first, second string) bool {
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	if firstErr != nil || secondErr != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(firstAbs), filepath.Clean(secondAbs))
	}
	return filepath.Clean(firstAbs) == filepath.Clean(secondAbs)
}

func pathSelectsShim(shimPath string) bool {
	resolved, err := exec.LookPath("cf")
	return err == nil && samePath(resolved, shimPath)
}
