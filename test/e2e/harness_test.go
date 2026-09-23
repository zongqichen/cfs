//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	exitUsage              = 64
	exitUnavailable        = 69
	exitTemporary          = 75
	commandTimeout         = 20 * time.Second
	installationTimeout    = 2 * time.Minute
	requestBarrierTimeout  = 10 * time.Second
	processStartupTimeout  = 5 * time.Second
	processShutdownTimeout = 5 * time.Second
)

type commandResult struct {
	stdout string
	stderr string
	code   int
}

type testEnvironment struct {
	repoRoot string
	root     string
	home     string
	state    string
	shimDir  string
	realCF   string
	cfs      string
	env      []string
}

func newInstalledTestEnvironment(t *testing.T) testEnvironment {
	t.Helper()
	repoRoot := repositoryRoot(t)
	root := t.TempDir()
	binDir := makeDirectory(t, filepath.Join(root, "bin"))
	officialDir := makeDirectory(t, filepath.Join(root, "official-bin"))
	shimDir := filepath.Join(root, "shims")
	home := makeDirectory(t, filepath.Join(root, "home"))
	state := filepath.Join(root, "state")
	temporary := makeDirectory(t, filepath.Join(root, "tmp"))

	officialCF := filepath.Join(officialDir, executableName("cf"))
	if err := copyFile(resolveOfficialCF(t), officialCF); err != nil {
		t.Fatalf("copy official cf: %v", err)
	}
	cfs := installCFS(t, repoRoot, binDir)
	testPath := strings.Join([]string{shimDir, officialDir, binDir, os.Getenv("PATH")}, string(os.PathListSeparator))
	env := isolatedProcessEnvironment(root, home, state, shimDir, temporary, testPath)
	t.Setenv("PATH", testPath)

	return testEnvironment{
		repoRoot: repoRoot, root: root, home: home, state: state, shimDir: shimDir,
		realCF: officialCF, cfs: cfs, env: env,
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve E2E source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func resolveOfficialCF(t *testing.T) string {
	t.Helper()
	configured := os.Getenv("CFS_REAL_CF")
	if configured == "" {
		t.Fatal("CFS_REAL_CF must point to an official cf CLI binary")
	}
	resolved, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatalf("resolve CFS_REAL_CF: %v", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		t.Fatalf("make CFS_REAL_CF absolute: %v", err)
	}
	if strings.EqualFold(filepath.Base(resolved), executableName("cfs")) {
		t.Fatal("CFS_REAL_CF resolves to the cfs shim; provide the official CLI binary")
	}
	return resolved
}

func installCFS(t *testing.T, repoRoot, binDir string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), installationTimeout)
	defer cancel()
	install := exec.CommandContext(ctx, "go", "install", "./cmd/cfs")
	install.Dir = repoRoot
	install.Env = replaceEnvironment(os.Environ(), map[string]string{"GOBIN": binDir})
	if output, err := install.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("install cfs into E2E environment timed out after %s", installationTimeout)
		}
		t.Fatalf("install cfs into E2E environment: %v: %s", err, output)
	}
	return filepath.Join(binDir, executableName("cfs"))
}

func isolatedProcessEnvironment(root, userHome, state, shimDir, temporary, testPath string) []string {
	values := map[string]string{
		"HOME":                userHome,
		"USERPROFILE":         userHome,
		"PATH":                testPath,
		"TMPDIR":              temporary,
		"TEMP":                temporary,
		"TMP":                 temporary,
		"APPDATA":             filepath.Join(root, "appdata", "roaming"),
		"LOCALAPPDATA":        filepath.Join(root, "appdata", "local"),
		"XDG_CONFIG_HOME":     filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME":      filepath.Join(root, "xdg-state"),
		"GIT_CONFIG_GLOBAL":   filepath.Join(root, "gitconfig"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"CFS_CONFIG_FILE":     filepath.Join(root, "config", "config.json"),
		"CFS_STATE_HOME":      state,
		"CFS_SHIM_DIR":        shimDir,
		"CF_COLOR":            "false",
		"LANG":                "C",
		"LC_ALL":              "C",
		"NO_PROXY":            "127.0.0.1,localhost",
		"no_proxy":            "127.0.0.1,localhost",
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(userHome)
		values["HOMEDRIVE"] = volume
		values["HOMEPATH"] = strings.TrimPrefix(userHome, volume)
	}
	base := removeEnvironmentWithPrefixes(os.Environ(), "CF_", "CFS_")
	base = removeEnvironment(base,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CEILING_DIRECTORIES", "GIT_COMMON_DIR",
		"GIT_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_WORK_TREE",
	)
	return replaceEnvironment(base, values)
}

func (e testEnvironment) run(t *testing.T, executable, directory string, overrides map[string]string, args ...string) commandResult {
	t.Helper()
	return e.runWithInput(t, executable, directory, overrides, "", args...)
}

func (e testEnvironment) runWithInput(t *testing.T, executable, directory string, overrides map[string]string, input string, args ...string) commandResult {
	t.Helper()
	result := runCommandWithInput(executable, directory, replaceEnvironment(e.env, overrides), input, args...)
	if result.code == -1 {
		t.Fatalf("start command %s: %s", filepath.Base(executable), result.stderr)
	}
	return result
}

func runCommand(executable, directory string, env []string, args ...string) commandResult {
	return runCommandWithInput(executable, directory, env, "", args...)
}

func runCommandWithInput(executable, directory string, env []string, input string, args ...string) commandResult {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = env
	command.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	if ctx.Err() != nil {
		result.code = -1
		result.stderr = "command timed out after " + commandTimeout.String()
		return result
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.code = exitError.ExitCode()
		return result
	}
	result.code = -1
	result.stderr = err.Error()
	return result
}

func markerWorkspace(t *testing.T, root, name string) string {
	t.Helper()
	workspace := makeDirectory(t, filepath.Join(root, "workspaces", name))
	if err := os.WriteFile(filepath.Join(workspace, ".cfs.toml"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatalf("write workspace marker: %v", err)
	}
	return workspace
}

func makeDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
	return path
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func replaceEnvironment(base []string, replacements map[string]string) []string {
	if len(replacements) == 0 {
		return append([]string(nil), base...)
	}
	keys := mapKeys(replacements)
	result := removeEnvironment(base, keys...)
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+replacements[key])
	}
	return result
}

func removeEnvironment(base []string, names ...string) []string {
	removed := make(map[string]struct{}, len(names))
	for _, name := range names {
		removed[normalizeEnvironmentName(name)] = struct{}{}
	}
	result := make([]string, 0, len(base))
	for _, entry := range base {
		name := normalizeEnvironmentName(strings.SplitN(entry, "=", 2)[0])
		if _, found := removed[name]; !found {
			result = append(result, entry)
		}
	}
	return result
}

func removeEnvironmentWithPrefixes(base []string, prefixes ...string) []string {
	normalizedPrefixes := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		normalizedPrefixes[index] = normalizeEnvironmentName(prefix)
	}
	result := make([]string, 0, len(base))
	for _, entry := range base {
		name := normalizeEnvironmentName(strings.SplitN(entry, "=", 2)[0])
		remove := false
		for _, prefix := range normalizedPrefixes {
			if strings.HasPrefix(name, prefix) {
				remove = true
				break
			}
		}
		if !remove {
			result = append(result, entry)
		}
	}
	return result
}

func normalizeEnvironmentName(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
