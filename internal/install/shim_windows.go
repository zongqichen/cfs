//go:build windows

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/securefs"
)

const (
	shimManifestVersion = 1
	shimManifestSuffix  = ".cfs-shim.json"
	shimManifestLimit   = 4 * 1024
)

type shimManifest struct {
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

func installShim(path, target string) (bool, error) {
	created := false
	if info, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		created = true
	} else if err != nil {
		return false, fmt.Errorf("inspect shim path: %w", err)
	} else if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing to replace non-regular shim: %s", path)
	} else {
		managed, managedErr := isManagedShim(path)
		current, currentErr := sameContents(path, target)
		if currentErr != nil {
			return false, currentErr
		}
		if !managed && !current {
			if managedErr != nil {
				return false, fmt.Errorf("refusing to replace invalid cfs shim %s: %w", path, managedErr)
			}
			return false, fmt.Errorf("refusing to replace unmanaged file: %s", path)
		}
		if current {
			if err := writeShimManifest(path); err != nil {
				return false, err
			}
			return false, nil
		}
	}

	if err := copyExecutable(target, path); err != nil {
		return false, err
	}
	if err := writeShimManifest(path); err != nil {
		if created {
			_ = os.Remove(path)
		}
		return false, err
	}
	return created, nil
}

func removeShim(path, target string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return removeManifestIfPresent(path)
	} else if err != nil {
		return fmt.Errorf("inspect shim: %w", err)
	}

	managed, managedErr := isManagedShim(path)
	current, currentErr := sameContents(path, target)
	if currentErr != nil {
		return currentErr
	}
	if !managed && !current {
		if managedErr != nil {
			return fmt.Errorf("refusing to remove invalid cfs shim %s: %w", path, managedErr)
		}
		return fmt.Errorf("refusing to remove unmanaged file: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove shim: %w", err)
	}
	if err := removeManifestIfPresent(path); err != nil {
		return err
	}
	return nil
}

func validateShim(path, target string) error {
	managed, err := isManagedShim(path)
	if err != nil {
		return fmt.Errorf("validate configured shim: %w", err)
	}
	if !managed {
		return fmt.Errorf("configured shim is not managed by cfs: %s", path)
	}
	current, err := sameContents(path, target)
	if err != nil {
		return err
	}
	if !current {
		return fmt.Errorf("configured shim is outdated; run 'cfs setup': %s", path)
	}
	return nil
}

func isManagedShim(path string) (bool, error) {
	manifest, err := readShimManifest(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	digest, err := fileDigest(path)
	if err != nil {
		return false, err
	}
	if digest != manifest.SHA256 {
		return false, errors.New("shim checksum does not match its manifest")
	}
	return true, nil
}

func writeShimManifest(path string) error {
	digest, err := fileDigest(path)
	if err != nil {
		return err
	}
	manifest := shimManifest{Version: shimManifestVersion, SHA256: digest}
	if err := securefs.WriteJSONAtomic(shimManifestPath(path), manifest); err != nil {
		return fmt.Errorf("write shim manifest: %w", err)
	}
	return nil
}

func readShimManifest(path string) (shimManifest, error) {
	manifestPath := shimManifestPath(path)
	file, err := securefs.OpenRegularFile(manifestPath)
	if err != nil {
		return shimManifest{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, shimManifestLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return shimManifest{}, errors.Join(fmt.Errorf("read %s: %w", manifestPath, readErr), closeErr)
	}
	if closeErr != nil {
		return shimManifest{}, fmt.Errorf("close %s: %w", manifestPath, closeErr)
	}
	if len(raw) > shimManifestLimit {
		return shimManifest{}, fmt.Errorf("shim manifest exceeds %d bytes", shimManifestLimit)
	}
	var manifest shimManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return shimManifest{}, fmt.Errorf("parse shim manifest: %w", err)
	}
	if manifest.Version != shimManifestVersion {
		return shimManifest{}, fmt.Errorf("unsupported shim manifest version %d", manifest.Version)
	}
	decoded, err := hex.DecodeString(manifest.SHA256)
	if err != nil || len(decoded) != sha256.Size {
		return shimManifest{}, errors.New("invalid shim checksum in manifest")
	}
	return manifest, nil
}

func sameContents(first, second string) (bool, error) {
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", first, err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", second, err)
	}
	if firstInfo.Size() != secondInfo.Size() {
		return false, nil
	}
	firstDigest, err := fileDigest(first)
	if err != nil {
		return false, err
	}
	secondDigest, err := fileDigest(second)
	if err != nil {
		return false, err
	}
	return firstDigest == secondDigest, nil
}

func matchesShimExecutable(path, target string) (bool, error) {
	return sameContents(path, target)
}

func fileDigest(path string) (string, error) {
	file, err := securefs.OpenRegularFile(path)
	if err != nil {
		return "", fmt.Errorf("open %s for checksum: %w", path, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", errors.Join(fmt.Errorf("checksum %s: %w", path, copyErr), closeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close %s: %w", path, closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyExecutable(source, destination string) error {
	sourceFile, err := securefs.OpenRegularFile(source)
	if err != nil {
		return fmt.Errorf("open cfs executable: %w", err)
	}
	defer sourceFile.Close()

	temporary, err := os.CreateTemp(filepath.Dir(destination), "cfs-shim-*.exe")
	if err != nil {
		return fmt.Errorf("create temporary shim: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := io.Copy(temporary, sourceFile); err != nil {
		return errors.Join(fmt.Errorf("copy cfs shim: %w", err), temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary shim: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("install shim: %w", err)
	}
	return nil
}

func removeManifestIfPresent(path string) error {
	if err := os.Remove(shimManifestPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove shim manifest: %w", err)
	}
	return nil
}

func shimManifestPath(path string) string {
	return path + shimManifestSuffix
}
