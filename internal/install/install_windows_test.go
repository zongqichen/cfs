//go:build windows

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zongqichen/cloud-foundry-cli-contexts/internal/executable"
)

func TestWindowsSetupAndUninstallShim(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(root, "config", "config.json"))
	stateRoot := filepath.Join(root, "state")
	t.Setenv("CFS_STATE_HOME", stateRoot)
	shimDir := filepath.Join(root, "shims")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeCF := filepath.Join(root, "cf-real.exe")
	if err := os.WriteFile(fakeCF, []byte("synthetic cf"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Setup(SetupOptions{RealCFPath: fakeCF, ShimDir: shimDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.PathReady {
		t.Fatal("setup did not place the Windows shim first on PATH")
	}
	if info, err := os.Lstat(result.ShimPath); err != nil {
		t.Fatal(err)
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("shim mode = %s, want a regular executable copy", info.Mode())
	}
	self, err := executable.Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateShim(result.ShimPath, self); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shimManifestPath(result.ShimPath)); err != nil {
		t.Fatalf("shim manifest: %v", err)
	}

	stateFile := filepath.Join(stateRoot, "contexts", "preserve-me")
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("workspace state"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed != result.ShimPath {
		t.Fatalf("removed = %q, want %q", removed, result.ShimPath)
	}
	for _, path := range []string{result.ShimPath, shimManifestPath(result.ShimPath)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("shim artifact still exists at %s: %v", path, err)
		}
	}
	if raw, err := os.ReadFile(stateFile); err != nil {
		t.Fatalf("workspace state was removed: %v", err)
	} else if string(raw) != "workspace state" {
		t.Fatalf("workspace state changed: %q", raw)
	}
}

func TestWindowsShimCanBeUpdated(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "cf.exe")
	first := filepath.Join(root, "cfs-first.exe")
	second := filepath.Join(root, "cfs-second.exe")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installShim(shim, first); err != nil {
		t.Fatal(err)
	}
	if _, err := installShim(shim, second); err != nil {
		t.Fatal(err)
	}
	if err := validateShim(shim, second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "second" {
		t.Fatalf("updated shim = %q, want second", raw)
	}
}

func TestWindowsSetupRepairsMissingShimManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(root, "config", "config.json"))
	shimDir := filepath.Join(root, "shims")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeCF := filepath.Join(root, "cf-real.exe")
	if err := os.WriteFile(fakeCF, []byte("synthetic cf"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Setup(SetupOptions{RealCFPath: fakeCF, ShimDir: shimDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(shimManifestPath(first.ShimPath)); err != nil {
		t.Fatal(err)
	}

	second, err := Setup(SetupOptions{ShimDir: shimDir})
	if err != nil {
		t.Fatalf("repair setup: %v", err)
	}
	if second.RealCFPath != first.RealCFPath {
		t.Fatalf("repaired real CF path = %q, want %q", second.RealCFPath, first.RealCFPath)
	}
	self, err := executable.Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateShim(second.ShimPath, self); err != nil {
		t.Fatalf("repaired shim: %v", err)
	}
}

func TestWindowsSetupRefusesUnmanagedFile(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "cf.exe")
	target := filepath.Join(root, "cfs.exe")
	if err := os.WriteFile(shim, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("cfs"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := installShim(shim, target); err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("installShim() error = %v, want unmanaged-file refusal", err)
	}
	raw, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "do not replace" {
		t.Fatalf("existing file was modified: %q", raw)
	}
}

func TestWindowsShimRefusesModifiedManagedFile(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "cf.exe")
	target := filepath.Join(root, "cfs.exe")
	if err := os.WriteFile(target, []byte("cfs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installShim(shim, target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeShim(shim, target); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("removeShim() error = %v, want checksum refusal", err)
	}
	if _, err := installShim(shim, target); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("installShim() error = %v, want checksum refusal", err)
	}
	raw, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "modified" {
		t.Fatalf("modified shim was replaced: %q", raw)
	}
}
