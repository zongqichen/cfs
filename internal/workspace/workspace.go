package workspace

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zongqichen/cfs/internal/envvar"
)

const (
	MarkerName         = ".cfs.toml"
	markerVersion      = "1"
	markerReadLimit    = 64 * 1024
	contextHashPrefix  = "cfs:v1\x00"
	missingFingerprint = "none"
)

var ErrNotFound = errors.New("no workspace could be resolved")

type Workspace struct {
	Root        string `json:"root"`
	Source      string `json:"source"`
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

func Resolve(cwd string) (Workspace, error) {
	if explicit := os.Getenv(envvar.WorkspaceRoot); explicit != "" {
		root, err := canonicalDirectory(explicit)
		if err != nil {
			return Workspace{}, fmt.Errorf("resolve %s: %w", envvar.WorkspaceRoot, err)
		}
		return identify(root, "environment"), nil
	}

	canonicalCWD, err := canonicalDirectory(cwd)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve current directory: %w", err)
	}

	if root, ok := findMarkerRoot(canonicalCWD); ok {
		if err := validateMarker(filepath.Join(root, MarkerName)); err != nil {
			return Workspace{}, err
		}
		return identify(root, "marker"), nil
	}

	if root, err := gitTopLevel(canonicalCWD); err == nil {
		return identify(root, "git-worktree"), nil
	}

	return Workspace{}, ErrNotFound
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// #nosec G703 -- Inspecting a caller-selected workspace root is intentional; it is canonicalized before use.
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(real), nil
}

func findMarkerRoot(start string) (string, bool) {
	current := start
	for {
		if info, err := os.Stat(filepath.Join(current, MarkerName)); err == nil && !info.IsDir() {
			return current, true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func validateMarker(path string) error {
	// #nosec G304 -- The marker belongs to the selected workspace and is read with a strict size and syntax limit.
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read workspace marker: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(io.LimitReader(file, markerReadLimit))
	foundVersion := false
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "version" {
			foundVersion = true
			if strings.TrimSpace(parts[1]) != markerVersion {
				return fmt.Errorf("unsupported workspace marker version in %s", path)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read workspace marker: %w", err)
	}
	if !foundVersion {
		return fmt.Errorf("workspace marker %s must contain 'version = %s'", path, markerVersion)
	}
	return nil
}

func gitTopLevel(cwd string) (string, error) {
	// #nosec G204 -- cwd is canonical and passed as an argv element without a shell.
	command := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	command.Stderr = nil
	raw, err := command.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(raw))
	if root == "" {
		return "", errors.New("git returned an empty worktree root")
	}
	return canonicalDirectory(root)
}

func identify(root, source string) Workspace {
	fingerprint := repositoryFingerprint(root)
	hash := sha256.New()
	hash.Write([]byte(contextHashPrefix))
	hash.Write([]byte(root))
	hash.Write([]byte{0})
	hash.Write([]byte(fingerprint))
	id := hex.EncodeToString(hash.Sum(nil))

	return Workspace{
		Root:        root,
		Source:      source,
		ID:          id,
		Fingerprint: fingerprint,
	}
}

func repositoryFingerprint(root string) string {
	// #nosec G204 G702 -- root is canonical and passed as an argv element without a shell.
	command := exec.Command("git", "-C", root, "rev-parse", "--absolute-git-dir")
	command.Stderr = nil
	raw, err := command.Output()
	if err != nil {
		return missingFingerprint
	}
	gitDir := strings.TrimSpace(string(raw))
	if gitDir == "" {
		return missingFingerprint
	}
	if real, err := filepath.EvalSymlinks(gitDir); err == nil {
		gitDir = real
	}
	sum := sha256.Sum256([]byte(filepath.Clean(gitDir)))
	return hex.EncodeToString(sum[:])
}
