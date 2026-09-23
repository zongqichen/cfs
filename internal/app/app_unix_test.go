//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zongqichen/cfs/internal/config"
	"github.com/zongqichen/cfs/internal/lock"
	"github.com/zongqichen/cfs/internal/store"
	"github.com/zongqichen/cfs/internal/workspace"
)

func TestShimUsesWorkspaceSpecificCFHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)

	firstRoot := markerWorkspace(t)
	secondRoot := markerWorkspace(t)
	first := runFromDirectory(t, firstRoot, []string{"cf", "apps"})
	second := runFromDirectory(t, secondRoot, []string{"cf", "apps"})

	if first.code != 0 || second.code != 0 {
		t.Fatalf("exit codes = %d, %d; stderr = %q / %q", first.code, second.code, first.stderr, second.stderr)
	}
	firstHome := outputValue(first.stdout, "CF_HOME")
	secondHome := outputValue(second.stdout, "CF_HOME")
	if firstHome == "" || secondHome == "" {
		t.Fatalf("missing CF_HOME output: %q / %q", first.stdout, second.stdout)
	}
	if firstHome == secondHome {
		t.Fatalf("different workspaces shared CF_HOME %q", firstHome)
	}
	if !strings.HasPrefix(firstHome, stateRoot) || !strings.HasPrefix(secondHome, stateRoot) {
		t.Fatalf("CF homes are outside state root: %q / %q", firstHome, secondHome)
	}
	firstPluginHome := outputValue(first.stdout, "CF_PLUGIN_HOME")
	secondPluginHome := outputValue(second.stdout, "CF_PLUGIN_HOME")
	if firstPluginHome == "" || firstPluginHome != secondPluginHome {
		t.Fatalf("workspaces did not share a plugin home: %q / %q", firstPluginHome, secondPluginHome)
	}
	if !strings.HasPrefix(firstPluginHome, stateRoot) {
		t.Fatalf("plugin home %q is outside state root %q", firstPluginHome, stateRoot)
	}
}

func TestShimReusesWorkspaceAcrossInvocations(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)

	first := runFromDirectory(t, root, []string{"cf", "apps"})
	second := runFromDirectory(t, filepath.Join(root, "nested"), []string{"cf", "target"})
	if first.code != 0 || second.code != 0 {
		t.Fatalf("exit codes = %d, %d", first.code, second.code)
	}
	if outputValue(first.stdout, "CF_HOME") != outputValue(second.stdout, "CF_HOME") {
		t.Fatalf("same workspace did not reuse CF_HOME: %q / %q", first.stdout, second.stdout)
	}
}

func TestShimFailsClosedOutsideWorkspace(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := t.TempDir()

	result := runFromDirectory(t, root, []string{"cf", "apps"})
	if result.code != exitUnavailable {
		t.Fatalf("exit code = %d, want %d", result.code, exitUnavailable)
	}
	if !strings.Contains(result.stderr, "refusing to use the global CF home") {
		t.Fatalf("stderr = %q", result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("official CLI unexpectedly ran: %q", result.stdout)
	}
}

func TestShimPreservesExplicitCFHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	externalHome := filepath.Join(t.TempDir(), "external")
	t.Setenv("CF_HOME", externalHome)

	result := runFromDirectory(t, t.TempDir(), []string{"cf", "apps"})
	if result.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.code, result.stderr)
	}
	if got := outputValue(result.stdout, "CF_HOME"); got != externalHome {
		t.Fatalf("CF_HOME = %q, want %q", got, externalHome)
	}
}

func TestShimPreservesExplicitPluginHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	externalPluginHome := filepath.Join(t.TempDir(), "plugins")
	t.Setenv("CF_PLUGIN_HOME", externalPluginHome)

	result := runFromDirectory(t, markerWorkspace(t), []string{"cf", "plugins"})
	if result.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.code, result.stderr)
	}
	if got := outputValue(result.stdout, "CF_PLUGIN_HOME"); got != externalPluginHome {
		t.Fatalf("CF_PLUGIN_HOME = %q, want %q", got, externalPluginHome)
	}
}

func TestExplicitWorkspaceRootOverridesInheritedCFHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	workspaceRoot := t.TempDir()
	externalHome := filepath.Join(t.TempDir(), "external")
	t.Setenv("CF_HOME", externalHome)
	t.Setenv("CFS_WORKSPACE_ROOT", workspaceRoot)

	result := runFromDirectory(t, t.TempDir(), []string{"cf", "apps"})
	if result.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.code, result.stderr)
	}
	got := outputValue(result.stdout, "CF_HOME")
	if got == externalHome || !strings.HasPrefix(got, stateRoot) {
		t.Fatalf("CF_HOME = %q, want managed home under %q", got, stateRoot)
	}
}

func TestShimPropagatesOfficialExitCode(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)

	result := runFromDirectory(t, root, []string{"cf", "exit-42"})
	if result.code != 42 {
		t.Fatalf("exit code = %d, want 42; stderr = %q", result.code, result.stderr)
	}
}

func TestShimRejectsConcurrentCommandInSameWorkspace(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)

	ws, err := workspace.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	stateStore := store.New(stateRoot)
	ctx, err := stateStore.ContextFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(ctx.LockPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	result := runFromDirectory(t, root, []string{"cf", "apps"})
	if result.code != exitTemporary {
		t.Fatalf("exit code = %d, want %d; stderr = %q", result.code, exitTemporary, result.stderr)
	}
	if !strings.Contains(result.stderr, "already has an active CF command") {
		t.Fatalf("stderr = %q", result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("official CLI unexpectedly ran: %q", result.stdout)
	}
}

func TestShimForwardsInteractiveInputAndArguments(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)

	result := runFromDirectoryWithInput(t, root, []string{"cf", "login", "--sso"}, "temporary-code\n")
	if result.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.code, result.stderr)
	}
	if !strings.Contains(result.stdout, "ARGS=login --sso") {
		t.Fatalf("arguments were not forwarded: %q", result.stdout)
	}
	if !strings.Contains(result.stdout, "STDIN=temporary-code") {
		t.Fatalf("stdin was not forwarded: %q", result.stdout)
	}
}

func TestShimRejectsForgedActiveContext(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	t.Setenv("CFS_ACTIVE_CONTEXT", "forged")

	result := runFromDirectory(t, t.TempDir(), []string{"cf", "version"})
	if result.code != exitUnavailable {
		t.Fatalf("exit code = %d, want %d; stdout = %q; stderr = %q", result.code, exitUnavailable, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "refusing invalid active context") {
		t.Fatalf("stderr = %q", result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("official CLI unexpectedly ran: %q", result.stdout)
	}
}

func TestShimAcceptsValidatedActiveContext(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)
	first := runFromDirectory(t, root, []string{"cf", "apps"})
	if first.code != exitOK {
		t.Fatalf("create workspace state: %#v", first)
	}
	home := outputValue(first.stdout, "CF_HOME")
	id := filepath.Base(filepath.Dir(home))
	t.Setenv("CFS_ACTIVE_CONTEXT", id)
	t.Setenv("CF_HOME", home)
	t.Setenv("CFS_WORKSPACE_ROOT", root)

	result := runFromDirectory(t, t.TempDir(), []string{"cf", "apps"})
	if result.code != exitOK {
		t.Fatalf("exit code = %d; stderr = %q", result.code, result.stderr)
	}
	if got := outputValue(result.stdout, "CF_HOME"); got != home {
		t.Fatalf("CF_HOME = %q, want %q", got, home)
	}
}

func TestShimRejectsActiveContextWithMismatchedHome(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)
	first := runFromDirectory(t, root, []string{"cf", "apps"})
	if first.code != exitOK {
		t.Fatalf("create workspace state: %#v", first)
	}
	home := outputValue(first.stdout, "CF_HOME")
	t.Setenv("CFS_ACTIVE_CONTEXT", filepath.Base(filepath.Dir(home)))
	t.Setenv("CF_HOME", filepath.Join(t.TempDir(), "other-home"))
	t.Setenv("CFS_WORKSPACE_ROOT", root)

	result := runFromDirectory(t, t.TempDir(), []string{"cf", "apps"})
	if result.code != exitUnavailable {
		t.Fatalf("exit code = %d, want %d; stdout = %q; stderr = %q", result.code, exitUnavailable, result.stdout, result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("official CLI unexpectedly ran: %q", result.stdout)
	}
}

func TestStatusRedactsOperationalMetadataAndDisablesCFTrace(t *testing.T) {
	fakeCF := writeFakeCF(t)
	stateRoot := canonicalTestPath(t, t.TempDir())
	configureTestEnvironment(t, fakeCF, stateRoot)
	root := markerWorkspace(t)
	t.Setenv("CFS_WORKSPACE_ROOT", root)
	t.Setenv("CF_TRACE", "/private/trace-output")

	result := runFromDirectory(t, root, []string{"cfs", "status", "--json", "--redact"})
	if result.code != exitOK {
		t.Fatalf("exit code = %d; stderr = %q", result.code, result.stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &output); err != nil {
		t.Fatalf("decode status: %v: %s", err, result.stdout)
	}
	for _, field := range []string{"workspace", "official_cf", "cf_home", "target_output"} {
		if _, exists := output[field]; exists {
			t.Fatalf("redacted status contains %q: %s", field, result.stdout)
		}
	}
	if redacted, _ := output["redacted"].(bool); !redacted {
		t.Fatalf("status is not marked as redacted: %s", result.stdout)
	}
	if strings.Contains(result.stdout, "/private/trace-output") {
		t.Fatalf("redacted status contains CF_TRACE: %s", result.stdout)
	}
}

func TestStatusProbeDisablesCFTrace(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)
	t.Setenv("CFS_WORKSPACE_ROOT", root)
	t.Setenv("CF_TRACE", "sensitive-trace-setting")

	result := runFromDirectory(t, root, []string{"cfs", "status", "--json"})
	if result.code != exitOK {
		t.Fatalf("exit code = %d; stderr = %q", result.code, result.stderr)
	}
	if strings.Contains(result.stdout, "sensitive-trace-setting") {
		t.Fatalf("status inherited CF_TRACE: %s", result.stdout)
	}
}

func TestResetRequiresYesWithNonTerminalInput(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)
	if setup := runFromDirectory(t, root, []string{"cf", "apps"}); setup.code != exitOK {
		t.Fatalf("create workspace state: %#v", setup)
	}

	result := runFromDirectory(t, root, []string{"cfs", "reset"})
	if result.code != exitUsage {
		t.Fatalf("exit code = %d, want %d; stderr = %q", result.code, exitUsage, result.stderr)
	}
	if !strings.Contains(result.stderr, "reset requires --yes") {
		t.Fatalf("stderr = %q", result.stderr)
	}
}

func TestResetUsesConfiguredLockTimeout(t *testing.T) {
	fakeCF := writeFakeCF(t)
	configureTestEnvironment(t, fakeCF, t.TempDir())
	root := markerWorkspace(t)
	if setup := runFromDirectory(t, root, []string{"cf", "apps"}); setup.code != exitOK {
		t.Fatalf("create workspace state: %#v", setup)
	}
	t.Setenv("CFS_LOCK_TIMEOUT", "invalid")

	result := runFromDirectory(t, root, []string{"cfs", "reset", "--yes"})
	if result.code != exitUsage {
		t.Fatalf("exit code = %d, want %d; stdout = %q; stderr = %q", result.code, exitUsage, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, `invalid CFS_LOCK_TIMEOUT "invalid"`) {
		t.Fatalf("stderr = %q", result.stderr)
	}
}

func runFromDirectory(t *testing.T, directory string, args []string) commandResult {
	return runFromDirectoryWithInput(t, directory, args, "")
}

func runFromDirectoryWithInput(t *testing.T, directory string, args []string, input string) commandResult {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(Options{Args: args, Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &stderr})
	if err := os.Chdir(previous); err != nil {
		t.Fatal(err)
	}
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func markerWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".cfs.toml"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func configureTestEnvironment(t *testing.T, fakeCF, stateRoot string) {
	t.Helper()
	t.Setenv("CFS_CONFIG_FILE", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("CFS_STATE_HOME", stateRoot)
	t.Setenv("CFS_WORKSPACE_ROOT", "")
	t.Setenv("CFS_DISABLE", "")
	t.Setenv("CFS_ACTIVE_CONTEXT", "")
	t.Setenv("CFS_LOCK_TIMEOUT", "50ms")
	t.Setenv("CF_HOME", "")
	t.Setenv("CF_PLUGIN_HOME", "")
	if err := config.Save(config.Config{Version: config.CurrentVersion, RealCFPath: fakeCF, ShimDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func writeFakeCF(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cf-real")
	script := `#!/bin/sh
printf 'CF_HOME=%s\n' "$CF_HOME"
printf 'CF_PLUGIN_HOME=%s\n' "$CF_PLUGIN_HOME"
printf 'CF_TRACE=%s\n' "$CF_TRACE"
printf 'ARGS=%s\n' "$*"
if [ "$1" = "exit-42" ]; then
  exit 42
fi
if [ "$1" = "login" ] && [ "$2" = "--sso" ]; then
  IFS= read -r value
  printf 'STDIN=%s\n' "$value"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func outputValue(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
